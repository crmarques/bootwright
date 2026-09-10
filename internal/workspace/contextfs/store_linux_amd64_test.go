//go:build linux && amd64

package contextfs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

func fixture(t *testing.T) (*Store, desiredstate.Sources) {
	t.Helper()
	base := t.TempDir()
	input := filepath.Join(base, "input")
	if err := os.Mkdir(input, 0700); err != nil {
		t.Fatal(err)
	}
	return New(testOptions(filepath.Join(base, "state"))), desiredstate.Sources{Roots: []string{input}, Files: []desiredstate.SourceFile{desiredstate.NewSourceFile(filepath.Join(input, "environment.yaml"), []byte("version: original\n"))}, Markers: []desiredstate.SourceFile{}}
}

func testOptions(root string) Options {
	return Options{Root: root, Owner: &Ownership{UID: uint32(os.Geteuid()), GID: uint32(os.Getegid())}}
}

func publish(t *testing.T, store *Store, name string, sources desiredstate.Sources) contexts.Record {
	t.Helper()
	var result contexts.Record
	err := store.Transact(context.Background(), true, sources.Roots, func(tx contexts.Transaction) error {
		registry := tx.Registry()
		for _, record := range registry.Contexts {
			if record.Name == name {
				result = record
			}
		}
		if result.ID == "" {
			var err error
			result, err = tx.Reserve(context.Background(), name, sources.Roots[0], contexts.DefaultConfiguration(name).Canonical())
			if err != nil {
				return err
			}
		}
		if _, err := tx.MutationState(context.Background(), result.ID); err != nil {
			return err
		}
		revision, err := tx.Publish(context.Background(), result.ID, sources.Roots[0], sources)
		if err != nil {
			return err
		}
		registry = tx.Registry()
		result.Revision = revision
		result.Mode = contexts.Ready
		for i := range registry.Contexts {
			if registry.Contexts[i].ID == result.ID {
				registry.Contexts[i] = result
			}
		}
		return tx.Commit(context.Background(), registry)
	})
	if err != nil {
		t.Fatalf("publication failed: %#v", err)
	}
	return result
}

func expectState(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected context.state refusal")
	}
	var failure *desiredstate.Failure
	if !errors.As(err, &failure) || len(failure.Diagnostics) != 1 || failure.Diagnostics[0].Code != "context.state" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func expectMissingRegistry(t *testing.T, err error) {
	t.Helper()
	diagnostics := desiredstate.DiagnosticsOf(err)
	if len(diagnostics) != 1 || diagnostics[0].Code != "context.state" || diagnostics[0].Message != missingRegistryMessage || diagnostics[0].Remediation != storeRecoveryRemediation {
		t.Fatalf("unexpected missing-registry diagnostic: %#v", diagnostics)
	}
}

func expectPendingRegistry(t *testing.T, err error) {
	t.Helper()
	diagnostics := desiredstate.DiagnosticsOf(err)
	if len(diagnostics) != 1 || diagnostics[0].Code != "context.state" || diagnostics[0].Message != pendingRegistryMessage || diagnostics[0].Remediation != pendingRegistryRemediation {
		t.Fatalf("unexpected pending-registry diagnostic: %#v", diagnostics)
	}
}

func expectInconsistentEmptyRegistry(t *testing.T, err error) {
	t.Helper()
	diagnostics := desiredstate.DiagnosticsOf(err)
	if len(diagnostics) != 1 || diagnostics[0].Code != "context.state" || diagnostics[0].Message != "registry.json is empty but the context store is not" || diagnostics[0].Remediation != storeRecoveryRemediation {
		t.Fatalf("unexpected empty-registry diagnostic: %#v", diagnostics)
	}
}

