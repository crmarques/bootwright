//go:build linux && amd64

package contextfs

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/managedos"
	"github.com/crmarques/bootwright/internal/managedos/media"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// limitFileSize lowers this process's RLIMIT_FSIZE and ignores SIGXFSZ, so a
// write past the limit fails with EFBIG as a full filesystem's write fails
// with ENOSPC. The limit holds for the whole process, so no test that sets it
// runs in parallel. The returned function restores both, as Cleanup does.
func limitFileSize(t *testing.T, limit uint64) func() {
	t.Helper()
	var original syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &original); err != nil {
		t.Fatal(err)
	}
	signal.Ignore(syscall.SIGXFSZ)
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &syscall.Rlimit{Cur: limit, Max: original.Max}); err != nil {
		t.Fatal(err)
	}
	restored := false
	restore := func() {
		if restored {
			return
		}
		restored = true
		if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &original); err != nil {
			t.Errorf("restoring the file size limit: %v", err)
		}
		signal.Reset(syscall.SIGXFSZ)
	}
	t.Cleanup(restore)
	return restore
}

// cancelOnceWritten turns Canceled once a file in the named directory holds
// bytes, as an interrupt that arrives in the middle of a bundle write does.
type cancelOnceWritten struct {
	context.Context
	directory string
}

func (c cancelOnceWritten) Err() error {
	entries, _ := os.ReadDir(c.directory)
	for _, entry := range entries {
		if info, err := entry.Info(); err == nil && info.Mode().IsRegular() && info.Size() > 0 {
			return context.Canceled
		}
	}
	return c.Context.Err()
}

const bundlePayloadSize = 280000

// writeBundlePayload is a durably intended setup action that publishes one
// executable through write, which may interrupt it, and reports the error
// write returned and what the area lists afterwards.
func writeBundlePayload(t *testing.T, store *Store, write func(context.Context, prerequisites.BundleArea, []byte) error) (error, []prerequisites.BundleEntry) {
	t.Helper()
	ctx := context.Background()
	digest := syntheticControllerState(t, prerequisites.SetupContext{}).Receipt.CatalogDigest
	payload := bytes.Repeat([]byte("b"), bundlePayloadSize)
	var written error
	var listed []prerequisites.BundleEntry
	err := store.MutateController(ctx, prerequisites.SetupContext{}, false, func(tx prerequisites.StorageTransaction) error {
		next := tx.Snapshot().State
		next.Receipt.Actions[0].Phase = "intent"
		if _, err := tx.Publish(ctx, next); err != nil {
			return err
		}
		area, err := tx.Bundle(ctx, digest)
		if err != nil {
			return err
		}
		if err := area.EnsureDirectory(ctx, "bin"); err != nil {
			return err
		}
		written = write(ctx, area, payload)
		listed, err = area.Entries(ctx)
		if err != nil {
			return err
		}
		return written
	})
	if !errors.Is(err, written) && !reflect.DeepEqual(diagnostics.Of(err), diagnostics.Of(written)) {
		t.Fatalf("the setup action reported %v, but its write returned %v", err, written)
	}
	return written, listed
}

// replayBundlePayload is the exact replay setup retry performs: it lists the
// area and publishes the payload only when it is absent.
func replayBundlePayload(t *testing.T, store *Store) {
	t.Helper()
	_, listed := writeBundlePayload(t, store, func(ctx context.Context, area prerequisites.BundleArea, payload []byte) error {
		return checkpointReplay(ctx, area, "bin", "bin/payload", payload)
	})
	index := slices.IndexFunc(listed, func(entry prerequisites.BundleEntry) bool { return entry.Path == "bin/payload" })
	if index < 0 || listed[index].Size != bundlePayloadSize || !listed[index].Executable {
		t.Fatalf("the replay left %#v, want the whole executable payload", listed)
	}
}

func requireNoBundlePayload(t *testing.T, store *Store, listed []prerequisites.BundleEntry) {
	t.Helper()
	target := filepath.Join(bundlePath(store, syntheticControllerState(t, prerequisites.SetupContext{}).Receipt.CatalogDigest), "bin", "payload")
	if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the interrupted write left its file under its final name (%v)", err)
	}
	if left, err := os.ReadDir(filepath.Dir(target)); err != nil || len(left) != 0 {
		t.Fatalf("the interrupted write left %v beside its final name (%v)", left, err)
	}
	if slices.ContainsFunc(listed, func(entry prerequisites.BundleEntry) bool { return entry.Path == "bin/payload" }) {
		t.Fatalf("the area still lists the interrupted file: %#v", listed)
	}
}

