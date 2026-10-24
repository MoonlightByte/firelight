// Package nativesave stores explicit, complete native campaign checkpoints.
// It does not make individual gameplay transactions durable or authenticate
// history against an external source.
package nativesave

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"math"
	"reflect"
	"strconv"
	"strings"

	"github.com/MoonlightByte/firelight/core"
)

const (
	FormatVersion                = "nql-native-jsonl-v1"
	SemanticsVersion             = "nql-native-adventure-alpha-1"
	EquipmentSemanticsVersion    = "nql-native-equipment-1"
	BattleSemanticsVersion       = "nql-native-battle-1"
	ClockSemanticsVersion        = "nql-native-clock-1"
	CreationSemanticsVersion     = "nql-native-character-1"
	ItemCreationSemanticsVersion = "nql-native-item-creation-1"
	InventorySemanticsVersion    = "nql-native-inventory-1"
	ItemCatalogSemanticsVersion  = "nql-native-item-catalog-1"
	ItemChargesSemanticsVersion  = "nql-native-item-charges-1"
	ItemRechargeSemanticsVersion = "nql-native-item-recharge-1"
	MaxBytes                     = 128 << 20
	MaxRecordBytes               = 1 << 20
	MaxGenesisRecordBytes        = core.MaxGenesisRecordBytes
	MaxHeadBytes                 = 4 << 10
	MaxHostBytes                 = 32 << 10
)

type AttemptRef struct {
	RequestID string `json:"request_id"`
	Committed bool   `json:"committed"`
}

type SessionState struct {
	Allocation  string         `json:"allocation,omitempty"`
	Attachment  *AttachmentRef `json:"attachment,omitempty"`
	Campaign    string         `json:"campaign"`
	Next        uint64         `json:"next"`
	LastAttempt *AttemptRef    `json:"last_attempt"`
}

type ArchiveInfo struct {
	Format        string        `json:"format"`
	Semantics     string        `json:"semantics"`
	RNG           string        `json:"rng"`
	Campaign      string        `json:"campaign"`
	GenesisDigest string        `json:"genesis_digest"`
	StateDigest   string        `json:"state_digest"`
	Count         int           `json:"count"`
	Head          core.Revision `json:"head"`
	Bytes         int64         `json:"bytes"`
	SHA256        string        `json:"sha256"`
}

type Loaded struct {
	World      *core.World
	Session    SessionState
	Info       ArchiveInfo
	Generation string
	Recovered  bool
}

type archiveMetadata struct {
	Format        string        `json:"format"`
	Semantics     string        `json:"semantics"`
	RNG           string        `json:"rng"`
	Campaign      string        `json:"campaign"`
	GenesisDigest string        `json:"genesis_digest"`
	Count         int           `json:"count"`
	Head          core.Revision `json:"head"`
}

type headerRecord struct {
	Kind string          `json:"kind"`
	Meta archiveMetadata `json:"meta"`
}

type genesisEmpty struct {
	Abilities   bool `json:"abilities"`
	Definitions bool `json:"definitions"`
	Conditions  bool `json:"conditions"`
	Retired     bool `json:"retired"`
}

type genesisRecord struct {
	Kind  string         `json:"kind"`
	Data  core.Blueprint `json:"data"`
	Empty genesisEmpty   `json:"empty"`
}

type batchRecord struct {
	Kind string     `json:"kind"`
	Data core.Batch `json:"data"`
}

type hostRecord struct {
	Kind string       `json:"kind"`
	Data SessionState `json:"data"`
}

type footerRecord struct {
	Kind        string          `json:"kind"`
	Meta        archiveMetadata `json:"meta"`
	StateDigest string          `json:"state_digest"`
}

