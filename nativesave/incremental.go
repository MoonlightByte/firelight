package nativesave

import (
	"bytes"
	"crypto/sha256"
	"encoding"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"

	"github.com/MoonlightByte/firelight/core"
)

// SegmentedFormatVersion preserves complete history in immutable linked records.
// Bytes/SHA256 describe the physical descriptor, not the virtual JSONL archive.
const SegmentedFormatVersion = "nql-native-linked-v1"
const maxDescriptorBytes = MaxHostBytes + 2*MaxHeadBytes

type segmentDescriptor struct {
	Kind        string          `json:"kind"`
	Meta        archiveMetadata `json:"meta"`
	Genesis     string          `json:"genesis"`
	Tail        string          `json:"tail"`
	RecordBytes int64           `json:"record_bytes"`
	Session     SessionState    `json:"session"`
	StateDigest string          `json:"state_digest"`
}

type incrementalSave struct {
	cursor           core.CheckpointCursor
	descriptor       segmentDescriptor
	prefixState      []byte
	genesisSemantics string
}

func encodedRecord(v any, limit int) ([]byte, error) {
	var b bytes.Buffer
	w := recordWriter{output: &b, sum: sha256.New()}
	if err := w.record(v, limit); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func combineSemantics(a, b string) string {
	for _, tag := range []string{ItemRechargeSemanticsVersion, ItemChargesSemanticsVersion, ItemCatalogSemanticsVersion, BattleSemanticsVersion, ItemCreationSemanticsVersion, InventorySemanticsVersion, CreationSemanticsVersion, ClockSemanticsVersion, EquipmentSemanticsVersion, SemanticsVersion} {
		if a == tag || b == tag {
			return tag
		}
	}
	return ""
}

// SaveWorldJoint obtains a coherent detached suffix directly from the live
// authority. A caller-provided Checkpoint must still use SaveJoint's full audit.
func (s *Store) SaveWorldJoint(w *core.World, session SessionState, data []byte) (SaveResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkOpen(); err != nil {
		return SaveResult{}, err
	}
	if session.Campaign != s.campaign {
		return SaveResult{}, fmt.Errorf("save campaign identity mismatch")
	}
	if !s.fresh && s.selected == nil {
		return SaveResult{}, fmt.Errorf("load or explicitly recover the campaign before saving")
	}
	if s.selected != nil && (session.Next < s.next || sessionAllocation(session) != s.allocation) {
		return SaveResult{}, fmt.Errorf("checkpoint allocation differs from selected session")
	}
	var cursor core.CheckpointCursor
	if s.incremental != nil {
		cursor = s.incremental.cursor
	}
	delta, nextCursor, err := w.ExportSince(cursor)
	if err != nil {
		return SaveResult{}, err
	}
	if delta.Head > core.MaxArchiveBatches {
		return SaveResult{}, fmt.Errorf("history exceeds native capacity")
	}
	if err := validateBlueprintLimits(delta.State.Blueprint); err != nil {
		return SaveResult{}, err
	}
	// This stage is also the deterministic interleaving seam: a later live commit
	// cannot change the captured export or its LastAttempt membership.
	if err := s.stage("suffix-captured", "", delta.Head, false, func() error { return nil }); err != nil {
		return SaveResult{}, err
	}
	if err := validateLiveSession(session, delta, w); err != nil {
		return SaveResult{}, err
	}
	candidate := incrementalSave{cursor: nextCursor}
	sum := sha256.New()
	if s.incremental != nil {
		candidate.descriptor = s.incremental.descriptor
		candidate.genesisSemantics = s.incremental.genesisSemantics
		if err := sum.(encoding.BinaryUnmarshaler).UnmarshalBinary(s.incremental.prefixState); err != nil {
			return SaveResult{}, err
		}
	} else {
		if delta.Genesis == nil {
			return SaveResult{}, fmt.Errorf("initial history binding lacks genesis")
		}
		if err := validateBlueprintLimits(*delta.Genesis); err != nil {
			return SaveResult{}, err
		}
		raw, err := json.Marshal(delta.Genesis)
		if err != nil {
			return SaveResult{}, err
		}
		if len(raw) > MaxGenesisRecordBytes-1024 {
			return SaveResult{}, fmt.Errorf("genesis exceeds reserved native record size")
		}
		g := genesisRecord{Kind: "genesis", Data: *delta.Genesis, Empty: emptyMask(*delta.Genesis)}
		ghash, err := digest(g)
		if err != nil {
			return SaveResult{}, err
		}
		if s.selected != nil {
			count := s.selected.Info.Count
			if count > len(delta.History) {
				return SaveResult{}, fmt.Errorf("checkpoint omits selected history prefix")
			}
			prefix, err := historyPrefixDigest(delta.History[:count])
			if err != nil {
				return SaveResult{}, err
			}
			if ghash != s.selected.Info.GenesisDigest || prefix != s.prefixDigest {
				return SaveResult{}, fmt.Errorf("live World does not extend selected history/genesis")
			}
		}
		record, err := encodedRecord(g, MaxGenesisRecordBytes)
		if err != nil {
			return SaveResult{}, err
		}
		member, err := s.saveMember("genesis", record, delta.Head)
		if err != nil {
			return SaveResult{}, err
		}
		candidate.genesisSemantics = semanticsFor(*delta.Genesis)
		candidate.descriptor = segmentDescriptor{Kind: SegmentedFormatVersion, Genesis: member, RecordBytes: int64(len(record)), Meta: archiveMetadata{Format: SegmentedFormatVersion, Semantics: candidate.genesisSemantics, RNG: core.RandomVersion, Campaign: s.campaign, GenesisDigest: ghash}}
	}
	d := &candidate.descriptor
	for _, batch := range delta.History {
		raw, err := json.Marshal(batch)
		if err != nil {
			return SaveResult{}, err
		}
		if len(raw) > MaxRecordBytes-64 || len(batch.Events) > 2*core.MaxOperations || len(batch.Request.Operations) > core.MaxOperations {
			return SaveResult{}, fmt.Errorf("history record exceeds native bounds")
		}
		record, err := encodedRecord(batchRecord{Kind: "batch", Data: batch}, MaxRecordBytes)
		if err != nil {
			return SaveResult{}, err
		}
		if d.RecordBytes+int64(len(record)) > MaxBytes {
			return SaveResult{}, fmt.Errorf("native archive byte limit exceeded")
		}
		// Link framing has a separate <=256-byte bound. The logical batch keeps its
		// existing full record allowance, including near-limit admitted requests.
		member := append([]byte(d.Tail+"\n"), record...)
		tail, err := s.saveMember("batch", member, delta.Head)
		if err != nil {
			return SaveResult{}, err
		}
		d.Tail = tail
		d.RecordBytes += int64(len(record))
		_, _ = sum.Write(raw)
		_, _ = sum.Write([]byte{'\n'})
	}
	d.Meta.Semantics = combineSemantics(d.Meta.Semantics, combineSemantics(candidate.genesisSemantics, semanticsFor(core.Blueprint{}, delta.History...)))
	d.Meta.Head, d.Meta.Count = delta.Head, int(delta.Head)
	d.StateDigest, err = stateDigest(delta.State)
	if err != nil {
		return SaveResult{}, err
	}
	ref, err := s.saveAttachmentLocked(delta.Head, data)
	if err != nil {
		return SaveResult{}, err
	}
	session.Attachment = ref
	d.Session = session
	// Session input pointers must not alias the private immutable cache.
	if session.LastAttempt != nil {
		value := *session.LastAttempt
		d.Session.LastAttempt = &value
	}
	if err := validateDescriptor(*d); err != nil {
		return SaveResult{}, err
	}
	descriptor, err := encodedRecord(*d, maxDescriptorBytes)
	if err != nil {
		return SaveResult{}, err
	}
	candidate.prefixState, err = sum.(encoding.BinaryMarshaler).MarshalBinary()
	if err != nil {
		return SaveResult{}, err
	}
	physical := sha256.Sum256(descriptor)
	info := infoFor(d.Meta, d.StateDigest, int64(len(descriptor)), hex.EncodeToString(physical[:]))
	result, err := s.publishLocked(delta.Head, func(output io.Writer) (ArchiveInfo, error) {
		n, e := output.Write(descriptor)
		if e == nil && n != len(descriptor) {
			e = io.ErrShortWrite
		}
		return info, e
	})
	if err != nil {
		return SaveResult{}, err
	}
	s.incremental = &candidate
	s.prefixDigest = hex.EncodeToString(sum.Sum(nil))
	s.next, s.allocation = session.Next, sessionAllocation(session)
	return result, nil
}

func validateLiveSession(session SessionState, delta core.CheckpointDelta, w *core.World) error {
	attempt := session.LastAttempt
	session.LastAttempt = nil
	if err := validateSession(session, delta.History); err != nil {
		return err
	}
	if attempt == nil {
		return nil
	}
	n, matches, err := requestSequence(sessionAllocation(session), attempt.RequestID)
	if err != nil {
		return err
	}
	if (matches && n >= session.Next) || (!matches && !attempt.Committed) {
		return fmt.Errorf("last attempt does not precede the next campaign allocation")
	}
	revision, exists := w.ReceiptRevision(attempt.RequestID)
	if (exists && revision <= delta.Head) != attempt.Committed {
		return fmt.Errorf("last attempt committed status contradicts captured history")
	}
	return nil
}

func validateDescriptor(d segmentDescriptor) error {
	m := d.Meta
	if d.Kind != SegmentedFormatVersion || m.Format != SegmentedFormatVersion {
		return fmt.Errorf("invalid segmented format")
	}
	m.Format = FormatVersion
	if err := validateMeta(m); err != nil {
		return err
	}
	if !validDigest(d.Genesis) || !validDigest(d.StateDigest) || d.Session.Campaign != m.Campaign || (m.Count == 0 && d.Tail != "") || (m.Count > 0 && !validDigest(d.Tail)) || d.RecordBytes <= 0 || d.RecordBytes > MaxBytes {
		return fmt.Errorf("invalid segmented references or bounds")
	}
	header, err := encodedRecord(headerRecord{Kind: "header", Meta: m}, MaxHeadBytes)
	if err != nil {
		return err
	}
	host, err := encodedRecord(hostRecord{Kind: "host", Data: d.Session}, MaxHostBytes)
	if err != nil {
		return err
	}
	footer, err := encodedRecord(footerRecord{Kind: "footer", Meta: m, StateDigest: d.StateDigest}, MaxHeadBytes)
	if err != nil {
		return err
	}
	if d.RecordBytes+int64(len(header)+len(host)+len(footer)) > MaxBytes {
		return fmt.Errorf("logical archive exceeds native byte bound")
	}
	return nil
}