// A bundle write cancelled once its stage holds bytes removes exactly that
// stage, so the exact replay of the setup that retries it publishes the file
// whole.
func TestABundleWriteCancelledAfterItsCreateLeavesNoFile(t *testing.T) {
	store, _ := controllerFixture(t)
	directory := filepath.Join(bundlePath(store, syntheticControllerState(t, prerequisites.SetupContext{}).Receipt.CatalogDigest), "bin")
	written, listed := writeBundlePayload(t, store, func(ctx context.Context, area prerequisites.BundleArea, payload []byte) error {
		return area.Write(cancelOnceWritten{Context: ctx, directory: directory}, "bin/payload", payload, true)
	})
	if !errors.Is(written, context.Canceled) {
		t.Fatalf("the cancelled write returned %v %#v", written, diagnostics.Of(written))
	}
	requireNoBundlePayload(t, store, listed)
	replayBundlePayload(t, store)
}

// A bundle write the filesystem refuses after its create removes exactly that
// file and names the kernel's answer with the free-space remedy, never a path,
// and the replay completes once the filesystem has room again.
func TestABundleWriteFaultedAfterItsCreateLeavesNoFile(t *testing.T) {
	store, _ := controllerFixture(t)
	written, listed := writeBundlePayload(t, store, func(ctx context.Context, area prerequisites.BundleArea, payload []byte) error {
		restore := limitFileSize(t, 64<<10)
		defer restore()
		return area.Write(ctx, "bin/payload", payload, true)
	})
	reported := diagnostics.Of(written)
	if len(reported) != 1 || reported[0].Code != "context.state" || !strings.Contains(reported[0].Message, "file too large") ||
		!strings.Contains(reported[0].Remediation, "free space") || strings.Contains(reported[0].Message, store.options.Root) || strings.Contains(reported[0].Message, "payload") {
		t.Fatalf("the faulted write reported %v %#v", written, reported)
	}
	requireNoBundlePayload(t, store, listed)
	replayBundlePayload(t, store)
}

// A stage a killed bundle write left is not bundle content: a read lists the
// area without it and leaves it, the exact replay removes it before it
// publishes the file under its final name, and a sealed area holding one
// refuses.
func TestAStageAKilledBundleWriteLeftIsSweptByTheReplay(t *testing.T) {
	store, _ := controllerFixture(t)
	ctx := context.Background()
	digest := syntheticControllerState(t, prerequisites.SetupContext{}).Receipt.CatalogDigest
	if written, _ := writeBundlePayload(t, store, func(context.Context, prerequisites.BundleArea, []byte) error { return nil }); written != nil {
		t.Fatal(written)
	}
	stage := filepath.Join(bundlePath(store, digest), "bin", bundleStagePrefix+strings.Repeat("0", 32))
	writePrivate(t, stage, []byte("partial"))
	read := func() ([]prerequisites.BundleEntry, error) {
		var listed []prerequisites.BundleEntry
		err := store.ReadController(ctx, "", func(view prerequisites.StorageView) error {
			area, err := view.OpenBundle(ctx, digest)
			if err != nil {
				return err
			}
			listed, err = area.Entries(ctx)
			return err
		})
		return listed, err
	}
	if listed, err := read(); err != nil || !reflect.DeepEqual(listed, []prerequisites.BundleEntry{{Path: "bin", Directory: true}}) {
		t.Fatalf("a read of the interrupted area lists %#v (%v)", listed, diagnostics.Of(err))
	}
	if _, err := os.Lstat(stage); err != nil {
		t.Fatalf("a read removed the stage: %v", err)
	}
	replayBundlePayload(t, store)
	if _, err := os.Lstat(stage); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the replay kept the stage (%v)", err)
	}
	if err := store.MutateController(ctx, prerequisites.SetupContext{}, false, func(tx prerequisites.StorageTransaction) error {
		_, err := tx.Publish(ctx, completeControllerState(tx.Snapshot().State))
		return err
	}); err != nil {
		t.Fatalf("sealing the replayed bundle: %#v", diagnostics.Of(err))
	}
	writePrivate(t, stage, []byte("planted"))
	_, err := read()
	expectState(t, err)
}

