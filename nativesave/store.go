package nativesave

import (
	"bufio"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/MoonlightByte/firelight/core"
)

// StageError distinguishes checkpoint publication from gameplay commitment.
// Uncertain means HEAD publication may have occurred without an established
// durable acknowledgment. Candidate and previous immutable files are retained.
type StageError struct {
	Stage      string
	Uncertain  bool
	Generation string
	Head       core.Revision
	Err        error
}

func (e *StageError) Error() string {
	if e.Uncertain {
		return fmt.Sprintf("uncertain checkpoint publication at %s: %v", e.Stage, e.Err)
	}
	return fmt.Sprintf("checkpoint %s: %v", e.Stage, e.Err)
}
func (e *StageError) Unwrap() error { return e.Err }

type SaveResult struct {
	Generation string
	Head       core.Revision
	Bytes      int64
	SHA256     string
}

type manifest struct {
	Generation string      `json:"generation"`
	Previous   string      `json:"previous"`
	Info       ArchiveInfo `json:"info"`
}

type syncFile interface {
	io.Writer
	Sync() error
	Close() error
	Name() string
}

// Private seams support deterministic syscall failures without exposing a
// product callback mechanism or holding a World lock across any storage work.
type fileOps struct {
	createTemp func(string, string) (syncFile, error)
	rename     func(string, string) error
	syncDir    func(string) error
	flush      func(*bufio.Writer) error
	before     func(string) error
}

func realFileOps() fileOps {
	return fileOps{
		createTemp: func(dir, pattern string) (syncFile, error) { return os.CreateTemp(dir, pattern) },
		rename:     os.Rename, syncDir: syncDirectory,
		flush: func(w *bufio.Writer) error { return w.Flush() },
	}
}

type Store struct {
	mu           sync.Mutex
	dir          string
	campaign     string
	lock         *os.File
	closed       bool
	fresh        bool
	selected     *manifest
	prefixDigest string
	next         uint64
	allocation   string
	forkPrevious string // temporary fork candidate only; never a second selector
	ops          fileOps
	incremental  *incrementalSave
}

func syncDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	err = f.Sync()
	return errors.Join(err, f.Close())
}

func requireDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("save path is not a directory: %s", path)
	}
	return nil
}

func ensureDirectory(path string) error {
	info, err := os.Lstat(path)
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("save root is not a directory")
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	parent := filepath.Dir(path)
	if parent == path {
		return err
	}
	if err := ensureDirectory(parent); err != nil {
		return err
	}
	if err := os.Mkdir(path, 0700); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return err
		}
		if err := requireDirectory(path); err != nil {
			return err
		}
	}
	// Persist every newly introduced directory entry before any save can ack.
	return syncDirectory(parent)
}

// Acquire takes exclusive cooperative ownership for the entire campaign
// session. create requires a new campaign directory; resume requires an existing
// persistent lock inode. The product selects its documented Linux save root.
// Generic host-selected roots have only the guarantees of their own filesystem.
func Acquire(root, campaign string, create bool) (*Store, error) {
	if err := platformSupported(); err != nil {
		return nil, err
	}
	if err := validateCampaign(campaign); err != nil {
		return nil, err
	}
	if root == "" {
		return nil, fmt.Errorf("save root is required")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if create {
		err = ensureDirectory(root)
	} else {
		err = requireDirectory(root)
	}
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(root, campaign)
	if create {
		if err := os.Mkdir(dir, 0700); err != nil {
			return nil, fmt.Errorf("new campaign requires a fresh target: %w", err)
		}
		if err := syncDirectory(root); err != nil {
			return nil, fmt.Errorf("sync new campaign parent: %w", err)
		}
	} else if err := requireDirectory(dir); err != nil {
		return nil, err
	}
	f, err := openSessionLock(filepath.Join(dir, ".lock"), create)
	if err != nil {
		return nil, err
	}
	if err := lockSession(f); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("campaign already in use or cannot be locked: %w", err)
	}
	if create {
		if err := errors.Join(f.Sync(), syncDirectory(dir)); err != nil {
			return nil, errors.Join(fmt.Errorf("persist campaign lock inode: %w", err), unlockSession(f), f.Close())
		}
	}
	return &Store{dir: dir, campaign: campaign, lock: f, fresh: create, ops: realFileOps()}, nil
}