func emptyMask(b core.Blueprint) genesisEmpty {
	return genesisEmpty{
		Abilities:   b.AbilityDefinitions != nil && len(b.AbilityDefinitions) == 0,
		Definitions: b.ConditionDefinitions != nil && len(b.ConditionDefinitions) == 0,
		Conditions:  b.Conditions != nil && len(b.Conditions) == 0,
		Retired:     b.RetiredConditionIDs != nil && len(b.RetiredConditionIDs) == 0,
	}
}

func restoreEmpty(g *genesisRecord) {
	// After strict typed decode, omitted fields are nil. Present [] and null
	// need distinct detection, so the raw member presence is checked by Read.
	if g.Empty.Abilities {
		g.Data.AbilityDefinitions = []core.AbilityDefinition{}
	}
	if g.Empty.Definitions {
		g.Data.ConditionDefinitions = []core.ConditionDefinition{}
	}
	if g.Empty.Conditions {
		g.Data.Conditions = []core.ConditionInstance{}
	}
	if g.Empty.Retired {
		g.Data.RetiredConditionIDs = []core.ConditionInstanceID{}
	}
}

func checkGenesisMask(raw []byte, g genesisRecord) error {
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(wrapper["data"], &fields); err != nil {
		return err
	}
	for _, f := range []struct {
		name   string
		empty  bool
		length int
	}{
		{"ability_definitions", g.Empty.Abilities, len(g.Data.AbilityDefinitions)},
		{"condition_definitions", g.Empty.Definitions, len(g.Data.ConditionDefinitions)},
		{"conditions", g.Empty.Conditions, len(g.Data.Conditions)},
		{"retired_condition_ids", g.Empty.Retired, len(g.Data.RetiredConditionIDs)},
	} {
		_, present := fields[f.name]
		if present && (f.empty || f.length == 0) {
			return fmt.Errorf("genesis %s has inconsistent empty-presence mask", f.name)
		}
	}
	return nil
}

func digest(v any) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func stateDigest(s core.Snapshot) (string, error) {
	return digest(struct {
		State core.Snapshot `json:"state"`
		Empty genesisEmpty  `json:"empty"`
	}{s, emptyMask(s.Blueprint)})
}

func validDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func validateCampaign(s string) error {
	if len(s) == 0 || len(s) > 128 || s == "." || s == ".." {
		return fmt.Errorf("campaign identity must be 1..128 safe ASCII bytes")
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' || c == ':') {
			return fmt.Errorf("campaign identity contains an invalid character")
		}
	}
	return nil
}

func requestSequence(campaign, id string) (uint64, bool, error) {
	prefix := campaign + ":request:"
	if !strings.HasPrefix(id, prefix) {
		return 0, false, nil
	}
	s := strings.TrimPrefix(id, prefix)
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil || n == 0 || n == math.MaxUint64 || strconv.FormatUint(n, 10) != s {
		return 0, true, fmt.Errorf("noncanonical or exhausted request allocation %q", id)
	}
	return n, true, nil
}

func validateSession(s SessionState, history []core.Batch) error {
	if err := validateAttachmentRef(s.Attachment); err != nil {
		return err
	}
	if err := validateCampaign(s.Campaign); err != nil {
		return err
	}
	if err := validateCampaign(sessionAllocation(s)); err != nil {
		return err
	}
	if s.Next == 0 || s.Next == math.MaxUint64 {
		return fmt.Errorf("next request allocation is zero or exhausted")
	}
	committed := make(map[string]bool, len(history))
	for _, b := range history {
		id := b.Request.ID
		if committed[id] {
			return fmt.Errorf("duplicate committed request identity")
		}
		committed[id] = true
		n, matches, err := requestSequence(sessionAllocation(s), id)
		if err != nil {
			return err
		}
		if matches && n >= s.Next {
			return fmt.Errorf("next request allocation does not follow committed history")
		}
	}
	if s.LastAttempt != nil {
		n, matches, err := requestSequence(sessionAllocation(s), s.LastAttempt.RequestID)
		if err != nil {
			return err
		}
		if (matches && n >= s.Next) || (!matches && !s.LastAttempt.Committed) {
			return fmt.Errorf("last attempt does not precede the next campaign allocation")
		}
		if committed[s.LastAttempt.RequestID] != s.LastAttempt.Committed {
			return fmt.Errorf("last attempt committed status contradicts history")
		}
	}
	return nil
}