// requireCapacity requires one diagnostic of code that names the kernel's
// capacity answer and the free-space remedy, and names neither the state root
// nor any file beneath it.
func requireCapacity(t *testing.T, store *Store, err error, code, message string) {
	t.Helper()
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != code || !strings.HasPrefix(reported[0].Message, message) || !strings.Contains(reported[0].Message, "file too large (EFBIG)") ||
		!strings.Contains(reported[0].Remediation, "free space, or raise the quota") {
		t.Fatalf("want %s %q naming the capacity errno, got %v %#v", code, message, err, reported)
	}
	for _, private := range []string{store.options.Root, filepath.Dir(store.options.Root), "file-0000", "staging-", "blob-"} {
		if strings.Contains(reported[0].Message, private) || strings.Contains(reported[0].Remediation, private) {
			t.Fatalf("the capacity refusal names %q: %#v", private, reported)
		}
	}
}

// A filesystem with no room is a capacity refusal wherever the store writes:
// the kernel's answer and the free-space remedy, never corruption or a failed
// source, and never a private path.
func TestStoreCapacityNamesOnlyTheErrno(t *testing.T) {
	const limit = 64 << 10
	large := "large: " + strings.Repeat("x", 3*limit) + "\n"
	t.Run("context update", func(t *testing.T) {
		store, sources := fixture(t)
		record := publish(t, store, "example", sources)
		input := desiredstate.Sources{Roots: sources.Roots, Files: []desiredstate.SourceFile{desiredstate.NewSourceFile(filepath.Join(sources.Roots[0], "environment.yaml"), []byte(large))}, Markers: []desiredstate.SourceFile{}}
		err := store.Transact(context.Background(), false, sources.Roots, func(tx contexts.Transaction) error {
			if _, err := tx.MutationState(context.Background(), record.Name); err != nil {
				return err
			}
			restore := limitFileSize(t, limit)
			defer restore()
			_, err := tx.Publish(context.Background(), record.Name, sources.Roots[0], input)
			return err
		})
		requireCapacity(t, store, err, "context.state", "state file could not be written: the filesystem holding the state root has no room: ")
	})
	t.Run("secret part", func(t *testing.T) {
		store, sources := fixture(t)
		record := publish(t, store, "example", sources)
		err := store.MutateSecrets(context.Background(), secretToken(record), func(area secretstore.Area) error {
			if err := area.EnsureDirectory(context.Background(), "parts"); err != nil {
				return err
			}
			restore := limitFileSize(t, limit)
			defer restore()
			return area.WriteExclusive(context.Background(), "parts/blob-00000000000000000000000000000000", []byte(large))
		})
		requireCapacity(t, store, err, "secret.store", "state file could not be written: the filesystem holding the state root has no room: ")
	})
	t.Run("secret record", func(t *testing.T) {
		store, sources := fixture(t)
		record := publish(t, store, "example", sources)
		var outcome secretstore.Outcome
		err := store.MutateSecrets(context.Background(), secretToken(record), func(area secretstore.Area) error {
			expected, _, err := area.ReadMutable(context.Background(), secretstore.RecordPath, 8<<20)
			if err != nil {
				return err
			}
			restore := limitFileSize(t, limit)
			defer restore()
			outcome, err = area.Replace(context.Background(), secretstore.RecordPath, []byte(large), expected)
			return err
		})
		if outcome != secretstore.NotCommitted {
			t.Fatalf("a refused record publication reported %q", outcome)
		}
		requireCapacity(t, store, err, "secret.store", "state file could not be written: the filesystem holding the state root has no room: ")
	})
	t.Run("media stage", func(t *testing.T) {
		store := mediaFixture(t)
		stage := claimStage(t, store, "demo.iso")
		restore := limitFileSize(t, limit)
		_, err := stage.Fill(context.Background(), mediaPayload(large), managedos.MaxMediaBytes)
		restore()
		requireCapacity(t, store, err, "media.store", "the media store could not hold the image: ")
	})
}

// A raw kernel answer that reaches the store's last line of classification is
// named by its own text: capacity as capacity, anything else beside the
// generic refusal, and never through the text of the error carrying it.
func TestSafeErrorNamesTheKernelsAnswer(t *testing.T) {
	private := &os.PathError{Op: "open", Path: "/private/state/contexts/example", Err: syscall.EDQUOT}
	for _, row := range []struct {
		name        string
		err         error
		message     string
		remediation string
	}{
		{"capacity", private, "context storage could not be safely accessed: the filesystem holding the state root has no room: disk quota exceeded (EDQUOT)", storeCapacityRemediation},
		{"other errno", syscall.ELOOP, "context storage could not be safely accessed: too many levels of symbolic links", ""},
		{"no errno", errors.New("synthetic-error-canary"), "context storage could not be safely accessed", ""},
	} {
		t.Run(row.name, func(t *testing.T) {
			reported := diagnostics.Of(safeError(row.err))
			if len(reported) != 1 || reported[0].Code != "context.state" || reported[0].Message != row.message || reported[0].Remediation != row.remediation {
				t.Fatalf("safeError(%v) = %#v", row.err, reported)
			}
		})
	}
}