func writePrivate(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestImmutableInputAndReadOnlyLifecycleBoundary(t *testing.T) {
	store, sources := fixture(t)
	sources.Files = append(sources.Files, desiredstate.NewSourceFile(filepath.Join(sources.Roots[0], "excluded.yaml"), []byte{0xff, 0xfe}))
	record := publish(t, store, "example", sources)
	mutation := filepath.Join(store.options.Root, "contexts", record.Name, "state", "mutation.json")
	if err := os.Remove(mutation); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(mutation, 0600); err != nil {
		t.Fatal(err)
	}
	writePrivate(t, filepath.Join(sources.Roots[0], "environment.yaml"), []byte("changed: original source\n"))
	if err := os.Mkdir(filepath.Join(sources.Roots[0], "secrets"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(sources.Roots[0], "secrets", "payload.yaml"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(store.options.Root, "registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		view, err := store.View(context.Background())
		if err != nil || len(view.Contexts) != 1 || view.Contexts[0].Name != "example" {
			t.Fatalf("view: %v", err)
		}
		input, err := store.ReadInputs(context.Background(), "example", "")
		if err != nil {
			t.Fatal(err)
		}
		if len(input.Files) != 2 || !bytes.Equal(input.Files[0].Bytes(), sources.Files[0].Bytes()) || !bytes.Equal(input.Files[1].Bytes(), sources.Files[1].Bytes()) || input.Files[0].Path() != sources.Files[0].Path() {
			t.Fatal("immutable bytes or original provenance changed")
		}
	}
	after, err := os.ReadFile(filepath.Join(store.options.Root, "registry.json"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("read-only inspection changed registry")
	}
}

func TestMissingStoreAndInputPreflight(t *testing.T) {
	store, sources := fixture(t)
	view, err := store.View(context.Background())
	if err != nil || view.Version != 2 || len(view.Contexts) != 0 {
		t.Fatalf("absent list: %v", err)
	}
	if _, err := os.Stat(store.options.Root); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("read-only lookup created state")
	}
	for _, root := range []string{sources.Roots[0], filepath.Join(sources.Roots[0], "state")} {
		unsafe := New(testOptions(root))
		expectState(t, unsafe.CheckInputDirectory(context.Background(), sources.Roots[0]))
		expectState(t, unsafe.Transact(context.Background(), true, sources.Roots, func(contexts.Transaction) error { t.Fatal("callback reached unsafe root"); return nil }))
	}
	if err := store.CheckInputDirectory(context.Background(), sources.Roots[0]); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyRootViewRemainsReadOnly(t *testing.T) {
	store, _ := fixture(t)
	if err := os.Mkdir(store.options.Root, 0700); err != nil {
		t.Fatal(err)
	}
	view, err := store.View(context.Background())
	if err != nil || !reflect.DeepEqual(view, emptyRegistry()) {
		t.Fatalf("empty root view: %#v %v", view, err)
	}
	entries, err := os.ReadDir(store.options.Root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("empty root view changed state: %#v %v", entries, err)
	}
}

func TestEmptyRegistryToleratesVerifiedUnpublishedRegistryStage(t *testing.T) {
	store, _ := fixture(t)
	checkpoints := 0
	store.fail = func(point string) error {
		if point == "before-registry-rename" {
			checkpoints++
			if checkpoints == 2 {
				return errors.New("synthetic reservation publication interruption")
			}
		}
		return nil
	}
	reserve := func() error {
		return store.Transact(context.Background(), true, nil, func(tx contexts.Transaction) error {
			_, err := tx.Reserve(context.Background(), "pending", "", contexts.DefaultConfiguration("pending").Canonical())
			return err
		})
	}
	expectState(t, reserve())
	store.fail = nil
	view, err := store.View(context.Background())
	if err != nil || !reflect.DeepEqual(view, emptyRegistry()) {
		t.Fatalf("unpublished registry stage changed the visible registry: %#v %v", view, err)
	}
	entries, err := os.ReadDir(store.options.Root)
	if err != nil {
		t.Fatal(err)
	}
	pending := 0
	for _, entry := range entries {
		if pendingInitialRegistryName(entry.Name()) {
			pending++
		}
	}
	if pending != 1 {
		t.Fatalf("interruption retained %d pending registries, want 1", pending)
	}
	if err := reserve(); err != nil {
		t.Fatal(err)
	}
	view, err = store.View(context.Background())
	if err != nil || len(view.Contexts) != 1 || view.Contexts[0].Mode != contexts.Initializing {
		t.Fatalf("retry did not publish initialization intent: %#v %v", view, err)
	}
}

func TestInterruptedInitialRegistryPublicationRecoversOnlyDuringCreate(t *testing.T) {
	store, _ := fixture(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^TestInitialRegistryCrashHelper$")
	command.Env = append(os.Environ(), "BOOTWRIGHT_INITIAL_REGISTRY_TEST_ROOT="+store.options.Root)
	output, err := command.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 78 {
		t.Fatalf("initial registry crash helper: %v %s", err, output)
	}

	entries, err := os.ReadDir(store.options.Root)
	if err != nil || len(entries) != 1 || !pendingInitialRegistryName(entries[0].Name()) {
		t.Fatalf("unexpected interrupted initial state: %v %#v", err, entries)
	}
	pending := filepath.Join(store.options.Root, entries[0].Name())
	before, err := os.ReadFile(pending)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.View(context.Background()); err == nil {
		t.Fatal("read-only view accepted an unpublished initial registry")
	} else {
		expectPendingRegistry(t, err)
	}
	after, err := os.ReadFile(pending)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("read-only view changed the pending initial registry")
	}
	if _, err := os.Stat(filepath.Join(store.options.Root, "registry.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("read-only view published the initial registry")
	}
	callbackCalled := false
	if err := store.Transact(context.Background(), false, nil, func(contexts.Transaction) error {
		callbackCalled = true
		return nil
	}); err == nil {
		t.Fatal("non-create transaction accepted an unpublished initial registry")
	} else {
		expectPendingRegistry(t, err)
	}
	if callbackCalled {
		t.Fatal("non-create transaction callback ran before registry publication")
	}
	if _, err := os.Stat(filepath.Join(store.options.Root, "registry.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("non-create transaction published the initial registry")
	}

	callbackCalled = false
	if err := store.Transact(context.Background(), true, nil, func(tx contexts.Transaction) error {
		callbackCalled = true
		registry := tx.Registry()
		if !reflect.DeepEqual(registry, contexts.Registry{Version: 2, Identities: []contexts.Identity{}, Contexts: []contexts.Record{}}) {
			t.Fatalf("recovered registry is not empty and canonical: %#v", registry)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !callbackCalled {
		t.Fatal("create transaction did not continue after initial registry recovery")
	}
	view, err := store.View(context.Background())
	if err != nil || !reflect.DeepEqual(view, emptyRegistry()) {
		t.Fatalf("recovered registry cannot be read: %#v %v", view, err)
	}
	entries, err = os.ReadDir(store.options.Root)
	if err != nil || len(entries) != 1 || entries[0].Name() != "registry.json" {
		t.Fatalf("pending initial registry was not atomically consumed: %v %#v", err, entries)
	}
}

func TestInitialRegistryCrashHelper(t *testing.T) {
	root := os.Getenv("BOOTWRIGHT_INITIAL_REGISTRY_TEST_ROOT")
	if root == "" {
		t.Skip("subprocess helper")
	}
	store := New(testOptions(root))
	store.fail = func(point string) error {
		if point == "before-registry-rename" {
			os.Exit(78)
		}
		return nil
	}
	if err := store.Transact(context.Background(), true, nil, func(contexts.Transaction) error {
		t.Fatal("initial transaction callback ran before registry publication")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	t.Fatal("initial registry crash checkpoint was not reached")
}

type rootEntrySnapshot struct {
	name       string
	mode       uint32
	device     uint64
	inode      uint64
	links      uint64
	size       int64
	modified   syscall.Timespec
	changed    syscall.Timespec
	linkTarget string
	data       []byte
}

func snapshotRootEntries(t *testing.T, root string) []rootEntrySnapshot {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	result := make([]rootEntrySnapshot, 0, len(entries))
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			t.Fatal("state entry has no syscall identity")
		}
		snapshot := rootEntrySnapshot{name: entry.Name(), mode: stat.Mode, device: uint64(stat.Dev), inode: stat.Ino, links: uint64(stat.Nlink), size: stat.Size, modified: stat.Mtim, changed: stat.Ctim}
		if info.Mode().IsRegular() {
			snapshot.data, err = os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
		}
		if info.Mode()&os.ModeSymlink != 0 {
			snapshot.linkTarget, err = os.Readlink(path)
			if err != nil {
				t.Fatal(err)
			}
		}
		result = append(result, snapshot)
	}
	return result
}

func TestInitialRegistryRecoveryRefusesUnknownOrUnsafeState(t *testing.T) {
	const pending = "pending-00000000000000000000000000000000.json"
	for _, kind := range []string{"unrelated", "wrong-name", "partial", "noncanonical", "extra", "multiple", "mode", "hardlink", "symlink", "fifo", "directory"} {
		t.Run(kind, func(t *testing.T) {
			store, _ := fixture(t)
			if err := os.Mkdir(store.options.Root, 0700); err != nil {
				t.Fatal(err)
			}
			canonical, err := encodeRecord(emptyRegistry(), maxRegistry)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(store.options.Root, pending)
			switch kind {
			case "unrelated":
				writePrivate(t, filepath.Join(store.options.Root, "legacy-state"), []byte("retained\n"))
			case "wrong-name":
				writePrivate(t, filepath.Join(store.options.Root, strings.TrimSuffix(pending, ".json")), canonical)
			case "partial":
				writePrivate(t, path, canonical[:len(canonical)/2])
			case "noncanonical":
				writePrivate(t, path, []byte("{\"version\":2, \"identities\":[],\"contexts\":[]}\n"))
			case "extra":
				writePrivate(t, path, canonical)
				writePrivate(t, filepath.Join(store.options.Root, "retained"), []byte("retained\n"))
			case "multiple":
				writePrivate(t, path, canonical)
				writePrivate(t, filepath.Join(store.options.Root, "pending-11111111111111111111111111111111.json"), canonical)
			case "mode":
				writePrivate(t, path, canonical)
				if err := os.Chmod(path, 0644); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				writePrivate(t, path, canonical)
				if err := os.Link(path, filepath.Join(filepath.Dir(store.options.Root), "retained-link")); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				target := filepath.Join(filepath.Dir(store.options.Root), "retained-target")
				writePrivate(t, target, canonical)
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			case "fifo":
				if err := syscall.Mkfifo(path, 0600); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			before := snapshotRootEntries(t, store.options.Root)
			callbackCalled := false
			err = store.Transact(context.Background(), true, nil, func(contexts.Transaction) error {
				callbackCalled = true
				return nil
			})
			expectMissingRegistry(t, err)
			if callbackCalled {
				t.Fatal("create callback reached unknown or unsafe state")
			}
			after := snapshotRootEntries(t, store.options.Root)
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("refusal changed unknown or unsafe state:\nbefore: %#v\nafter:  %#v", before, after)
			}
			if _, err := os.Stat(filepath.Join(store.options.Root, "registry.json")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("refusal published a registry")
			}
		})
	}
}

func TestInitialRegistryRecoveryRejectsCandidateSubstitution(t *testing.T) {
	store, _ := fixture(t)
	if err := store.Transact(context.Background(), true, nil, func(contexts.Transaction) error { return nil }); err != nil {
		t.Fatal(err)
	}
	registry := filepath.Join(store.options.Root, "registry.json")
	data, err := os.ReadFile(registry)
	if err != nil {
		t.Fatal(err)
	}
	pending := filepath.Join(store.options.Root, "pending-00000000000000000000000000000000.json")
	if err := os.Rename(registry, pending); err != nil {
		t.Fatal(err)
	}
	store.fail = func(point string) error {
		if point != "before-initial-registry-recovery" {
			return nil
		}
		if err := os.Remove(pending); err != nil {
			return err
		}
		return os.WriteFile(pending, data, 0600)
	}
	callbackCalled := false
	err = store.Transact(context.Background(), true, nil, func(contexts.Transaction) error {
		callbackCalled = true
		return nil
	})
	expectState(t, err)
	if callbackCalled {
		t.Fatal("create callback ran after pending registry substitution")
	}
	if _, err := os.Stat(registry); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("substituted pending registry was published")
	}
}

func TestInitialRegistryRecoveryReportsUncertainDurability(t *testing.T) {
	store, _ := fixture(t)
	if err := os.Mkdir(store.options.Root, 0700); err != nil {
		t.Fatal(err)
	}
	want, err := encodeRecord(emptyRegistry(), maxRegistry)
	if err != nil {
		t.Fatal(err)
	}
	pending := filepath.Join(store.options.Root, "pending-00000000000000000000000000000000.json")
	writePrivate(t, pending, want)
	store.fail = func(point string) error {
		if point == "after-initial-registry-recovery" {
			return errors.New("synthetic post-rename interruption")
		}
		return nil
	}
	callbackCalled := false
	err = store.Transact(context.Background(), true, nil, func(contexts.Transaction) error {
		callbackCalled = true
		return nil
	})
	diagnostics := desiredstate.DiagnosticsOf(err)
	if len(diagnostics) != 1 || diagnostics[0].Message != "context initialization may have completed" || diagnostics[0].Remediation != pendingRegistryRemediation {
		t.Fatalf("unexpected uncertain-recovery diagnostic: %#v", diagnostics)
	}
	if callbackCalled {
		t.Fatal("create callback ran after uncertain initial registry recovery")
	}
	registry := filepath.Join(store.options.Root, "registry.json")
	got, err := os.ReadFile(registry)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("published initial registry is not inspectable after uncertainty: %v %q", err, got)
	}
	if _, err := os.Stat(pending); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("published initial registry retained its pending name")
	}

	store.fail = nil
	if err := store.Transact(context.Background(), true, nil, func(contexts.Transaction) error {
		callbackCalled = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !callbackCalled {
		t.Fatal("retry did not inspect the published registry and continue")
	}
}

func TestInitialRegistryRecoveryRejectsPostRenameRootChange(t *testing.T) {
	store, _ := fixture(t)
	if err := os.Mkdir(store.options.Root, 0700); err != nil {
		t.Fatal(err)
	}
	want, err := encodeRecord(emptyRegistry(), maxRegistry)
	if err != nil {
		t.Fatal(err)
	}
	pending := filepath.Join(store.options.Root, "pending-00000000000000000000000000000000.json")
	writePrivate(t, pending, want)
	store.fail = func(point string) error {
		if point == "after-initial-registry-recovery" {
			writePrivate(t, filepath.Join(store.options.Root, "unexpected"), []byte("retain\n"))
		}
		return nil
	}
	callbackCalled := false
	err = store.Transact(context.Background(), true, nil, func(contexts.Transaction) error {
		callbackCalled = true
		return nil
	})
	diagnostics := desiredstate.DiagnosticsOf(err)
	if len(diagnostics) != 1 || diagnostics[0].Message != "context initialization may have completed" || diagnostics[0].Remediation != pendingRegistryRemediation {
		t.Fatalf("unexpected changed-root diagnostic: %#v", diagnostics)
	}
	if callbackCalled {
		t.Fatal("create callback ran after the recovered root changed")
	}
	if data, readErr := os.ReadFile(filepath.Join(store.options.Root, "registry.json")); readErr != nil || !bytes.Equal(data, want) {
		t.Fatalf("published registry was not preserved: %v %q", readErr, data)
	}
	store.fail = nil
	callbackCalled = false
	if _, err := store.View(context.Background()); err == nil {
		t.Fatal("read-only view accepted the changed recovered root")
	} else {
		expectInconsistentEmptyRegistry(t, err)
	}
	if _, err := store.ReadInputs(context.Background(), "test", ""); err == nil {
		t.Fatal("input reader accepted the changed recovered root")
	} else {
		expectInconsistentEmptyRegistry(t, err)
	}
	if _, err := store.SecretContext(context.Background(), "test"); err == nil {
		t.Fatal("secret reader accepted the changed recovered root")
	} else {
		expectInconsistentEmptyRegistry(t, err)
	}
	err = store.Transact(context.Background(), true, nil, func(contexts.Transaction) error {
		callbackCalled = true
		return nil
	})
	expectInconsistentEmptyRegistry(t, err)
	if callbackCalled {
		t.Fatal("retry callback ran with unexpected state beside the empty registry")
	}
}

func TestRootSelectionIgnoresEnvironment(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	selected, err := New(Options{}).rootPath()
	if err != nil || selected != "/var/lib/bootwright" {
		t.Fatalf("fixed root: %q %v", selected, err)
	}
	expectState(t, New(Options{Root: "relative"}).CheckInputDirectory(context.Background(), t.TempDir()))
	if os.Geteuid() != 0 {
		_, err = New(Options{}).View(context.Background())
		expectState(t, err)
	}
}

func TestUnsafeStoreObjects(t *testing.T) {
	for _, kind := range []string{"root-mode", "root-link", "ancestor-link", "registry-link", "registry-hardlink", "registry-fifo", "missing-reservation", "missing-manifest", "manifest-mode", "blob-link", "blob-digest"} {
		t.Run(kind, func(t *testing.T) {
			store, sources := fixture(t)
			record := publish(t, store, "example", sources)
			registry := filepath.Join(store.options.Root, "registry.json")
			contextDir := filepath.Join(store.options.Root, "contexts", record.Name)
			revisionDir := filepath.Join(contextDir, "desired-state", "revisions", record.Revision)
			switch kind {
			case "root-mode":
				if err := os.Chmod(store.options.Root, 0755); err != nil {
					t.Fatal(err)
				}
			case "root-link":
				moved := store.options.Root + "-owned"
				if err := os.Rename(store.options.Root, moved); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(moved, store.options.Root); err != nil {
					t.Fatal(err)
				}
			case "ancestor-link":
				link := filepath.Join(filepath.Dir(store.options.Root), "alias")
				if err := os.Symlink(filepath.Dir(store.options.Root), link); err != nil {
					t.Fatal(err)
				}
				store.options.Root = filepath.Join(link, "state")
			case "registry-link":
				data, err := os.ReadFile(registry)
				if err != nil {
					t.Fatal(err)
				}
				other := filepath.Join(t.TempDir(), "other")
				writePrivate(t, other, data)
				if err := os.Remove(registry); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(other, registry); err != nil {
					t.Fatal(err)
				}
			case "registry-hardlink":
				if err := os.Link(registry, registry+".link"); err != nil {
					t.Fatal(err)
				}
			case "registry-fifo":
				if err := os.Remove(registry); err != nil {
					t.Fatal(err)
				}
				if err := syscall.Mkfifo(registry, 0600); err != nil {
					t.Fatal(err)
				}
			case "missing-reservation":
				if err := os.Remove(filepath.Join(contextDir, "state", "reservation.json")); err != nil {
					t.Fatal(err)
				}
			case "missing-manifest":
				if err := os.Remove(filepath.Join(revisionDir, "manifest.json")); err != nil {
					t.Fatal(err)
				}
			case "manifest-mode":
				if err := os.Chmod(filepath.Join(revisionDir, "manifest.json"), 0644); err != nil {
					t.Fatal(err)
				}
			case "blob-link":
				blob := filepath.Join(revisionDir, blobName(0))
				if err := os.Remove(blob); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(sources.Roots[0], "never-open"), blob); err != nil {
					t.Fatal(err)
				}
			case "blob-digest":
				writePrivate(t, filepath.Join(revisionDir, blobName(0)), []byte("version: modified\n"))
			}
			_, err := store.ReadInputs(context.Background(), "example", "")
			expectState(t, err)
			if kind != "blob-link" && kind != "blob-digest" {
				_, err = store.View(context.Background())
				expectState(t, err)
			}
		})
	}
}

func TestRegistryStrictnessAndManifestPaths(t *testing.T) {
	valid, _ := encodeRecord(emptyRegistry(), maxRegistry)
	for _, data := range [][]byte{
		[]byte(`{"version":1,"version":1,"current":"","identities":[],"contexts":[]}` + "\n"),
		[]byte(`{"version":1,"current":"","identities":null,"contexts":[]}` + "\n"),
		[]byte(`{"Version":1,"current":"","identities":[],"contexts":[]}` + "\n"),
		[]byte(`{"version":1,"current":"","identities":[],"contexts":[],"future":true}` + "\n"),
		[]byte(`{"version":"1","current":"","identities":[],"contexts":[]}` + "\n"),
		[]byte(strings.Repeat("[", 10000) + strings.Repeat("]", 10000)),
		append(slices.Clone(valid), valid...),
	} {
		var registry contexts.Registry
		expectState(t, decodeRecord(data, maxRegistry, &registry))
	}
	var registry contexts.Registry
	if err := decodeRecord(valid, maxRegistry, &registry); err != nil {
		t.Fatal(err)
	}
	store, sources := fixture(t)
	for _, path := range []string{"../escape.yaml", "secrets/payload.yaml", "manifests/native.yaml", ".hidden/object.yaml", "roles/main.yaml"} {
		changed := sources
		changed.Files = []desiredstate.SourceFile{desiredstate.NewSourceFile(filepath.Join(sources.Roots[0], path), []byte("a: b\n"))}
		_, _, err := prepareManifest("ctx-00000000000000000000000000000000", sources.Roots[0], changed)
		expectState(t, err)
	}
	if _, err := os.Stat(store.options.Root); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("pure manifest validation wrote state")
	}
	markerRoot := filepath.Join(t.TempDir(), "add-ons", "_store", "sample")
	markerSources := desiredstate.Sources{Roots: []string{markerRoot}, Markers: []desiredstate.SourceFile{desiredstate.NewSourceFile(filepath.Join(markerRoot, ".bootwright-addon"), []byte("native\n"))}}
	if _, _, err := prepareManifest("ctx-00000000000000000000000000000000", markerRoot, markerSources); err != nil {
		t.Fatal(err)
	}
}

func TestMutationGuardLayoutAndLeases(t *testing.T) {
	store, sources := fixture(t)
	record := publish(t, store, "example", sources)
	contextDir := filepath.Join(store.options.Root, "contexts", record.Name)
	writePrivate(t, filepath.Join(contextDir, "future-operation.json"), []byte("{}\n"))
	if _, err := store.ReadInputs(context.Background(), "example", ""); err != nil {
		t.Fatal(err)
	}
	expectState(t, store.Transact(context.Background(), false, nil, func(tx contexts.Transaction) error {
		_, err := tx.MutationState(context.Background(), record.ID)
		return err
	}))
	if err := os.Remove(filepath.Join(contextDir, "future-operation.json")); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(contextDir)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	expectState(t, store.Transact(context.Background(), false, nil, func(tx contexts.Transaction) error {
		_, err := tx.MutationState(context.Background(), record.ID)
		return err
	}))
	if _, err := store.ReadInputs(context.Background(), "example", ""); err != nil {
		t.Fatal("reader acquired a lease", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	if err := store.Transact(context.Background(), false, nil, func(tx contexts.Transaction) error {
		expectState(t, store.Transact(context.Background(), false, nil, func(contexts.Transaction) error { t.Fatal("second mutator acquired root"); return nil }))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestTransactionDoesNotPublishWithoutCommit(t *testing.T) {
	store, sources := fixture(t)
	record := publish(t, store, "example", sources)
	var orphan string
	if err := store.Transact(context.Background(), false, nil, func(tx contexts.Transaction) error {
		if _, err := tx.MutationState(context.Background(), record.ID); err != nil {
			return err
		}
		var err error
		orphan, err = tx.Publish(context.Background(), record.ID, sources.Roots[0], sources)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	view, err := store.View(context.Background())
	if err != nil || view.Contexts[0].Revision != record.Revision || orphan == record.Revision {
		t.Fatalf("uncommitted publication: %v", err)
	}
	err = store.Transact(context.Background(), false, nil, func(tx contexts.Transaction) error {
		registry := tx.Registry()
		if err := tx.Commit(context.Background(), registry); err != nil {
			return err
		}
		return tx.Commit(context.Background(), registry)
	})
	expectState(t, err)
}

func TestReplacementDuringTransactionRefuses(t *testing.T) {
	for _, target := range []string{"registry", "root", "revision-bytes", "evidence"} {
		t.Run(target, func(t *testing.T) {
			store, sources := fixture(t)
			record := publish(t, store, "example", sources)
			err := store.Transact(context.Background(), false, nil, func(tx contexts.Transaction) error {
				registry := tx.Registry()
				if _, err := tx.MutationState(context.Background(), record.ID); err != nil {
					return err
				}
				if target == "revision-bytes" {
					revision, err := tx.Publish(context.Background(), record.ID, sources.Roots[0], sources)
					if err != nil {
						return err
					}
					registry.Contexts[0].Revision = revision
					writePrivate(t, filepath.Join(store.options.Root, "contexts", record.Name, "desired-state", "revisions", revision, blobName(0)), []byte("version: tampered\n"))
				}
				if target == "registry" {
					path := filepath.Join(store.options.Root, "registry.json")
					data, err := os.ReadFile(path)
					if err != nil {
						return err
					}
					writePrivate(t, path+".replacement", data)
					if err := os.Rename(path+".replacement", path); err != nil {
						return err
					}
				}
				if target == "root" {
					if err := os.Rename(store.options.Root, store.options.Root+"-moved"); err != nil {
						return err
					}
					if err := os.Mkdir(store.options.Root, 0700); err != nil {
						return err
					}
				}
				if target == "evidence" {
					writePrivate(t, filepath.Join(store.options.Root, "contexts", record.Name, "state", "mutation.json"), []byte("{\"version\":1,\"operation\":\"pending\",\"ownership\":\"none\"}\n"))
				}
				return tx.Commit(context.Background(), registry)
			})
			expectState(t, err)
		})
	}
}

func TestPublicationFailurePoints(t *testing.T) {
	for _, point := range []string{"mkdir", "create-file", "write-file", "sync-file", "sync-directory", "before-registry-rename", "after-registry-rename"} {
		t.Run(point, func(t *testing.T) {
			store, sources := fixture(t)
			original := publish(t, store, "example", sources)
			store.fail = func(name string) error {
				if name == point {
					return errors.New("synthetic interruption")
				}
				return nil
			}
			err := store.Transact(context.Background(), false, nil, func(tx contexts.Transaction) error {
				if _, err := tx.MutationState(context.Background(), original.ID); err != nil {
					return err
				}
				revision, err := tx.Publish(context.Background(), original.ID, sources.Roots[0], sources)
				if err != nil {
					return err
				}
				registry := tx.Registry()
				registry.Contexts[0].Revision = revision
				return tx.Commit(context.Background(), registry)
			})
			expectState(t, err)
			store.fail = nil
			view, err := store.View(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if point != "after-registry-rename" && view.Contexts[0].Revision != original.Revision {
				t.Fatal("failed precommit operation changed selected input")
			}
			if point == "after-registry-rename" && view.Contexts[0].Revision == original.Revision {
				t.Fatal("postcommit interruption claimed rollback")
			}
			if _, err := store.ReadInputs(context.Background(), "example", ""); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestConcurrentReadersSeeCompleteRevisions(t *testing.T) {
	store, sources := fixture(t)
	publish(t, store, "example", sources)
	var group sync.WaitGroup
	failures := make(chan error, 4)
	for range 4 {
		group.Go(func() {
			for range 10 {
				input, err := store.ReadInputs(context.Background(), "example", "")
				if err != nil {
					failures <- err
					return
				}
				if len(input.Files) != 1 || !bytes.Equal(input.Files[0].Bytes(), sources.Files[0].Bytes()) {
					failures <- errors.New("partial revision")
					return
				}
			}
		})
	}
	group.Wait()
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	root, err := store.openRoot(context.Background(), false, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer root.file.Close()
	if err := lockShared(root); err != nil {
		t.Fatal(err)
	}
	defer syscall.Flock(int(root.file.Fd()), syscall.LOCK_UN)
	expectState(t, store.Transact(context.Background(), false, nil, func(contexts.Transaction) error { t.Fatal("mutator entered shared reader lock"); return nil }))
}

func TestCrashReleasesLocksAndLeavesCompleteSelection(t *testing.T) {
	for _, point := range []string{"before-registry-rename", "after-registry-rename"} {
		t.Run(point, func(t *testing.T) {
			store, sources := fixture(t)
			original := publish(t, store, "example", sources)
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, executable, "-test.run=^TestCrashHelper$")
			command.Env = append(os.Environ(), "BOOTWRIGHT_CONTEXTFS_TEST_ROOT="+store.options.Root, "BOOTWRIGHT_CONTEXTFS_TEST_POINT="+point)
			output, err := command.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 77 {
				t.Fatalf("crash helper: %v %s", err, output)
			}
			view, err := store.View(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if (view.Contexts[0].Revision == original.Revision) != (point == "before-registry-rename") {
				t.Fatal("crash visibility violated commit point")
			}
			if _, err := store.ReadInputs(context.Background(), "example", ""); err != nil {
				t.Fatal(err)
			}
			if err := store.Transact(context.Background(), false, nil, func(tx contexts.Transaction) error {
				if _, err := tx.MutationState(context.Background(), original.ID); err != nil {
					return err
				}
				return tx.Commit(context.Background(), tx.Registry())
			}); err != nil {
				t.Fatal("process exit retained a lock", err)
			}
		})
	}
}

func TestCrashHelper(t *testing.T) {
	root := os.Getenv("BOOTWRIGHT_CONTEXTFS_TEST_ROOT")
	if root == "" {
		t.Skip("subprocess helper")
	}
	store := New(testOptions(root))
	sources, err := store.ReadInputs(context.Background(), "example", "")
	if err != nil {
		t.Fatal(err)
	}
	point := os.Getenv("BOOTWRIGHT_CONTEXTFS_TEST_POINT")
	store.fail = func(name string) error {
		if name == point {
			os.Exit(77)
		}
		return nil
	}
	publish(t, store, "example", sources)
	t.Fatal("crash checkpoint was not reached")
}

type constantRandom byte

func (r constantRandom) Read(data []byte) (int, error) {
	for i := range data {
		data[i] = byte(r)
	}
	return len(data), nil
}

func FuzzPersistedRecords(f *testing.F) {
	valid, _ := encodeRecord(emptyRegistry(), maxRegistry)
	f.Add(valid, false)
	input := "/example/input"
	m := manifest{Version: 2, ID: "ctx-00000000000000000000000000000000", Revision: "rev-00000000000000000000000000000000", InputDirectory: input, EnvironmentDirectory: input, Files: []frozenFile{{Path: "environment.yaml", Category: "yaml", Size: 0, SHA256: digest(nil)}}}
	validManifest, _ := encodeRecord(m, maxManifest)
	f.Add(validManifest, true)
	f.Add([]byte(`{"version":1,"version":1}`), false)
	f.Add([]byte(`{"files":null}`), true)
	f.Add([]byte(strings.Repeat("[", 32)+strings.Repeat("]", 32)), false)
	f.Fuzz(func(t *testing.T, data []byte, isManifest bool) {
		if len(data) > 64<<10 {
			t.Skip()
		}
		if isManifest {
			var value manifest
			if decodeRecord(data, maxManifest, &value) == nil {
				_ = validateManifest(value, value.ID, value.Revision, value.EnvironmentDirectory)
			}
		} else {
			var value contexts.Registry
			if decodeRecord(data, maxRegistry, &value) == nil {
				_ = validateRegistry(value)
			}
		}
	})
}

func TestEveryPublicationEffectCanBeInterrupted(t *testing.T) {
	traceStore, traceSources := fixture(t)
	publish(t, traceStore, "example", traceSources)
	var trace []string
	traceStore.fail = func(point string) error { trace = append(trace, point); return nil }
	publish(t, traceStore, "example", traceSources)
	commitIndex := slices.Index(trace, "after-registry-rename")
	if commitIndex < 0 {
		t.Fatal("no publication commit point")
	}
	for interrupted := range trace {
		store, sources := fixture(t)
		original := publish(t, store, "example", sources)
		count := 0
		store.fail = func(string) error {
			index := count
			count++
			if index == interrupted {
				return errors.New("synthetic interruption")
			}
			return nil
		}
		err := store.Transact(context.Background(), false, nil, func(tx contexts.Transaction) error {
			if _, err := tx.MutationState(context.Background(), original.ID); err != nil {
				return err
			}
			revision, err := tx.Publish(context.Background(), original.ID, sources.Roots[0], sources)
			if err != nil {
				return err
			}
			registry := tx.Registry()
			registry.Contexts[0].Revision = revision
			return tx.Commit(context.Background(), registry)
		})
		expectState(t, err)
		store.fail = nil
		view, err := store.View(context.Background())
		if err != nil {
			t.Fatalf("checkpoint %d %s: %v", interrupted, trace[interrupted], err)
		}
		if (view.Contexts[0].Revision == original.Revision) != (interrupted < commitIndex) {
			t.Fatalf("checkpoint %d violated atomic visibility", interrupted)
		}
		if _, err := store.ReadInputs(context.Background(), "example", ""); err != nil {
			t.Fatalf("checkpoint %d: %v", interrupted, err)
		}
	}
}

func TestPendingSourceReplacementRefusesPublication(t *testing.T) {
	for _, replacement := range []string{"same-bytes", "different-bytes", "symlink"} {
		t.Run(replacement, func(t *testing.T) {
			store, sources := fixture(t)
			original := publish(t, store, "example", sources)
			before, err := os.ReadFile(filepath.Join(store.options.Root, "registry.json"))
			if err != nil {
				t.Fatal(err)
			}
			store.fail = func(point string) error {
				if point != "before-registry-rename" {
					return nil
				}
				entries, err := os.ReadDir(store.options.Root)
				if err != nil {
					return err
				}
				for _, entry := range entries {
					if strings.HasPrefix(entry.Name(), "pending-") {
						path := filepath.Join(store.options.Root, entry.Name())
						data, err := os.ReadFile(path)
						if err != nil {
							return err
						}
						if err := os.Remove(path); err != nil {
							return err
						}
						if replacement == "symlink" {
							return os.Symlink(filepath.Join(store.options.Root, "registry.json"), path)
						}
						if replacement == "different-bytes" {
							data = []byte("{}\n")
						}
						return os.WriteFile(path, data, 0600)
					}
				}
				return errors.New("missing pending source")
			}
			err = store.Transact(context.Background(), false, nil, func(tx contexts.Transaction) error {
				if _, err := tx.MutationState(context.Background(), original.ID); err != nil {
					return err
				}
				revision, err := tx.Publish(context.Background(), original.ID, sources.Roots[0], sources)
				if err != nil {
					return err
				}
				registry := tx.Registry()
				registry.Contexts[0].Revision = revision
				return tx.Commit(context.Background(), registry)
			})
			expectState(t, err)
			after, err := os.ReadFile(filepath.Join(store.options.Root, "registry.json"))
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("replaced source was published")
			}
		})
	}
}

func TestViewValidatesManifestAgreement(t *testing.T) {
	for _, malformed := range []bool{false, true} {
		store, sources := fixture(t)
		record := publish(t, store, "example", sources)
		path := filepath.Join(store.options.Root, "contexts", record.Name, "desired-state", "revisions", record.Revision, "manifest.json")
		if malformed {
			writePrivate(t, path, []byte("{}\n"))
		} else {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var m manifest
			if err := decodeRecord(data, maxManifest, &m); err != nil {
				t.Fatal(err)
			}
			m.ID = "ctx-00000000000000000000000000000000"
			data, err = encodeRecord(m, maxManifest)
			if err != nil {
				t.Fatal(err)
			}
			writePrivate(t, path, data)
		}
		_, err := store.View(context.Background())
		expectState(t, err)
		err = store.Transact(context.Background(), false, nil, func(contexts.Transaction) error {
			t.Fatal("mutation callback reached inconsistent manifest")
			return nil
		})
		expectState(t, err)
	}
}

func TestAggregateManifestBytesPrecedeDecoding(t *testing.T) {
	store, _ := fixture(t)
	var paths []string
	for i := range 9 {
		input := filepath.Join(filepath.Dir(store.options.Root), "source-"+strconv.Itoa(i))
		if err := os.Mkdir(input, 0700); err != nil {
			t.Fatal(err)
		}
		sources := desiredstate.Sources{Roots: []string{input}, Files: []desiredstate.SourceFile{desiredstate.NewSourceFile(filepath.Join(input, "environment.yaml"), nil)}}
		record := publish(t, store, "example-"+strconv.Itoa(i), sources)
		paths = append(paths, filepath.Join(store.options.Root, "contexts", record.Name, "desired-state", "revisions", record.Revision, "manifest.json"))
	}
	for _, path := range paths {
		if err := os.Truncate(path, maxManifest); err != nil {
			t.Fatal(err)
		}
	}
	_, err := store.View(context.Background())
	expectState(t, err)
	var failure *desiredstate.Failure
	errors.As(err, &failure)
	if !strings.Contains(failure.Diagnostics[0].Message, "aggregate") {
		t.Fatal("manifest decoding began before aggregate bounds")
	}
}

func TestRecordSizePreflightMatchesCanonicalEncoding(t *testing.T) {
	values := []any{
		emptyRegistry(),
		contexts.Record{DirectoryDevice: ^uint64(0), DirectoryInode: ^uint64(0)},
		reservation{Version: -9223372036854775807, ID: "<>&\n\t\b\r\f\x00\"\\", Name: "/example/\u2028\u2029/é/雪"},
		manifest{Version: 2, Files: []frozenFile{{Path: "a.yaml", Category: "yaml", Size: 100, SHA256: digest(nil)}}},
	}
	for _, value := range values {
		canonical, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := encodeRecord(value, len(canonical)); err == nil {
			t.Fatal("undersized encoding budget accepted")
		}
		actual, err := encodeRecord(value, len(canonical)+1)
		if err != nil || !bytes.Equal(actual, append(canonical, '\n')) {
			t.Fatalf("exact encoding budget: %v", err)
		}
	}
}