func validateBlueprintLimits(b core.Blueprint) error {
	if len(b.Characters) > core.MaxWorldCharacters || len(b.Items) > core.MaxWorldItems || len(b.Locations) > core.MaxWorldLocations || len(b.ConditionDefinitions) > core.MaxWorldConditionDefinitions || len(b.Conditions) > core.MaxWorldConditions {
		return fmt.Errorf("native checkpoint entity or Condition capacity exceeded")
	}
	return nil
}

func semanticsFor(b core.Blueprint, history ...core.Batch) string {
	// Charge rules live only on genesis definitions, so they alone select the
	// charges profile, which extends the catalog profile. Recharge, dice and
	// last-charge rules select the recharge profile, which also pins
	// core.ChargeRollVersion.
	charges := false
	for _, d := range b.ItemDefinitions {
		if r := d.Charges; r != nil {
			if r.Recharge != nil || r.InitialDice != nil || r.Exhausted != nil {
				return ItemRechargeSemanticsVersion
			}
			charges = true
		}
	}
	if charges {
		return ItemChargesSemanticsVersion
	}
	if len(b.ItemDefinitions) > 0 {
		return ItemCatalogSemanticsVersion
	}
	if b.Battle != nil {
		return BattleSemanticsVersion
	}
	if b.Equipment != nil {
		for _, d := range b.Equipment.Definitions {
			for _, m := range d.Modes {
				for _, a := range m.Attacks {
					if a.Melee != nil {
						return BattleSemanticsVersion
					}
				}
			}
		}
		for _, t := range b.ConditionDefinitions {
			for _, a := range t.Attacks {
				if a.Melee != nil {
					return BattleSemanticsVersion
				}
			}
		}
	}
	for _, i := range b.Items {
		if i.Description != "" {
			return ItemCreationSemanticsVersion
		}
	}
	for _, batch := range history {
		for _, op := range batch.Request.Operations {
			if op.CreateItem != nil {
				return ItemCreationSemanticsVersion
			}
		}
	}
	// Inventory is a superset of Character creation. Scan the complete history
	// before selecting an older feature tag; creation may precede consumption.
	for _, i := range b.Items {
		if i.Quantity != nil {
			return InventorySemanticsVersion
		}
	}
	for _, batch := range history {
		for _, op := range batch.Request.Operations {
			if op.Consume != nil || op.Add != nil {
				return InventorySemanticsVersion
			}
		}
	}
	for _, batch := range history {
		for _, op := range batch.Request.Operations {
			if op.CreateCharacter != nil {
				return CreationSemanticsVersion
			}
		}
	}
	if b.Clock != nil {
		return ClockSemanticsVersion
	}
	if b.Equipment != nil {
		return EquipmentSemanticsVersion
	}
	return SemanticsVersion
}
func validateMeta(m archiveMetadata) error {
	if m.Format != FormatVersion || (m.Semantics != SemanticsVersion && m.Semantics != EquipmentSemanticsVersion && m.Semantics != ClockSemanticsVersion && m.Semantics != CreationSemanticsVersion && m.Semantics != InventorySemanticsVersion && m.Semantics != ItemCreationSemanticsVersion && m.Semantics != BattleSemanticsVersion && m.Semantics != ItemCatalogSemanticsVersion && m.Semantics != ItemChargesSemanticsVersion && m.Semantics != ItemRechargeSemanticsVersion) || m.RNG != core.RandomVersion {
		return fmt.Errorf("unsupported native archive format, semantics or RNG version")
	}
	if err := validateCampaign(m.Campaign); err != nil {
		return err
	}
	if m.Count < 0 || m.Count > core.MaxArchiveBatches || m.Head != core.Revision(m.Count) {
		return fmt.Errorf("native archive count/head mismatch or capacity exceeded")
	}
	if !validDigest(m.GenesisDigest) {
		return fmt.Errorf("invalid genesis digest")
	}
	return nil
}