// A secret write the filesystem refuses is secret.store with the kernel's
// answer and its remedy; only a failed integrity proof of the store's own
// state is secret.store.corrupt, and an unclassified error stays a conflict.
func TestASecretWriteFailureIsCorruptOnlyWhenTheStoreFailedItsProof(t *testing.T) {
	limit := secretstore.Failure("store.limit", "bounded secret storage is full")
	for _, row := range []struct {
		name, code, message string
		err                 error
	}{
		{"store refusal", "secret.store", "state file could not be written: the filesystem holding the state root has no room: no space left on device (ENOSPC)", storeFailure("state file could not be written", syscall.ENOSPC)},
		{"raw capacity", "secret.store", "secret write: the filesystem holding the state root has no room: no space left on device (ENOSPC)", syscall.ENOSPC},
		{"raw device error", "secret.store", "secret write: input/output error", &os.PathError{Op: "mkdirat", Path: "/private/secrets", Err: syscall.EIO}},
		{"secret store failure", "secret.store.limit", "bounded secret storage is full", limit},
		{"integrity refusal", "secret.store.corrupt", "secret write", state("held state directory changed")},
		{"unclassified", "secret.store.conflict", "secret write", syscall.EEXIST},
	} {
		t.Run(row.name, func(t *testing.T) {
			reported := diagnostics.Of(secretEffectFailure(context.Background(), "secret write", row.err))
			if len(reported) != 1 || reported[0].Code != row.code || reported[0].Message != row.message || strings.Contains(reported[0].Message, "/private") {
				t.Fatalf("secretEffectFailure(%v) = %#v", row.err, reported)
			}
		})
	}
}

// failingSource returns some bytes, then fails, as a source that drops its
// connection does.
type failingSource struct{ sent bool }

func (s *failingSource) Read(data []byte) (int, error) {
	if !s.sent {
		s.sent = true
		return copy(data, "partial image"), nil
	}
	return 0, errors.New("synthetic source failure")
}

func (*failingSource) Close() error { return nil }

var _ media.Payload = (*failingSource)(nil)

// Only the stage's own write is the store's failure: a source that fails is
// still blamed on the source, with its own remedy.
func TestAMediaSourceFailureStillBlamesTheSource(t *testing.T) {
	store := mediaFixture(t)
	stage := claimStage(t, store, "demo.iso")
	_, err := stage.Fill(context.Background(), &failingSource{}, managedos.MaxMediaBytes)
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "media.store" || reported[0].Message != "the image could not be read in full" || reported[0].Remediation != "verify the source and repeat the command" {
		t.Fatalf("a failed source reported %v %#v", err, reported)
	}
}

