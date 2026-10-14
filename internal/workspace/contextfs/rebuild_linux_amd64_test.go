//go:build linux && amd64

package contextfs

import (
	"context"
	"errors"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

type rebuildPython struct {
	prerequisites.PythonRuntime
	selected prerequisites.PythonInterpreter
	cached   bool
	build    error
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
	return prerequisites.Runtime{}, p.build
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
