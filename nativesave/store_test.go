//go:build linux

package nativesave

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MoonlightByte/firelight/core"
)

func saveTestRoot(t *testing.T) string {
	t.Helper()
	if root := os.Getenv("NQL_NATIVE_TEST_ROOT"); root != "" {
		dir, err := os.MkdirTemp(root, "nativesave-test-*")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := os.RemoveAll(dir); err != nil {
				t.Error(err)
			}
		})
		t.Logf("actual filesystem test root: %s", dir)
		return dir
	}
	dir := t.TempDir()
	t.Logf("actual filesystem test root: %s", dir)
	return dir
}

func saveTestStore(t *testing.T, root, campaign string, create bool) *Store {
	t.Helper()
	store, err := Acquire(root, campaign, create)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	return store
}

func TestStoreSaveLoadLockAndExplicitRecovery(t *testing.T) {
	root := saveTestRoot(t)
	w, s := saveTestFixture(t)
	store := saveTestStore(t, root, s.Campaign, true)
	first, err := store.Save(w.ExportCheckpoint(), s)
	if err != nil {
		t.Fatal(err)
	}
	if first.Head != 3 || !validGeneration(first.Generation) {
		t.Fatal("invalid first saved identity", first)
	}
	if other, err := Acquire(root, s.Campaign, false); err == nil {
		_ = other.Close()
		t.Fatal("second active session acquired same campaign")
	}
	if other, err := Acquire(root, s.Campaign, true); err == nil {
		_ = other.Close()
		t.Fatal("new campaign replaced existing target")
	}
	saveTestCommit(t, w, &s, core.Operation{Heal: &core.ResourceOp{Character: "hero", Resource: "hp", Amount: 1}})
	second, err := store.Save(w.ExportCheckpoint(), s)
	if err != nil {
		t.Fatal(err)
	}
	if first.Generation == second.Generation || second.Head != 4 {
		t.Fatal("generation was reused")
	}
	loaded, err := store.Load()
	if err != nil || loaded.Recovered || loaded.Generation != second.Generation || !reflect.DeepEqual(w.ExportCheckpoint(), loaded.World.ExportCheckpoint()) {
		t.Fatal("selected checkpoint did not load", err)
	}
	headPath := filepath.Join(root, s.Campaign, "HEAD")
	headBefore, err := os.ReadFile(headPath)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := store.Recover(first.Generation)
	if err != nil || !recovered.Recovered || recovered.World.Revision() != 3 {
		t.Fatal("explicit prior recovery failed", err)
	}
	headAfter, err := os.ReadFile(headPath)
	if err != nil || !bytes.Equal(headBefore, headAfter) {
		t.Fatal("recovery silently changed selected HEAD", err)
	}
	if _, err := store.Recover("../" + first.Generation); err == nil {
		t.Fatal("archive path traversal accepted")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); err == nil {
		t.Fatal("closed store remained usable")
	}
	resumed := saveTestStore(t, root, s.Campaign, false)
	loaded, err = resumed.Load()
	if err != nil || loaded.World.Revision() != 4 {
		t.Fatal("new session failed to resume selected checkpoint", err)
	}
	if _, err := os.Lstat(filepath.Join(root, s.Campaign, ".lock")); err != nil {
		t.Fatal("persistent lock inode was removed", err)
	}
}