// A listing reads the directory it holds, never whatever its name resolves to
// now, so a directory renamed over a held one refuses as replaced and its
// names are never returned.
func TestAHeldListingRefusesASubstitutedDirectory(t *testing.T) {
	store, sources := fixture(t)
	publish(t, store, "example", sources)
	root, err := store.openRoot(context.Background(), false, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer root.file.Close()
	container, err := openDirectory(root, "contexts")
	if err != nil {
		t.Fatal(err)
	}
	defer container.file.Close()
	held, err := openDirectory(container, "example")
	if err != nil {
		t.Fatal(err)
	}
	defer held.file.Close()
	if names, err := heldNames(root, 16); err != nil || !slices.Equal(names, []string{"contexts", "registry.json"}) {
		t.Fatalf("the root lists %v (%v)", names, err)
	}
	contextsPath := filepath.Join(store.options.Root, "contexts")
	if err := os.Rename(filepath.Join(contextsPath, "example"), filepath.Join(contextsPath, "moved")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(contextsPath, "example"), 0700); err != nil {
		t.Fatal(err)
	}
	writePrivate(t, filepath.Join(contextsPath, "example", "substitute"), []byte("planted\n"))
	for name, list := range map[string]func(*directory, int) ([]string, error){"heldNames": heldNames, "directoryNames": directoryNames} {
		names, err := list(held, 16)
		reported := diagnostics.Of(err)
		if names != nil || len(reported) != 1 || reported[0].Code != "context.state" || !strings.Contains(reported[0].Message, "replaced") {
			t.Fatalf("%s of a substituted directory = %v (%v %#v)", name, names, err, reported)
		}
	}
}

// requireEarlierBuildRemedy requires the refusal of a root this build cannot
// read as its own: another build may run live environments from it, so it is
// never to be moved aside while their services run.
func requireEarlierBuildRemedy(t *testing.T, operation string, err error) {
	t.Helper()
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "context.state" || reported[0].Message != missingRegistryMessage || reported[0].Remediation != storeRecoveryRemediation {
		t.Fatalf("%s refused with %v %#v", operation, err, reported)
	}
	for _, fragment := range []string{"another Bootwright build may have created this root and may manage live environments there", "run this build on another controller host", "never move it aside while its services run", "restored whole from a matching backup"} {
		if !strings.Contains(reported[0].Remediation, fragment) {
			t.Fatalf("%s refused without %q: %#v", operation, fragment, reported)
		}
	}
	if strings.Contains(reported[0].Remediation, "move it aside if") {
		t.Fatalf("%s still directs moving a live root aside: %#v", operation, reported)
	}
}

// A root that holds state another Bootwright build wrote reads as such, never
// as damage to move aside, and every command leaves it as it is: one without a
// registry, and one whose registry.json an earlier build wrote in the format
// version 2, 3 or 4 that named contexts by an allocated identity.
func TestAnEarlierBuildsRootRefusesWithTheLiveEnvironmentRemedy(t *testing.T) {
	for name, entries := range map[string]map[string]string{
		"without a registry": {"contexts.yaml": "contexts: []\n", ".bootwright-managed-root": "managed\n"},
		"registry version 2": {"registry.json": "{\"version\":2,\"idNamespace\":\"0123456789abcdef\",\"nextIdentity\":2,\"contexts\":[]}\n"},
		"registry version 3": {"registry.json": "{\"version\":3,\"idNamespace\":\"0123456789abcdef\",\"nextIdentity\":2,\"contexts\":[]}\n"},
		"registry version 4": {"registry.json": "{\"version\":4,\"idNamespace\":\"0123456789abcdef\",\"nextIdentity\":2,\"contexts\":[]}\n"},
	} {
		t.Run(name, func(t *testing.T) {
			store, _ := fixture(t)
			root := store.options.Root
			if err := os.MkdirAll(filepath.Join(root, "contexts", "legacy"), 0700); err != nil {
				t.Fatal(err)
			}
			for entry, data := range entries {
				writePrivate(t, filepath.Join(root, entry), []byte(data))
			}
			before := snapshotRootEntries(t, root)
			ctx := context.Background()
			_, err := store.View(ctx)
			requireEarlierBuildRemedy(t, "view", err)
			requireEarlierBuildRemedy(t, "controller read", store.ReadController(ctx, "", func(prerequisites.StorageView) error { return nil }))
			requireEarlierBuildRemedy(t, "init", store.Transact(ctx, true, nil, func(contexts.Transaction) error {
				t.Fatal("init reached an earlier build's root")
				return nil
			}))
			requireEarlierBuildRemedy(t, "update", store.Transact(ctx, false, nil, func(contexts.Transaction) error {
				t.Fatal("a registry transaction reached an earlier build's root")
				return nil
			}))
			requireEarlierBuildRemedy(t, "media read", store.ReadMedia(ctx, func(media.View) error { return nil }))
			if after := snapshotRootEntries(t, root); !reflect.DeepEqual(before, after) {
				t.Fatalf("the refusals changed the root:\nbefore: %#v\nafter:  %#v", before, after)
			}
		})
	}
}

// A root that is not private names what it is and what it must be, and its
// remedy, since nothing repairs it.
func TestARootThatIsNotPrivateNamesTheRequiredMode(t *testing.T) {
	store, _ := fixture(t)
	if err := os.Mkdir(store.options.Root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(store.options.Root, 0755); err != nil {
		t.Fatal(err)
	}
	_, err := store.View(context.Background())
	reported := diagnostics.Of(err)
	owner := ownerText(uint32(os.Geteuid()), uint32(os.Getegid()))
	want := "the state root is a directory owned by " + owner + " with mode 0755, but it must be a directory owned by " + owner + " with mode 0700"
	if len(reported) != 1 || reported[0].Code != "context.state" || reported[0].Message != want ||
		!strings.Contains(reported[0].Remediation, "nothing repairs it") || !strings.Contains(reported[0].Remediation, "never move it aside while its services run") {
		t.Fatalf("a 0755 root refused with %v %#v", err, reported)
	}
}
