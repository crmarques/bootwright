//go:build linux && amd64

package venvlocal

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/crmarques/bootwright/internal/canonicaljson"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

// WithPython holds the runtime's launch lock shared for the whole block, so
// retirement, which takes it exclusively without waiting, never removes a
// runtime in use; it then repeats the ownership walk and the record check.
func (a *Adapter) WithPython(ctx context.Context, runtime prerequisites.Runtime, use func(prerequisites.PythonLaunch) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	directory, err := a.runtimeDirectory(runtime.Digest)
	if err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(directory, lockName), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return prerequisites.ErrRuntimeUnavailable
	}
	if err != nil {
		return failure("the controller runtime " + runtime.Digest[:12] + " cannot be opened")
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
		return failure("the controller runtime " + runtime.Digest[:12] + " is being retired")
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	if err := a.verifyRuntime(runtime); err != nil {
		return err
	}
	return use(prerequisites.PythonLaunch{
		Program: filepath.Join(directory, "bin", "python"), Shim: filepath.Join(directory, shimName),
		Arguments: []string{"-I", "-B"}, Directory: directory, Environment: cleanEnvironment(directory),
	})
}

// Published reports whether the runtime's launch lock exists, reading
// nothing else, so a caller refuses before it registers work.
func (a *Adapter) Published(ctx context.Context, runtime prerequisites.Runtime) (bool, error) {
	directory, err := a.runtimeDirectory(runtime.Digest)
	if err != nil {
		return false, err
	}
	info, err := os.Lstat(filepath.Join(directory, lockName))
	if errors.Is(err, fs.ErrNotExist) {
		return false, ctx.Err()
	}
	if err != nil || !info.Mode().IsRegular() {
		return false, failure("the controller runtime " + runtime.Digest[:12] + " holds no usable launch lock")
	}
	return true, ctx.Err()
}

// Inspect reports whether the interpreter's runtime is published and still
// verifies, including its import probe. An absent runtime is not ready and
// no error; a present one that fails a check returns why.
func (a *Adapter) Inspect(ctx context.Context, interpreter prerequisites.PythonInterpreter) (prerequisites.Runtime, bool, error) {
	runtime := a.runtimeFor(interpreter)
	directory, err := a.runtimeDirectory(runtime.Digest)
	if err != nil {
		return runtime, false, err
	}
	if _, err := os.Lstat(directory); errors.Is(err, fs.ErrNotExist) {
		return runtime, false, ctx.Err()
	}
	files, err := a.lockedFiles(interpreter.ABI)
	if err != nil {
		return runtime, false, err
	}
	if err := a.verifyRuntime(runtime); err != nil {
		return runtime, false, err
	}
	if err := a.probeVenv(ctx, directory, runtime, files); err != nil {
		return runtime, false, err
	}
	return runtime, true, nil
}

// verifyRuntime walks the runtime directory, its interpreter link and the
// interpreter's stdlib, and requires runtime.json to name this runtime.
func (a *Adapter) verifyRuntime(runtime prerequisites.Runtime) error {
	directory, err := a.runtimeDirectory(runtime.Digest)
	if err != nil {
		return err
	}
	if err := a.ownedDirectory(filepath.Join(directory, "bin")); err != nil {
		return err
	}
	record, err := readRecord(filepath.Join(directory, recordName))
	if err != nil {
		return err
	}
	recorded := record.Runtime
	if record.Format != prerequisites.RuntimeFormat || recorded.Digest != runtime.Digest || recorded.LockDigest != runtime.LockDigest ||
		recorded.Interpreter.Path != runtime.Interpreter.Path || recorded.Interpreter.ABI != runtime.Interpreter.ABI ||
		prerequisites.RuntimeDigest(recorded.LockDigest, recorded.Interpreter.ABI, recorded.Interpreter.Path) != recorded.Digest {
		return failure("the controller runtime " + runtime.Digest[:12] + " holds a runtime.json that names another runtime")
	}
	linked, err := a.ownedExecutable(filepath.Join(directory, "bin", "python"))
	if err != nil {
		return err
	}
	interpreter, err := a.ownedExecutable(recorded.Interpreter.Path)
	if err != nil {
		return err
	}
	if linked != interpreter {
		return failure("the controller runtime's bin/python does not lead to " + recorded.Interpreter.Path)
	}
	for _, stdlib := range recorded.Interpreter.Stdlib {
		if err := a.ownedDirectory(stdlib); err != nil {
			return err
		}
	}
	return nil
}

func readRecord(path string) (runtimeRecord, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return runtimeRecord{}, failure("the controller runtime has no runtime.json")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxRecord+1))
	var record runtimeRecord
	if err != nil || len(data) > maxRecord || canonicaljson.Decode(data, &record, canonicaljson.Line) != nil {
		return runtimeRecord{}, failure("the controller runtime's runtime.json is not one this build wrote")
	}
	return record, nil
}