func TestStorePublicationFailuresRetainPriorCheckpoint(t *testing.T) {
	for _, stage := range []string{
		"archive-create", "archive-write", "archive-flush", "archive-sync", "archive-close",
		"generation-publish", "generation-directory-sync", "head-create", "head-write", "head-flush", "head-sync", "head-close", "head-publish", "head-directory-sync",
	} {
		t.Run(stage, func(t *testing.T) {
			root := saveTestRoot(t)
			w, s := saveTestFixture(t)
			store := saveTestStore(t, root, s.Campaign, true)
			first, err := store.Save(w.ExportCheckpoint(), s)
			if err != nil {
				t.Fatal(err)
			}
			saveTestCommit(t, w, &s, core.Operation{Heal: &core.ResourceOp{Character: "hero", Resource: "hp", Amount: 1}})
			cp := w.ExportCheckpoint()
			injected := errors.New("injected " + stage)
			store.ops.before = func(at string) error {
				if at == stage {
					return injected
				}
				return nil
			}
			result, err := store.Save(cp, s)
			var phase *StageError
			if err == nil || !errors.Is(err, injected) || !errors.As(err, &phase) || phase.Stage != stage || result.Generation != "" {
				t.Fatal("save failure became acknowledgment or lost stage", result, err)
			}
			wantUncertain := stage == "head-publish" || stage == "head-directory-sync"
			if phase.Uncertain != wantUncertain {
				t.Fatal("wrong publication uncertainty", phase)
			}
			if !reflect.DeepEqual(cp, w.ExportCheckpoint()) {
				t.Fatal("save failure changed gameplay authority")
			}
			store.ops = realFileOps()
			prior, err := store.Recover(first.Generation)
			if err != nil || prior.World.Revision() != 3 {
				t.Fatal("known-good prior generation was damaged", err)
			}
			selected, err := store.Load()
			if err != nil {
				t.Fatal("no valid selected checkpoint after injected boundary failure", err)
			}
			want := core.Revision(3)
			if stage == "head-directory-sync" {
				want = 4
			}
			if selected.World.Revision() != want {
				t.Fatal("mixed or unexpected selected generation", selected.World.Revision(), want)
			}
		})
	}
}

type failingSaveFile struct {
	syncFile
	failure string
	err     error
}

func (f *failingSaveFile) Write(p []byte) (int, error) {
	if f.failure == "write" {
		n, _ := f.syncFile.Write(p[:len(p)/2])
		return n, f.err
	}
	return f.syncFile.Write(p)
}
func (f *failingSaveFile) Sync() error {
	if f.failure == "sync" {
		return f.err
	}
	return f.syncFile.Sync()
}
func (f *failingSaveFile) Close() error {
	err := f.syncFile.Close()
	if f.failure == "close" {
		return errors.Join(err, f.err)
	}
	return err
}

func TestStoreHandlesInjectedIOErrorsAndShortWrites(t *testing.T) {
	for _, failure := range []string{"write", "flush", "sync", "close", "rename"} {
		t.Run(failure, func(t *testing.T) {
			root := saveTestRoot(t)
			w, s := saveTestFixture(t)
			store := saveTestStore(t, root, s.Campaign, true)
			first, err := store.Save(w.ExportCheckpoint(), s)
			if err != nil {
				t.Fatal(err)
			}
			injected := errors.New("simulated syscall " + failure)
			if failure == "flush" {
				store.ops.flush = func(*bufio.Writer) error { return injected }
			} else if failure == "rename" {
				store.ops.rename = func(string, string) error { return injected }
			} else {
				store.ops.createTemp = func(dir, pattern string) (syncFile, error) {
					f, err := os.CreateTemp(dir, pattern)
					if err != nil {
						return nil, err
					}
					return &failingSaveFile{syncFile: f, failure: failure, err: injected}, nil
				}
			}
			if _, err := store.Save(w.ExportCheckpoint(), s); !errors.Is(err, injected) {
				t.Fatal("underlying I/O error was lost", err)
			}
			store.ops = realFileOps()
			loaded, err := store.Load()
			if err != nil || loaded.Generation != first.Generation {
				t.Fatal("failed I/O replaced previous checkpoint", err)
			}
		})
	}
}

