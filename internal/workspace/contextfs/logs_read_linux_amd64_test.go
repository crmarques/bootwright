//go:build linux && amd64

package contextfs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/reconciliation/logview"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

const logOperation = "op-0123456789abcdef0123456789abcdef"

func appendLog(t *testing.T, store *Store, target, data string) {
	t.Helper()
	ctx := context.Background()
	if err := store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error {
		return tx.Logs().Append(ctx, target, []byte(data))
	}); err != nil {
		t.Fatalf("a log write failed: %#v", diagnostics.Of(err))
	}
}

func diagnosticCode(err error) string {
	if reported := diagnostics.Of(err); len(reported) == 1 {
		return reported[0].Code
	}
	return ""
}

func readAt(t *testing.T, file logview.LogFile) string {
	t.Helper()
	size, err := file.Size()
	if err != nil {
		t.Fatal(err)
	}
	data, err := file.ReadRange(0, int(size))
	if err != nil || int64(len(data)) != size {
		t.Fatalf("read %d of %d bytes: %v", len(data), size, err)
	}
	return string(data)
}

func TestReadLogsTakesOnlyTheSharedRootLockAndReadsNoInput(t *testing.T) {
	ctx := context.Background()
	store, _ := lifecycleFixture(t)
	appendLog(t, store, logOperation+"/operation.jsonl", "{}\n")
	err := store.Transact(ctx, false, nil, func(contexts.Transaction) error {
		readErr := store.ReadLogs(ctx, "example", func(logview.View) error { return nil })
		if diagnosticCode(readErr) != "lifecycle.lease" {
			t.Fatalf("a read beside an exclusive root holder = %#v, want lifecycle.lease", diagnostics.Of(readErr))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var sealed []string
	if err := filepath.WalkDir(filepath.Join(store.options.Root, "contexts", "example", "desired-state"), func(path string, entry os.DirEntry, err error) error {
		if err == nil && entry.Type().IsRegular() && entry.Name() != "manifest.json" {
			sealed = append(sealed, path)
		}
		return err
	}); err != nil || len(sealed) == 0 {
		t.Fatalf("no stored input copy to seal (%v)", err)
	}
	for _, path := range sealed {
		if err := os.Chmod(path, 0); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(path, 0600)
	}
	if err := store.ReadLifecycle(ctx, "example", func(lifecycle.View) error { return nil }); err == nil {
		t.Fatal("a lifecycle read opened a sealed input copy, so sealing proves nothing")
	}
	var shared bool
	err = store.ReadLogs(ctx, "example", func(view logview.View) error {
		root, err := os.Open(store.options.Root)
		if err != nil {
			return err
		}
		defer root.Close()
		exclusive := syscall.Flock(int(root.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		shared = syscall.Flock(int(root.Fd()), syscall.LOCK_SH|syscall.LOCK_NB) == nil
		if !errors.Is(exclusive, syscall.EWOULDBLOCK) {
			t.Fatalf("the root lock taken exclusively beside a log read = %v, want it held shared", exclusive)
		}
		file, found, err := view.Open(ctx, logOperation+"/operation.jsonl")
		if err != nil || !found {
			t.Fatalf("the operation log = %t %#v", found, diagnostics.Of(err))
		}
		defer file.Close()
		return nil
	})
	if err != nil || !shared {
		t.Fatalf("a log read with every input copy sealed = %#v (another shared read %t)", diagnostics.Of(err), shared)
	}
}

func TestALogHandleOutlivesItsReadAndSeesAppendedBytes(t *testing.T) {
	ctx := context.Background()
	store, _ := lifecycleFixture(t)
	target := logOperation + "/blocks/network/attempt-000001.output"
	appendLog(t, store, target, "first\n")
	var file logview.LogFile
	if err := store.ReadLogs(ctx, "example", func(view logview.View) error {
		if _, found, err := view.Open(ctx, logOperation+"/blocks/network/attempt-000002.output"); found || err != nil {
			t.Fatalf("an absent output = %t %v, want absent", found, err)
		}
		var found bool
		var err error
		file, found, err = view.Open(ctx, target)
		if err == nil && !found {
			err = errors.New("the output is absent")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if got := readAt(t, file); got != "first\n" {
		t.Fatalf("after the read = %q", got)
	}
	appendLog(t, store, target, "second\n")
	if got := readAt(t, file); got != "first\nsecond\n" {
		t.Fatalf("after an append outside every lock = %q", got)
	}
	if err := os.RemoveAll(filepath.Join(contextLogsPath(store, "example"), operationLogs, logOperation)); err != nil {
		t.Fatal(err)
	}
	if got := readAt(t, file); got != "first\nsecond\n" {
		t.Fatalf("after retention removed the directory = %q", got)
	}
}

func TestLogOpenRefusesALinkASecondLinkAGroupReadableFileOrANonRegularOne(t *testing.T) {
	ctx := context.Background()
	store, _ := lifecycleFixture(t)
	appendLog(t, store, logOperation+"/operation.jsonl", "{}\n")
	directory := filepath.Join(contextLogsPath(store, "example"), operationLogs, logOperation)
	original := filepath.Join(directory, "operation.jsonl")
	plant := map[string]func(string) error{
		"link":           func(path string) error { return os.Symlink(original, path) },
		"second-link":    func(path string) error { return os.Link(original, path) },
		"group-readable": func(path string) error { return os.WriteFile(path, []byte("{}\n"), 0640) },
		"fifo":           func(path string) error { return syscall.Mkfifo(path, 0600) },
	}
	for name, create := range plant {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(directory, name+".jsonl")
			if err := create(path); err != nil {
				t.Fatal(err)
			}
			defer os.Remove(path)
			err := store.ReadLogs(ctx, "example", func(view logview.View) error {
				file, found, err := view.Open(ctx, logOperation+"/"+name+".jsonl")
				if file != nil {
					file.Close()
				}
				if found {
					t.Fatal("an unsafe file was opened")
				}
				return err
			})
			reported := diagnostics.Of(err)
			if len(reported) != 1 || reported[0].Code != "context.state" || !strings.Contains(reported[0].Message, path) {
				t.Fatalf("opening the %s = %#v, want context.state naming %s", name, reported, path)
			}
		})
	}
}

func TestLogOpenRefusesASymlinkedDirectoryInItsDescent(t *testing.T) {
	ctx := context.Background()
	store, _ := lifecycleFixture(t)
	appendLog(t, store, logOperation+"/blocks/network/attempt-000001.jsonl", "{}\n")
	logs := filepath.Join(contextLogsPath(store, "example"), operationLogs)
	const linked = "op-fedcba9876543210fedcba9876543210"
	if err := os.Symlink(logOperation, filepath.Join(logs, linked)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("blocks", filepath.Join(logs, logOperation, "linked")); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{linked + "/blocks/network/attempt-000001.jsonl", logOperation + "/linked/network/attempt-000001.jsonl"} {
		err := store.ReadLogs(ctx, "example", func(view logview.View) error {
			file, found, err := view.Open(ctx, target)
			if file != nil {
				file.Close()
			}
			if found {
				t.Fatalf("%s was opened through a symbolic link", target)
			}
			return err
		})
		if code := diagnosticCode(err); code != "context.state" {
			t.Fatalf("opening %s = %#v, want context.state", target, diagnostics.Of(err))
		}
	}
}

func holdWhile(t *testing.T, run func(held func()) error) (release func()) {
	t.Helper()
	ready, done, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		finished <- run(func() {
			close(ready)
			<-done
		})
	}()
	select {
	case <-ready:
	case err := <-finished:
		t.Fatalf("the holder ended before it held: %#v", diagnostics.Of(err))
	}
	return func() {
		close(done)
		if err := <-finished; err != nil {
			t.Fatalf("the holder failed: %#v", diagnostics.Of(err))
		}
	}
}

func mutating(t *testing.T, store *Store) bool {
	t.Helper()
	var answer bool
	if err := store.ReadLogs(context.Background(), "example", func(view logview.View) error {
		var err error
		answer, err = view.Mutating(context.Background())
		return err
	}); err != nil {
		t.Fatalf("the probe failed: %#v", diagnostics.Of(err))
	}
	return answer
}

func TestMutatingSeesAnExclusiveLeaseAndNotASharedOne(t *testing.T) {
	ctx := context.Background()
	store, _ := lifecycleFixture(t)
	release := holdWhile(t, func(held func()) error {
		return store.MutateLifecycle(ctx, "example", func(lifecycle.Transaction) error { held(); return nil })
	})
	if !mutating(t, store) {
		t.Error("the probe missed a live lifecycle mutation")
	}
	release()
	release = holdWhile(t, func(held func()) error {
		return store.RunLifecycle(ctx, "example", func(lifecycle.RunView) error { held(); return nil })
	})
	if mutating(t, store) {
		t.Error("the probe read a bounded run's shared lease as a mutation")
	}
	release()
	if mutating(t, store) {
		t.Error("the probe saw a mutation with no holder")
	}
	if err := store.MutateLifecycle(ctx, "example", func(lifecycle.Transaction) error { return nil }); err != nil {
		t.Fatalf("a lease taken right after the probe = %#v", diagnostics.Of(err))
	}
}

func TestRunLivenessFollowsItsRunsSharedLock(t *testing.T) {
	ctx := context.Background()
	store, _ := lifecycleFixture(t)
	var run string
	release := holdWhile(t, func(held func()) error {
		return store.RunLifecycle(ctx, "example", func(view lifecycle.RunView) error {
			name, err := view.OpenRun(ctx)
			if err != nil {
				return err
			}
			if err := view.Runs().Append(ctx, name, []byte("powering on\n")); err != nil {
				return err
			}
			run = strings.TrimSuffix(name, outputSuffix)
			held()
			return nil
		})
	})
	stream, found, err := store.OpenRun(ctx, "example", run)
	if err != nil || !found {
		release()
		t.Fatalf("the live run = %t %#v", found, diagnostics.Of(err))
	}
	defer stream.Close()
	if held, err := stream.Held(ctx); err != nil || !held {
		t.Errorf("a live run reads as held %t (%v)", held, err)
	}
	if stream.Name() != run || stream.Reference() != "contexts/example/runs/"+run+outputSuffix || readAt(t, stream) != "powering on\n" {
		t.Errorf("the run is %q at %q holding %q", stream.Name(), stream.Reference(), readAt(t, stream))
	}
	release()
	if held, err := stream.Held(ctx); err != nil || held {
		t.Errorf("a finished run reads as held %t (%v)", held, err)
	}
	if _, found, err := store.OpenRun(ctx, "example", "run-000099"); found || err != nil {
		t.Errorf("an absent run = %t %v, want absent", found, err)
	}
	if _, _, err := store.OpenRun(ctx, "example", "run-1"); diagnosticCode(err) != "context.state" {
		t.Errorf("a malformed run name = %#v", diagnostics.Of(err))
	}
}

// retryObserver is the context holdRun consults only after a hold met
// EWOULDBLOCK; its first consultation releases the probe.
type retryObserver struct {
	context.Context
	retried bool
	release func()
}

func (o *retryObserver) Done() <-chan struct{} {
	if !o.retried {
		o.retried = true
		o.release()
	}
	return o.Context.Done()
}

func TestARunStillHoldsItsFileBesideAnInstantProbe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run-000001.output")
	writePrivate(t, path, nil)
	probe, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Close()
	run, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer run.Close()
	if err := syscall.Flock(int(probe.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	observer := &retryObserver{Context: context.Background(), release: func() { syscall.Flock(int(probe.Fd()), syscall.LOCK_UN) }}
	if err := holdRun(observer, run); err != nil || !observer.retried {
		t.Fatalf("a run meeting a probe's instant hold = %v, retried %t", err, observer.retried)
	}
	if err := syscall.Flock(int(probe.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); !errors.Is(err, syscall.EWOULDBLOCK) {
		t.Fatalf("the run does not hold its file: %v", err)
	}
}

func TestSetupLivenessFollowsSetupLock(t *testing.T) {
	ctx := context.Background()
	store := New(testOptions(filepath.Join(t.TempDir(), "state")))
	if _, found, err := store.OpenSetup(ctx, ""); found || err != nil {
		t.Fatalf("a host that never ran setup = %t %v, want absent", found, err)
	}
	var id string
	release := holdWhile(t, func(held func()) error {
		return store.MutateSetup(ctx, func(tx prerequisites.SetupTransaction) error {
			name, run, err := tx.OpenRun(ctx)
			if err != nil {
				return err
			}
			defer run.Close()
			if _, err := run.Write([]byte("installing\n")); err != nil {
				return err
			}
			id = name
			held()
			return nil
		})
	})
	stream, found, err := store.OpenSetup(ctx, "")
	if err != nil || !found {
		release()
		t.Fatalf("the live setup = %t %#v", found, diagnostics.Of(err))
	}
	defer stream.Close()
	if held, err := stream.Held(ctx); err != nil || !held {
		t.Errorf("a live setup reads as held %t (%v)", held, err)
	}
	if stream.Name() != id || stream.Reference() != "controller/"+id+outputSuffix || readAt(t, stream) != "installing\n" {
		t.Errorf("the setup is %q at %q", stream.Name(), stream.Reference())
	}
	release()
	if held, err := stream.Held(ctx); err != nil || held {
		t.Errorf("a finished setup reads as held %t (%v)", held, err)
	}
	if named, found, err := store.OpenSetup(ctx, id); err != nil || !found || named.Name() != id {
		t.Errorf("the named setup = %t %v", found, err)
	} else {
		named.Close()
	}
	if _, found, err := store.OpenSetup(ctx, "install-000099"); found || err != nil {
		t.Errorf("an absent setup = %t %v, want absent", found, err)
	}
}

func TestNewestSetupOutputSurvivesPruningBetweenListAndOpen(t *testing.T) {
	ctx := context.Background()
	store := New(testOptions(filepath.Join(t.TempDir(), "state")))
	host := filepath.Join(store.options.Root+logsSuffix, logController)
	if err := os.MkdirAll(host, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(store.options.Root+logsSuffix, 0700); err != nil {
		t.Fatal(err)
	}
	writePrivate(t, filepath.Join(host, "install-000002.output"), []byte("second\n"))
	writePrivate(t, filepath.Join(host, "install-000003.output"), []byte("third\n"))
	root, err := store.openLogRoot(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	defer root.file.Close()
	held, err := store.logDirectory(ctx, root, logController, false)
	if err != nil {
		t.Fatal(err)
	}
	defer held.file.Close()
	listings := 0
	stream, found, err := store.openSetupOutput(ctx, held, "", func() {
		if listings++; listings == 1 {
			os.Remove(filepath.Join(host, "install-000003.output"))
			writePrivate(t, filepath.Join(host, "install-000004.output"), []byte("fourth\n"))
		}
	})
	if err != nil || !found {
		t.Fatalf("the newest setup after pruning = %t %#v", found, diagnostics.Of(err))
	}
	defer stream.Close()
	if stream.Name() != "install-000004" || readAt(t, stream) != "fourth\n" || listings != 2 {
		t.Fatalf("read %q after %d listings, want install-000004 after a second", stream.Name(), listings)
	}
}
