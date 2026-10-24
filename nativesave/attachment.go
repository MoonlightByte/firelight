package nativesave

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/MoonlightByte/firelight/core"
	"io"
	"os"
	"path/filepath"
)

const MaxAttachmentBytes = 8 << 20

// AttachmentRef pins immutable host continuation bytes to one mechanical
// generation. The host validates their application schema and meaning.
type AttachmentRef struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}
type JointLoaded struct {
	*Loaded
	Attachment []byte
}

func validateAttachmentRef(r *AttachmentRef) error {
	if r == nil {
		return nil
	}
	if !validDigest(r.SHA256) || r.Name != "host-"+r.SHA256+".json" || r.Bytes <= 0 || r.Bytes > MaxAttachmentBytes {
		return fmt.Errorf("invalid immutable host attachment reference")
	}
	return nil
}
func (s *Store) readAttachment(r *AttachmentRef) ([]byte, error) {
	if r == nil {
		return nil, fmt.Errorf("joint continuation requires a host attachment")
	}
	if err := validateAttachmentRef(r); err != nil {
		return nil, err
	}
	f, err := openRegular(filepath.Join(s.dir, r.Name))
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || info.Size() != r.Bytes {
		_ = f.Close()
		return nil, fmt.Errorf("host attachment size mismatch: %v", err)
	}
	data, readErr := io.ReadAll(io.LimitReader(f, MaxAttachmentBytes+1))
	err = errors.Join(readErr, f.Close())
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	if int64(len(data)) != r.Bytes || hex.EncodeToString(sum[:]) != r.SHA256 {
		return nil, fmt.Errorf("host attachment bytes disagree with pinned reference")
	}
	return data, nil
}

// SaveJoint publishes one selected checkpoint after its immutable adjunct is
// durable. There is no independent narrative latest pointer. On any error no
// gameplay rejection is implied; a caller that already committed must freeze
// mutation until it obtains durable checkpoint acknowledgement.
func (s *Store) SaveJoint(cp core.Checkpoint, session SessionState, data []byte) (SaveResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveJointLocked(cp, session, data)
}
func (s *Store) saveJointLocked(cp core.Checkpoint, session SessionState, data []byte) (SaveResult, error) {
	ref, err := s.saveAttachmentLocked(cp.Head, data)
	if err != nil {
		return SaveResult{}, err
	}
	session.Attachment = ref
	return s.saveLocked(cp, session)
}

func (s *Store) saveAttachmentLocked(head core.Revision, data []byte) (*AttachmentRef, error) {
	if err := s.checkOpen(); err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > MaxAttachmentBytes {
		return nil, fmt.Errorf("host attachment must be 1..8MiB")
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	ref := &AttachmentRef{Name: "host-" + hash + ".json", SHA256: hash, Bytes: int64(len(data))}
	path := filepath.Join(s.dir, ref.Name)
	if _, err := os.Lstat(path); err == nil {
		existing, e := s.readAttachment(ref)
		if e != nil {
			return nil, e
		}
		if !bytes.Equal(existing, data) {
			return nil, fmt.Errorf("immutable attachment collision")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	} else {
		temp, e := s.temporary("attachment", ref.Name, head, func(w io.Writer) error {
			n, e := w.Write(data)
			if e == nil && n != len(data) {
				e = io.ErrShortWrite
			}
			return e
		})
		if e != nil {
			return nil, e
		}
		defer os.Remove(temp)
		// Hard-link is atomic and never replaces an existing immutable member.
		if e = s.stage("attachment-publish", ref.Name, head, false, func() error { return os.Link(temp, path) }); e != nil {
			return nil, e
		}
	}
	// Sync even when reusing a member left by an earlier uncertain failure.
	if e := s.stage("attachment-directory-sync", ref.Name, head, false, func() error { return s.ops.syncDir(s.dir) }); e != nil {
		return nil, e
	}
	return ref, nil
}

// LoadJoint validates both selected files. It does not establish durability
// after an earlier uncertain publication, nor validate the host's schema.
func (s *Store) LoadJoint() (*JointLoaded, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, e := s.loadLocked()
	if e != nil {
		return nil, e
	}
	data, e := s.readAttachment(l.Session.Attachment)
	if e != nil {
		return nil, e
	}
	return &JointLoaded{Loaded: l, Attachment: data}, nil
}

// ReadNamedJoint inspects an exact immutable generation without selecting it or
// changing this Store's continuation basis. Loading an older gameplay save uses
// a fresh run allocation namespace at the host layer.
func (s *Store) ReadNamedJoint(generation string) (*JointLoaded, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.checkOpen(); e != nil {
		return nil, e
	}
	l, e := s.readGeneration(generation)
	if e != nil {
		return nil, e
	}
	data, e := s.readAttachment(l.Session.Attachment)
	if e != nil {
		return nil, e
	}
	return &JointLoaded{Loaded: l, Attachment: data}, nil
}