func TestStoreRejectsCorruptSelectedDataWithoutFallback(t *testing.T) {
	root := saveTestRoot(t)
	w, s := saveTestFixture(t)
	store := saveTestStore(t, root, s.Campaign, true)
	first, err := store.Save(w.ExportCheckpoint(), s)
	if err != nil {
		t.Fatal(err)
	}
	saveTestCommit(t, w, &s, core.Operation{Heal: &core.ResourceOp{Character: "hero", Resource: "hp", Amount: 1}})
	second, err := store.Save(w.ExportCheckpoint(), s)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.dir, second.Generation)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data[:len(data)/2], 0600); err != nil {
		t.Fatal(err)
	}
	if loaded, err := store.Load(); err == nil || loaded != nil {
		t.Fatal("truncated selected generation silently fell back")
	}
	if restored, err := store.Recover(first.Generation); err != nil || restored.World.Revision() != 3 {
		t.Fatal("explicit intact generation recovery failed", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	headPath := filepath.Join(store.dir, "HEAD")
	head, err := os.ReadFile(headPath)
	if err != nil {
		t.Fatal(err)
	}
	var selected manifest
	if err := decodeHead(head, &selected); err != nil {
		t.Fatal(err)
	}
	selected.Info.SHA256 = strings.Repeat("0", 64)
	bad, err := encodeHead(selected)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(headPath, bad, 0600); err != nil {
		t.Fatal(err)
	}
	if loaded, err := store.Load(); err == nil || loaded != nil {
		t.Fatal("manifest checksum contradiction accepted")
	}
	if err := os.Remove(headPath); err != nil {
		t.Fatal(err)
	}
	if loaded, err := store.Load(); err == nil || loaded != nil {
		t.Fatal("missing HEAD silently selected a generation")
	}
	if err := os.Symlink(first.Generation, headPath); err != nil {
		t.Fatal(err)
	}
	if loaded, err := store.Load(); err == nil || loaded != nil {
		t.Fatal("symlink HEAD followed")
	}
}

// Invoked only by TestStoreProcessInterruption. This is an actual second process
// and real filesystem publication, with a test-only pause before a named syscall.
func TestStoreSubprocessHelper(t *testing.T) {
	if os.Getenv("NQL_NATIVE_HELPER") != "interrupt" {
		t.Skip("subprocess helper")
	}
	store, err := Acquire(os.Getenv("NQL_NATIVE_HELPER_ROOT"), "codec-test", false)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	s := loaded.Session
	saveTestCommit(t, loaded.World, &s, core.Operation{Heal: &core.ResourceOp{Character: "hero", Resource: "hp", Amount: 1}})
	store.ops.before = func(stage string) error {
		if stage == os.Getenv("NQL_NATIVE_HELPER_STAGE") {
			fmt.Println("NATIVE_SAVE_BOUNDARY_REACHED")
			_, err := bufio.NewReader(os.Stdin).ReadByte()
			return err
		}
		return nil
	}
	if _, err := store.Save(loaded.World.ExportCheckpoint(), s); err != nil {
		t.Fatal(err)
	}
}

func TestStoreProcessInterruption(t *testing.T) {
	for _, stage := range []string{"archive-sync", "generation-directory-sync", "head-directory-sync"} {
		t.Run(stage, func(t *testing.T) {
			root := saveTestRoot(t)
			w, s := saveTestFixture(t)
			store := saveTestStore(t, root, s.Campaign, true)
			first, err := store.Save(w.ExportCheckpoint(), s)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestStoreSubprocessHelper$", "-test.v")
			cmd.Env = append(os.Environ(), "NQL_NATIVE_HELPER=interrupt", "NQL_NATIVE_HELPER_ROOT="+root, "NQL_NATIVE_HELPER_STAGE="+stage)
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			stdin, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			defer stdin.Close()
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = cmd.Process.Kill() }()
			scanner := bufio.NewScanner(stdout)
			reached := false
			for scanner.Scan() {
				if scanner.Text() == "NATIVE_SAVE_BOUNDARY_REACHED" {
					reached = true
					break
				}
			}
			if !reached {
				_ = cmd.Wait()
				t.Fatalf("child did not reach boundary: %v %s", scanner.Err(), stderr.String())
			}
			if competing, err := Acquire(root, s.Campaign, false); err == nil {
				_ = competing.Close()
				t.Fatal("competing process acquired active campaign")
			}
			if err := cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			if err := cmd.Wait(); err == nil {
				t.Fatal("interrupted child unexpectedly exited successfully")
			}
			resumed := saveTestStore(t, root, s.Campaign, false)
			loaded, err := resumed.Load()
			if err != nil {
				t.Fatal("checkpoint invalid after actual process death", err)
			}
			want := core.Revision(3)
			if stage == "head-directory-sync" {
				want = 4
			}
			if loaded.World.Revision() != want {
				t.Fatal("wrong selected generation after process death", loaded.World.Revision(), want)
			}
			if prior, err := resumed.Recover(first.Generation); err != nil || prior.World.Revision() != 3 {
				t.Fatal("prior acknowledged checkpoint lost", err)
			}
		})
	}
}

