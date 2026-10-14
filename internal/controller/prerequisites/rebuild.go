package prerequisites

import (
	"context"
	"errors"
	"fmt"
)

var ErrRuntimeWheelMissing = fmt.Errorf("%w: a locked wheel is not in the wheel cache", ErrRuntimeUnavailable)

var ErrRuntimeRebuildFailed = errors.New("the controller runtime could not be rebuilt from the wheel cache")

type rebuildFailure struct{ err error }

func (f *rebuildFailure) Error() string { return f.err.Error() }

func (f *rebuildFailure) Unwrap() error { return f.err }

func (f *rebuildFailure) Is(target error) bool { return target == ErrRuntimeRebuildFailed }

type RuntimeRebuilder struct {
	setup  SetupStore
	python PythonRuntime
	lock   RuntimeLock
}

func NewRuntimeRebuilder(setup SetupStore, python PythonRuntime, lock RuntimeLock) RuntimeRebuilder {
	return RuntimeRebuilder{setup: setup, python: python, lock: lock}
}

func (r RuntimeRebuilder) Rebuild(ctx context.Context, runtime Runtime) error {
	if r.setup == nil || r.python == nil || runtime.LockDigest != r.lock.Digest {
		return ErrRuntimeUnavailable
	}
	mutate := r.setup.MutateSetup
	if waiter, waits := r.setup.(SetupWaiter); waits {
		mutate = waiter.AwaitSetup
	}
	return mutate(ctx, func(tx SetupTransaction) error {
		view := tx.View()
		if err := SetupLaunchRefusal(view); err != nil {
			return err
		}
		receipt := view.State.Receipt
		if receipt == nil || receipt.Runtime.Interpreter != runtime.Interpreter.Path || receipt.Runtime.ABI != runtime.Interpreter.ABI {
			return ErrRuntimeUnavailable
		}
		interpreter, err := r.python.Select(ctx, receipt.Runtime.Interpreter)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil || interpreter.Path != runtime.Interpreter.Path || interpreter.ABI != runtime.Interpreter.ABI {
			return ErrRuntimeUnavailable
		}
		if present, ready, err := r.python.Inspect(ctx, interpreter); err == nil && ready && present.Digest == runtime.Digest {
			return nil
		}
		wheels, err := r.python.Wheels(ctx, interpreter.ABI)
		if err != nil {
			return err
		}
		for _, wheel := range wheels {
			if !wheel.Cached {
				return ErrRuntimeWheelMissing
			}
		}
		built, err := r.python.Build(ctx, tx, interpreter)
		if err != nil && ctx.Err() == nil && !errors.Is(err, ErrRuntimeUnavailable) {
			return &rebuildFailure{err: err}
		}
		if err != nil {
			return err
		}
		if built.Digest != runtime.Digest {
			return ErrRuntimeUnavailable
		}
		return nil
	})
}