func infoFor(m archiveMetadata, state string, size int64, sum string) ArchiveInfo {
	return ArchiveInfo{Format: m.Format, Semantics: m.Semantics, RNG: m.RNG, Campaign: m.Campaign, GenesisDigest: m.GenesisDigest, StateDigest: state, Count: m.Count, Head: m.Head, Bytes: size, SHA256: sum}
}

type sliceBatches struct {
	history []core.Batch
	index   int
}

func (s *sliceBatches) Next() (core.Batch, error) {
	if s.index == len(s.history) {
		return core.Batch{}, io.EOF
	}
	b := s.history[s.index]
	s.index++
	return b, nil
}

type recordWriter struct {
	output io.Writer
	bytes  int64
	sum    hash.Hash
}

func (w *recordWriter) record(v any, limit int) error {
	if err := validGoStrings(reflect.ValueOf(v), 0); err != nil {
		return err
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(data)+1 > limit || w.bytes+int64(len(data)+1) > MaxBytes {
		return fmt.Errorf("native archive byte or record limit exceeded")
	}
	// Apply the identical schema and string limits to caller-provided exports.
	if err := strictDecode(data, reflect.New(reflect.TypeOf(v)).Interface()); err != nil {
		return err
	}
	data = append(data, '\n')
	n, err := w.output.Write(data)
	if err != nil {
		return err
	}
	if n != len(data) {
		return io.ErrShortWrite
	}
	w.bytes += int64(n)
	_, _ = w.sum.Write(data)
	return nil
}

// Write encodes a coherent checkpoint. Success describes bytes accepted by the
// writer; callers still own flush, sync, close and publication acknowledgment.
func Write(output io.Writer, cp core.Checkpoint, session SessionState) (ArchiveInfo, error) {
	if output == nil {
		return ArchiveInfo{}, fmt.Errorf("native archive writer is required")
	}
	if cp.Head != cp.State.Revision || cp.Head != core.Revision(len(cp.History)) || len(cp.History) > core.MaxArchiveBatches {
		return ArchiveInfo{}, fmt.Errorf("incoherent checkpoint head/count")
	}
	if err := validateSession(session, cp.History); err != nil {
		return ArchiveInfo{}, err
	}
	if err := validateBlueprintLimits(cp.Genesis); err != nil {
		return ArchiveInfo{}, err
	}
	if err := validateBlueprintLimits(cp.State.Blueprint); err != nil {
		return ArchiveInfo{}, err
	}
	// Public checkpoints are detached caller records, not authority handles.
	// Validate their audit state before any caller can acknowledge them as saved.
	w, err := core.ReplayArchive(cp.Genesis, cp.Head, len(cp.History), &sliceBatches{history: cp.History})
	if err != nil {
		return ArchiveInfo{}, err
	}
	audit := w.ExportCheckpoint()
	if !reflect.DeepEqual(audit.Genesis, cp.Genesis) || !reflect.DeepEqual(audit.State, cp.State) {
		return ArchiveInfo{}, fmt.Errorf("checkpoint audit state or normalized genesis contradicts history")
	}
	g := genesisRecord{Kind: "genesis", Data: cp.Genesis, Empty: emptyMask(cp.Genesis)}
	genesisHash, err := digest(g)
	if err != nil {
		return ArchiveInfo{}, err
	}
	stateHash, err := stateDigest(cp.State)
	if err != nil {
		return ArchiveInfo{}, err
	}
	meta := archiveMetadata{Format: FormatVersion, Semantics: semanticsFor(cp.Genesis, audit.History...), RNG: core.RandomVersion, Campaign: session.Campaign, GenesisDigest: genesisHash, Count: len(cp.History), Head: cp.Head}
	rawGenesis, err := json.Marshal(cp.Genesis)
	if err != nil {
		return ArchiveInfo{}, err
	}
	if len(rawGenesis) > MaxGenesisRecordBytes-1024 {
		return ArchiveInfo{}, fmt.Errorf("genesis exceeds reserved native record size")
	}
	writer := recordWriter{output: output, sum: sha256.New()}
	if err := writer.record(headerRecord{Kind: "header", Meta: meta}, MaxHeadBytes); err != nil {
		return ArchiveInfo{}, err
	}
	before := writer.bytes
	if err := writer.record(g, MaxGenesisRecordBytes); err != nil {
		return ArchiveInfo{}, err
	}
	if writer.bytes-before > int64(len(rawGenesis)+1024) {
		return ArchiveInfo{}, fmt.Errorf("genesis framing exceeded reservation")
	}
	for _, b := range cp.History {
		raw, err := json.Marshal(b)
		if err != nil {
			return ArchiveInfo{}, err
		}
		if len(raw) > MaxRecordBytes-64 {
			return ArchiveInfo{}, fmt.Errorf("batch exceeds reserved native record size")
		}
		before = writer.bytes
		if err := writer.record(batchRecord{Kind: "batch", Data: b}, MaxRecordBytes); err != nil {
			return ArchiveInfo{}, err
		}
		if writer.bytes-before > int64(len(raw)+64) {
			return ArchiveInfo{}, fmt.Errorf("batch framing exceeded reservation")
		}
	}
	if err := writer.record(hostRecord{Kind: "host", Data: session}, MaxHostBytes); err != nil {
		return ArchiveInfo{}, err
	}
	if err := writer.record(footerRecord{Kind: "footer", Meta: meta, StateDigest: stateHash}, MaxHeadBytes); err != nil {
		return ArchiveInfo{}, err
	}
	return infoFor(meta, stateHash, writer.bytes, hex.EncodeToString(writer.sum.Sum(nil))), nil
}

type recordReader struct {
	input *bufio.Reader
	bytes int64
	sum   hash.Hash
}

func (r *recordReader) line(limit int) ([]byte, error) {
	var data []byte
	for {
		part, err := r.input.ReadSlice('\n')
		if len(data)+len(part) > limit || r.bytes+int64(len(part)) > MaxBytes {
			return nil, fmt.Errorf("native archive byte or record limit exceeded")
		}
		data = append(data, part...)
		r.bytes += int64(len(part))
		_, _ = r.sum.Write(part)
		if err == bufio.ErrBufferFull {
			continue
		}
		if err == io.EOF {
			return nil, fmt.Errorf("truncated native record: %w", io.ErrUnexpectedEOF)
		}
		if err != nil {
			return nil, err
		}
		return data[:len(data)-1], nil
	}
}

type archiveBatches struct {
	records   *recordReader
	remaining int
}

func (b *archiveBatches) Next() (core.Batch, error) {
	// This EOF terminates only the batch section. Read still owns the World
	// while validating the following host record, footer and physical EOF.
	if b.remaining == 0 {
		return core.Batch{}, io.EOF
	}
	data, err := b.records.line(MaxRecordBytes)
	if err != nil {
		return core.Batch{}, err
	}
	var record batchRecord
	if err := strictDecode(data, &record); err != nil {
		return core.Batch{}, err
	}
	if record.Kind != "batch" {
		return core.Batch{}, fmt.Errorf("expected native batch record")
	}
	b.remaining--
	return record.Data, nil
}

// Read returns no usable World until the full archive, allocation state, footer,
// continuation digest and physical EOF agree. It does not alter its input.
func Read(input io.Reader) (*Loaded, error) {
	if input == nil {
		return nil, fmt.Errorf("native archive reader is required")
	}
	r := &recordReader{input: bufio.NewReaderSize(input, 4096), sum: sha256.New()}
	data, err := r.line(MaxHeadBytes)
	if err != nil {
		return nil, err
	}
	var header headerRecord
	if err := strictDecode(data, &header); err != nil {
		return nil, err
	}
	if header.Kind != "header" {
		return nil, fmt.Errorf("expected native header")
	}
	if err := validateMeta(header.Meta); err != nil {
		return nil, err
	}
	data, err = r.line(MaxGenesisRecordBytes)
	if err != nil {
		return nil, err
	}
	var g genesisRecord
	if err := strictDecode(data, &g); err != nil {
		return nil, err
	}
	if g.Kind != "genesis" {
		return nil, fmt.Errorf("expected native genesis")
	}
	if err := checkGenesisMask(data, g); err != nil {
		return nil, err
	}
	restoreEmpty(&g)
	genesisHash, err := digest(g)
	if err != nil {
		return nil, err
	}
	if genesisHash != header.Meta.GenesisDigest {
		return nil, fmt.Errorf("genesis digest mismatch")
	}
	if err := validateBlueprintLimits(g.Data); err != nil {
		return nil, err
	}
	w, err := core.ReplayArchive(g.Data, header.Meta.Head, header.Meta.Count, &archiveBatches{records: r, remaining: header.Meta.Count})
	if err != nil {
		return nil, err
	}
	audit := w.ExportCheckpoint()
	if header.Meta.Semantics != semanticsFor(g.Data, audit.History...) {
		return nil, fmt.Errorf("native semantics profile contradicts genesis/history")
	}
	if !reflect.DeepEqual(audit.Genesis, g.Data) {
		return nil, fmt.Errorf("archive genesis is not normalized")
	}
	if err := validateBlueprintLimits(audit.State.Blueprint); err != nil {
		return nil, err
	}
	data, err = r.line(MaxHostBytes)
	if err != nil {
		return nil, err
	}
	var host hostRecord
	if err := strictDecode(data, &host); err != nil {
		return nil, err
	}
	if host.Kind != "host" || host.Data.Campaign != header.Meta.Campaign {
		return nil, fmt.Errorf("native host campaign or record kind mismatch")
	}
	if err := validateSession(host.Data, audit.History); err != nil {
		return nil, err
	}
	data, err = r.line(MaxHeadBytes)
	if err != nil {
		return nil, err
	}
	var footer footerRecord
	if err := strictDecode(data, &footer); err != nil {
		return nil, err
	}
	if footer.Kind != "footer" || footer.Meta != header.Meta {
		return nil, fmt.Errorf("native footer disagrees with selected header")
	}
	stateHash, err := stateDigest(audit.State)
	if err != nil {
		return nil, err
	}
	if !validDigest(footer.StateDigest) || footer.StateDigest != stateHash {
		return nil, fmt.Errorf("native continuation digest mismatch")
	}
	if _, err := r.input.ReadByte(); err != io.EOF {
		if err != nil {
			return nil, fmt.Errorf("native archive final read: %w", err)
		}
		return nil, errors.New("extra data after native footer")
	}
	return &Loaded{World: w, Session: host.Data, Info: infoFor(header.Meta, stateHash, r.bytes, hex.EncodeToString(r.sum.Sum(nil)))}, nil
}

// encodeHead uses the same schema rules, including all required members.
func encodeHead(v any) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if len(data)+1 > MaxHeadBytes {
		return nil, fmt.Errorf("selected head exceeds 4KiB")
	}
	if err := strictDecode(data, reflect.New(reflect.TypeOf(v)).Interface()); err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func decodeHead(data []byte, dst any) error {
	if len(data) == 0 || len(data) > MaxHeadBytes || !bytes.HasSuffix(data, []byte{'\n'}) {
		return fmt.Errorf("invalid selected-head size or framing")
	}
	return strictDecode(data, dst)
}