func TestStoreSessionMustLoadBeforeSaving(t *testing.T) {
	root := saveTestRoot(t)
	w, s := saveTestFixture(t)
	store := saveTestStore(t, root, s.Campaign, true)
	if _, err := store.Save(w.ExportCheckpoint(), s); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	resumed := saveTestStore(t, root, s.Campaign, false)
	if _, err := resumed.Save(w.ExportCheckpoint(), s); err == nil {
		t.Fatal("unloaded session could overwrite existing selected history")
	}
	if _, err := resumed.Load(); err != nil {
		t.Fatal(err)
	}
	s.LastAttempt = &AttemptRef{RequestID: s.Campaign + ":request:4", Committed: false}
	s.Next = 5
	if _, err := resumed.Save(w.ExportCheckpoint(), s); err != nil {
		t.Fatal("rejected attempt allocation could not be checkpointed", err)
	}
	s.Next = 4
	s.LastAttempt = nil
	if _, err := resumed.Save(w.ExportCheckpoint(), s); err == nil {
		t.Fatal("save rewound previously saved allocation")
	}
}

var _ io.Writer = (*failingSaveFile)(nil)

func TestStoreNormalSavePreservesSelectedHistoryPrefix(t *testing.T) {
	root := saveTestRoot(t)
	b := saveTestBlueprint()
	a, e := core.NewWorld(b)
	if e != nil {
		t.Fatal(e)
	}
	fork, e := core.NewWorld(b)
	if e != nil {
		t.Fatal(e)
	}
	state := SessionState{Campaign: "prefix", Next: 1}
	other := state
	first := saveTestCommit(t, a, &state, core.Operation{Heal: &core.ResourceOp{Character: "hero", Resource: "hp", Amount: 1}})
	saveTestCommit(t, fork, &other, core.Operation{Damage: &core.ResourceOp{Character: "hero", Resource: "hp", Amount: 1}})
	store := saveTestStore(t, root, state.Campaign, true)
	old, e := store.Save(a.ExportCheckpoint(), state)
	if e != nil {
		t.Fatal(e)
	}
	before, e := os.ReadFile(filepath.Join(root, state.Campaign, "HEAD"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e := store.Save(fork.ExportCheckpoint(), other); e == nil {
		t.Fatal("same-head divergent history replaced acknowledged receipt")
	}
	after, e := os.ReadFile(filepath.Join(root, state.Campaign, "HEAD"))
	if e != nil || !bytes.Equal(before, after) {
		t.Fatal("rejected fork changed selected manifest", e)
	}
	loaded, e := store.Load()
	if e != nil {
		t.Fatal(e)
	}
	receipt, e := loaded.World.Execute(first.Request)
	if e != nil || !reflect.DeepEqual(first, receipt) {
		t.Fatal("old acknowledged receipt lost", e)
	}
	saveTestCommit(t, a, &state, core.Operation{Heal: &core.ResourceOp{Character: "hero", Resource: "hp", Amount: 1}})
	if _, e := store.Save(a.ExportCheckpoint(), state); e != nil {
		t.Fatal("valid extension rejected", e)
	}
	recovered, e := store.Recover(old.Generation)
	if e != nil {
		t.Fatal(e)
	}
	saveTestCommit(t, recovered.World, &recovered.Session, core.Operation{Damage: &core.ResourceOp{Character: "hero", Resource: "hp", Amount: 1}})
	if _, e := store.Save(recovered.World.ExportCheckpoint(), recovered.Session); e != nil {
		t.Fatal("explicit recovery could not establish earlier valid basis", e)
	}
}
