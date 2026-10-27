//go:build linux && amd64

package contextfs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

type rebuildPython struct {
	prerequisites.PythonRuntime
	selected prerequisites.PythonInterpreter
	cached   bool
	build    error
	built    prerequisites.Runtime
}

func (p rebuildPython) Select(context.Context, string) (prerequisites.PythonInterpreter, error) {
	return p.selected, nil
}

func (p rebuildPython) Inspect(context.Context, prerequisites.PythonInterpreter) (prerequisites.Runtime, bool, error) {
	return prerequisites.Runtime{}, false, nil
}

func (p rebuildPython) Wheels(context.Context, string) ([]prerequisites.RuntimeWheel, error) {
	return []prerequisites.RuntimeWheel{{Filename: "ansible_core-2.21.6-py3-none-any.whl", Cached: p.cached}}, nil
}

func (p rebuildPython) Build(context.Context, prerequisites.RuntimePublisher, prerequisites.PythonInterpreter) (prerequisites.Runtime, error) {
	return p.built, p.build
}

func TestARebuildOnTheRealStoreKeepsItsRefusal(t *testing.T) {
	interpreter := prerequisites.PythonInterpreter{Path: "/usr/bin/python3.14", ABI: "cp314"}
	lock := prerequisites.RuntimeLock{Digest: setupDigest(1)}
	for _, test := range []struct {
		name        string
		python      rebuildPython
		interpreter string
		want        error
	}{
		{"a missing wheel", rebuildPython{selected: interpreter}, interpreter.Path, prerequisites.ErrRuntimeWheelMissing},
		{"an interpreter that no longer qualifies", rebuildPython{selected: prerequisites.PythonInterpreter{Path: interpreter.Path, ABI: "cp313"}, cached: true}, interpreter.Path, prerequisites.ErrRuntimeUnavailable},
		{"a receipt naming another interpreter", rebuildPython{selected: interpreter, cached: true}, "/usr/bin/python3.13", prerequisites.ErrRuntimeUnavailable},
		{"a failed build", rebuildPython{selected: interpreter, cached: true, build: errors.New("pip failed")}, interpreter.Path, prerequisites.ErrRuntimeRebuildFailed},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := mediaFixture(t)
			mutateSetup(t, store, func(tx prerequisites.SetupTransaction) error {
				return tx.Record(context.Background(), setupReceiptState())
			})
			runtime := prerequisites.Runtime{
				Digest: prerequisites.RuntimeDigest(lock.Digest, "cp314", test.interpreter), LockDigest: lock.Digest,
				Interpreter: prerequisites.PythonInterpreter{Path: test.interpreter, ABI: "cp314"},
			}
			err := prerequisites.NewRuntimeRebuilder(store, test.python, lock).Rebuild(context.Background(), runtime)
			if !errors.Is(err, test.want) {
				t.Fatalf("Rebuild = %#v, want %v", err, test.want)
			}
		})
	}
}

// A launch whose runtime is gone while a setup holds setup/lock waits for that
// setup to finish and then rebuilds from the cache, rather than refusing the
// way a second setup does.
func TestARebuildWaitsForAHeldSetupLockThenRebuilds(t *testing.T) {
	interpreter := prerequisites.PythonInterpreter{Path: "/usr/bin/python3.14", ABI: "cp314"}
	lock := prerequisites.RuntimeLock{Digest: setupDigest(1)}
	runtime := prerequisites.Runtime{Digest: prerequisites.RuntimeDigest(lock.Digest, "cp314", interpreter.Path), LockDigest: lock.Digest, Interpreter: interpreter}
	store := mediaFixture(t)
	mutateSetup(t, store, func(tx prerequisites.SetupTransaction) error {
		return tx.Record(context.Background(), setupReceiptState())
	})
	release, entered := make(chan struct{}), make(chan struct{})
	held := make(chan error, 1)
	go func() {
		held <- store.MutateSetup(context.Background(), func(prerequisites.SetupTransaction) error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered
	rebuilt := make(chan error, 1)
	go func() {
		python := rebuildPython{selected: interpreter, cached: true, built: runtime}
		rebuilt <- prerequisites.NewRuntimeRebuilder(store, python, lock).Rebuild(context.Background(), runtime)
	}()
	select {
	case err := <-rebuilt:
		t.Fatalf("the rebuild did not wait for the held setup lock: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if err := <-held; err != nil {
		t.Fatal(err)
	}
	if err := <-rebuilt; err != nil {
		t.Fatalf("the rebuild after the setup released its lock = %#v", err)
	}
}
