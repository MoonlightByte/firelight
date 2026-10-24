package nativesave

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/MoonlightByte/firelight/core"
)

func memberName(kind, hash string) string { return "history-" + kind + "-" + hash + ".nql" }

func (s *Store) readMember(kind, hash string) ([]byte, error) {
	if !validDigest(hash) {
		return nil, fmt.Errorf("invalid history member identity")
	}
	f, err := openRegular(filepath.Join(s.dir, memberName(kind, hash)))
	if err != nil {
		return nil, err
	}
	limit := int64(MaxRecordBytes)
	switch kind {
	case "genesis":
		limit = MaxGenesisRecordBytes
	case "batch":
		limit += 256
	}
	stat, err := f.Stat()
	if err != nil || stat.Size() <= 0 || stat.Size() > limit {
		_ = f.Close()
		return nil, fmt.Errorf("invalid history member size: %v", err)
	}
	data, e := io.ReadAll(io.LimitReader(f, limit+1))
	err = errors.Join(e, f.Close())
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	if int64(len(data)) > limit || hex.EncodeToString(sum[:]) != hash {
		return nil, fmt.Errorf("history member digest mismatch")
	}
	return data, nil
}

// Existing members are fully verified and fsynced on cold binding/recovery.
// A warm cursor never revisits its already acknowledged prefix.
func (s *Store) saveMember(kind string, data []byte, head core.Revision) (string, error) {
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	name := memberName(kind, hash)
	path := filepath.Join(s.dir, name)
	if _, err := os.Lstat(path); err == nil {
		old, e := s.readMember(kind, hash)
		if e != nil {
			return "", e
		}
		if !bytes.Equal(old, data) {
			return "", fmt.Errorf("immutable history member collision")
		}
		if e := s.stage("member-resync", name, head, false, func() error {
			f, e := openRegular(path)
			if e != nil {
				return e
			}
			return errors.Join(f.Sync(), f.Close())
		}); e != nil {
			return "", e
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	} else {
		temp, e := s.temporary("member", name, head, func(w io.Writer) error {
			n, e := w.Write(data)
			if e == nil && n != len(data) {
				e = io.ErrShortWrite
			}
			return e
		})
		if e != nil {
			return "", e
		}
		defer os.Remove(temp)
		if e := s.stage("member-publish", name, head, false, func() error { return os.Link(temp, path) }); e != nil {
			return "", e
		}
	}
	if e := s.stage("member-directory-sync", name, head, false, func() error { return s.ops.syncDir(s.dir) }); e != nil {
		return "", e
	}
	return hash, nil
}

func splitBatchMember(data []byte) (string, []byte, error) {
	i := bytes.IndexByte(data, '\n')
	if i < 0 || i > 255 {
		return "", nil, fmt.Errorf("invalid history link framing")
	}
	previous := string(data[:i])
	record := data[i+1:]
	if previous != "" && !validDigest(previous) {
		return "", nil, fmt.Errorf("invalid history predecessor")
	}
	if len(record) == 0 || len(record) > MaxRecordBytes || record[len(record)-1] != '\n' || bytes.IndexByte(record[:len(record)-1], '\n') >= 0 {
		return "", nil, fmt.Errorf("invalid logical batch framing")
	}
	return previous, record, nil
}

// segmentsReader holds bounded identities, not all decoded batches or open files.
// It presents precisely the existing JSONL schema to Read's replay validator.
type segmentsReader struct {
	store   *Store
	refs    []string
	next    int
	current *bytes.Reader
	footer  []byte
}

func (r *segmentsReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for {
		if r.current != nil {
			n, e := r.current.Read(p)
			if n > 0 {
				return n, nil
			}
			if e != io.EOF {
				return n, e
			}
		}
		if r.next < len(r.refs) {
			raw, e := r.store.readMember("batch", r.refs[r.next])
			if e != nil {
				return 0, e
			}
			_, record, e := splitBatchMember(raw)
			if e != nil {
				return 0, e
			}
			r.current = bytes.NewReader(record)
			r.next++
			continue
		}
		if r.footer != nil {
			r.current = bytes.NewReader(r.footer)
			r.footer = nil
			continue
		}
		return 0, io.EOF
	}
}

func (s *Store) readStoredGeneration(input io.Reader) (*Loaded, error) {
	r := bufio.NewReader(input)
	// Read only a bounded first record to distinguish the physical formats.
	first, err := (&recordReader{input: r, sum: sha256.New()}).line(maxDescriptorBytes)
	if err != nil {
		return nil, err
	}
	var kind struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(first, &kind); err != nil {
		return nil, err
	}
	if kind.Kind != "nql-native-linked-v1" {
		return Read(io.MultiReader(bytes.NewReader(append(first, '\n')), r))
	}
	if _, err := r.ReadByte(); err != io.EOF {
		return nil, fmt.Errorf("extra or unreadable segmented generation data: %v", err)
	}
	var d segmentDescriptor
	if err := strictDecode(first, &d); err != nil {
		return nil, err
	}
	if err := validateDescriptor(d); err != nil {
		return nil, err
	}
	genesis, err := s.readMember("genesis", d.Genesis)
	if err != nil {
		return nil, err
	}
	if len(genesis) == 0 || genesis[len(genesis)-1] != '\n' || bytes.IndexByte(genesis[:len(genesis)-1], '\n') >= 0 {
		return nil, fmt.Errorf("invalid genesis member framing")
	}
	refs := make([]string, d.Meta.Count)
	seen := make(map[string]bool, d.Meta.Count)
	tail := d.Tail
	total := int64(len(genesis))
	for i := len(refs) - 1; i >= 0; i-- {
		if tail == "" || seen[tail] {
			return nil, fmt.Errorf("short or cyclic history chain")
		}
		seen[tail] = true
		refs[i] = tail
		raw, e := s.readMember("batch", tail)
		if e != nil {
			return nil, e
		}
		previous, record, e := splitBatchMember(raw)
		if e != nil {
			return nil, e
		}
		total += int64(len(record))
		if total > d.RecordBytes || total > MaxBytes {
			return nil, fmt.Errorf("history chain exceeds descriptor byte bound")
		}
		tail = previous
	}
	if tail != "" || total != d.RecordBytes {
		return nil, fmt.Errorf("history chain count or byte mismatch")
	}
	m := d.Meta
	m.Format = FormatVersion
	header, err := encodedRecord(headerRecord{Kind: "header", Meta: m}, MaxHeadBytes)
	if err != nil {
		return nil, err
	}
	host, err := encodedRecord(hostRecord{Kind: "host", Data: d.Session}, MaxHostBytes)
	if err != nil {
		return nil, err
	}
	footer, err := encodedRecord(footerRecord{Kind: "footer", Meta: m, StateDigest: d.StateDigest}, MaxHeadBytes)
	if err != nil {
		return nil, err
	}
	stream := &segmentsReader{store: s, refs: refs, current: bytes.NewReader(append(header, genesis...)), footer: append(host, footer...)}
	loaded, err := Read(stream)
	if err != nil {
		return nil, err
	}
	physical := append(first, '\n')
	sum := sha256.Sum256(physical)
	loaded.Info = infoFor(d.Meta, d.StateDigest, int64(len(physical)), hex.EncodeToString(sum[:]))
	return loaded, nil
}