func validGeneration(name string) bool {
	if len(name) != len("gen-")+32+len(".nql") || name[:4] != "gen-" || name[len(name)-4:] != ".nql" {
		return false
	}
	for _, c := range name[4 : len(name)-4] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func (s *Store) checkOpen() error {
	if s.closed || s.lock == nil {
		return fmt.Errorf("save store is closed")
	}
	return nil
}

func (s *Store) stage(stage, generation string, head core.Revision, uncertain bool, fn func() error) error {
	if s.ops.before != nil {
		if err := s.ops.before(stage); err != nil {
			return &StageError{Stage: stage, Uncertain: uncertain, Generation: generation, Head: head, Err: err}
		}
	}
	if err := fn(); err != nil {
		return &StageError{Stage: stage, Uncertain: uncertain, Generation: generation, Head: head, Err: err}
	}
	return nil
}

func (s *Store) temporary(prefix, generation string, head core.Revision, write func(io.Writer) error) (path string, err error) {
	var f syncFile
	if err = s.stage(prefix+"-create", generation, head, false, func() error {
		var e error
		f, e = s.ops.createTemp(s.dir, "."+prefix+"-pending-*")
		return e
	}); err != nil {
		return "", err
	}
	path = f.Name()
	closed := false
	defer func() {
		if !closed {
			if e := f.Close(); e != nil {
				err = errors.Join(err, &StageError{Stage: prefix + "-close", Generation: generation, Head: head, Err: e})
			}
		}
		if err != nil {
			_ = os.Remove(path)
		}
	}()
	buffer := bufio.NewWriterSize(f, 64<<10)
	if err = s.stage(prefix+"-write", generation, head, false, func() error { return write(buffer) }); err != nil {
		return path, err
	}
	if err = s.stage(prefix+"-flush", generation, head, false, func() error { return s.ops.flush(buffer) }); err != nil {
		return path, err
	}
	if err = s.stage(prefix+"-sync", generation, head, false, f.Sync); err != nil {
		return path, err
	}
	err = s.stage(prefix+"-close", generation, head, false, func() error { closed = true; return f.Close() })
	return path, err
}

// Save acknowledges only after complete generation and HEAD publication plus
// directory synchronization. Checkpoints and session records must not be
// concurrently mutated by their caller. No World lock is held during this work.
func (s *Store) Save(cp core.Checkpoint, session SessionState) (SaveResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked(cp, session)
}
func (s *Store) saveLocked(cp core.Checkpoint, session SessionState) (SaveResult, error) {
	if err := s.checkOpen(); err != nil {
		return SaveResult{}, err
	}
	if session.Campaign != s.campaign {
		return SaveResult{}, fmt.Errorf("save campaign identity mismatch")
	}
	if session.Attachment != nil {
		if _, err := s.readAttachment(session.Attachment); err != nil {
			return SaveResult{}, err
		}
	}
	if !s.fresh && s.selected == nil {
		return SaveResult{}, fmt.Errorf("load or explicitly recover the campaign before saving")
	}
	if s.selected != nil && (cp.Head < s.selected.Info.Head || session.Next < s.next) {
		return SaveResult{}, fmt.Errorf("checkpoint or allocation precedes the selected session")
	}
	if s.selected != nil && sessionAllocation(session) != s.allocation {
		return SaveResult{}, fmt.Errorf("ordinary save cannot change allocation namespace")
	}
	if s.selected != nil {
		count := s.selected.Info.Count
		if count > len(cp.History) {
			return SaveResult{}, fmt.Errorf("checkpoint omits selected history prefix")
		}
		prefix, e := historyPrefixDigest(cp.History[:count])
		if e != nil {
			return SaveResult{}, e
		}
		if prefix != s.prefixDigest {
			return SaveResult{}, fmt.Errorf("checkpoint does not extend the selected history; explicit recovery is required to change its basis")
		}
	}
	nextPrefix, err := historyPrefixDigest(cp.History)
	if err != nil {
		return SaveResult{}, err
	}
	result, err := s.publishLocked(cp.Head, func(w io.Writer) (ArchiveInfo, error) {
		info, e := Write(w, cp, session)
		if e == nil && s.selected != nil && info.GenesisDigest != s.selected.Info.GenesisDigest {
			e = fmt.Errorf("save genesis differs from the selected campaign")
		}
		return info, e
	})
	if err != nil {
		return SaveResult{}, err
	}
	s.prefixDigest = nextPrefix
	s.next = session.Next
	s.allocation = sessionAllocation(session)
	s.incremental = nil
	return result, nil
}

// Both archive representations use exactly the same publication and uncertainty stages.
func (s *Store) publishLocked(head core.Revision, write func(io.Writer) (ArchiveInfo, error)) (SaveResult, error) {
	var identity [16]byte
	if _, err := rand.Read(identity[:]); err != nil {
		return SaveResult{}, err
	}
	generation := "gen-" + hex.EncodeToString(identity[:]) + ".nql"
	final := filepath.Join(s.dir, generation)
	if _, err := os.Lstat(final); !errors.Is(err, os.ErrNotExist) {
		return SaveResult{}, fmt.Errorf("generation already exists or cannot be checked: %v", err)
	}
	var info ArchiveInfo
	temp, err := s.temporary("archive", generation, head, func(w io.Writer) error {
		var e error
		info, e = write(w)
		return e
	})
	if err != nil {
		return SaveResult{}, err
	}
	if err := s.stage("generation-publish", generation, head, false, func() error { return s.ops.rename(temp, final) }); err != nil {
		return SaveResult{}, err
	}
	if err := s.stage("generation-directory-sync", generation, head, false, func() error { return s.ops.syncDir(s.dir) }); err != nil {
		return SaveResult{}, err
	}
	selected := manifest{Generation: generation, Info: info}
	if s.selected != nil {
		selected.Previous = s.selected.Generation
	}
	if s.forkPrevious != "" {
		selected.Previous = s.forkPrevious
	}
	data, err := encodeHead(selected)
	if err != nil {
		return SaveResult{}, err
	}
	headTemp, err := s.temporary("head", generation, head, func(w io.Writer) error {
		n, e := w.Write(data)
		if e == nil && n != len(data) {
			e = io.ErrShortWrite
		}
		return e
	})
	if err != nil {
		return SaveResult{}, err
	}
	// A real rename error can leave its delivery uncertain to the caller. Be
	// conservative once the selected-head publication syscall is attempted.
	if err := s.stage("head-publish", generation, head, true, func() error { return s.ops.rename(headTemp, filepath.Join(s.dir, "HEAD")) }); err != nil {
		return SaveResult{}, err
	}
	if err := s.stage("head-directory-sync", generation, head, true, func() error { return s.ops.syncDir(s.dir) }); err != nil {
		return SaveResult{}, err
	}
	s.selected = &selected
	s.fresh = false
	return SaveResult{Generation: generation, Head: head, Bytes: info.Bytes, SHA256: info.SHA256}, nil
}

func validateManifest(m manifest, campaign string) error {
	if !validGeneration(m.Generation) || (m.Previous != "" && !validGeneration(m.Previous)) || m.Previous == m.Generation {
		return fmt.Errorf("invalid selected generation basename")
	}
	if m.Info.Campaign != campaign || m.Info.Bytes <= 0 || m.Info.Bytes > MaxBytes || !validDigest(m.Info.SHA256) || !validDigest(m.Info.StateDigest) {
		return fmt.Errorf("invalid selected archive identity, byte count or digest")
	}
	format := m.Info.Format
	if format == SegmentedFormatVersion {
		format = FormatVersion
	}
	return validateMeta(archiveMetadata{Format: format, Semantics: m.Info.Semantics, RNG: m.Info.RNG, Campaign: m.Info.Campaign, GenesisDigest: m.Info.GenesisDigest, Count: m.Info.Count, Head: m.Info.Head})
}

func (s *Store) readGeneration(generation string) (*Loaded, error) {
	if !validGeneration(generation) {
		return nil, fmt.Errorf("recovery requires an exact generated basename")
	}
	f, err := openRegular(filepath.Join(s.dir, generation))
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || info.Size() <= 0 || info.Size() > MaxBytes {
		_ = f.Close()
		return nil, fmt.Errorf("invalid generation size: %v", err)
	}
	loaded, readErr := s.readStoredGeneration(f)
	closeErr := f.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return nil, err
	}
	if loaded.Info.Campaign != s.campaign || loaded.Info.Bytes != info.Size() {
		return nil, fmt.Errorf("generation campaign or physical size mismatch")
	}
	loaded.Generation = generation
	return loaded, nil
}

// Load restores exactly HEAD's selected generation. Missing or invalid selected
// data is an error; this method never silently chooses an older checkpoint.
func (s *Store) Load() (*Loaded, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked()
}
func (s *Store) loadLocked() (*Loaded, error) {
	if err := s.checkOpen(); err != nil {
		return nil, err
	}
	f, err := openRegular(filepath.Join(s.dir, "HEAD"))
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(f, MaxHeadBytes+1))
	closeErr := f.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return nil, err
	}
	var selected manifest
	if err := decodeHead(data, &selected); err != nil {
		return nil, err
	}
	if err := validateManifest(selected, s.campaign); err != nil {
		return nil, err
	}
	loaded, err := s.readGeneration(selected.Generation)
	if err != nil {
		return nil, err
	}
	if loaded.Info != selected.Info {
		return nil, fmt.Errorf("selected manifest disagrees with generation bytes or metadata")
	}
	prefix, e := historyPrefixDigest(loaded.World.History())
	if e != nil {
		return nil, e
	}
	s.selected = &selected
	s.incremental = nil
	s.prefixDigest = prefix
	s.next = loaded.Session.Next
	s.allocation = sessionAllocation(loaded.Session)
	s.fresh = false
	return loaded, nil
}

// Recover validates an explicitly named immutable generation without changing
// HEAD. The host must explain the chosen revision and possible unsaved loss;
// a later explicit Save publishes continuation from this recovered checkpoint.
func (s *Store) Recover(generation string) (*Loaded, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkOpen(); err != nil {
		return nil, err
	}
	loaded, err := s.readGeneration(generation)
	if err != nil {
		return nil, err
	}
	loaded.Recovered = true
	prefix, e := historyPrefixDigest(loaded.World.History())
	if e != nil {
		return nil, e
	}
	s.incremental = nil
	s.prefixDigest = prefix
	s.selected = &manifest{Generation: generation, Info: loaded.Info}
	s.next = loaded.Session.Next
	s.allocation = sessionAllocation(loaded.Session)
	s.fresh = false
	return loaded, nil
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.lock == nil {
		return nil
	}
	err := unlockSession(s.lock)
	err = errors.Join(err, s.lock.Close())
	s.lock = nil
	return err
}

// A selected checkpoint is a retained history commitment within this locked
// local session. Ordinary saves extend it; only explicit recovery changes basis.
func historyPrefixDigest(history []core.Batch) (string, error) {
	if len(history) > core.MaxArchiveBatches {
		return "", fmt.Errorf("history exceeds native capacity")
	}
	sum := sha256.New()
	for _, b := range history {
		if len(b.Events) > 2*core.MaxOperations || len(b.Request.Operations) > core.MaxOperations {
			return "", fmt.Errorf("history record exceeds work bounds")
		}
		data, e := json.Marshal(b)
		if e != nil {
			return "", e
		}
		if len(data) > MaxRecordBytes-64 {
			return "", fmt.Errorf("history record exceeds byte bound")
		}
		_, _ = sum.Write(data)
		_, _ = sum.Write([]byte{'\n'})
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}
