package venvlocal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/controller/runtimelock"
)

// Wheels lists the ABI's files and the installer; one neither cached nor
// carried by this executable refuses, so a build without its wheels is named
// before setup plans anything.
func (a *Adapter) Wheels(ctx context.Context, abi string) ([]prerequisites.RuntimeWheel, error) {
	files, err := a.lockedFiles(abi)
	if err != nil {
		return nil, err
	}
	var wheels []prerequisites.RuntimeWheel
	for _, file := range append(files, a.lock.Installer()) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		cached := a.cached(file) == nil
		if !cached && !a.carries(file) {
			return nil, notCarried(file)
		}
		wheels = append(wheels, prerequisites.RuntimeWheel{Filename: file.Filename, Size: file.Size, Cached: cached})
	}
	return wheels, nil
}

// Fetch fills the wheel cache with the ABI's files and the installer from the
// wheels this executable carries; a verified cached file is not copied again.
func (a *Adapter) Fetch(ctx context.Context, abi string) error {
	files, err := a.lockedFiles(abi)
	if err != nil {
		return err
	}
	for _, file := range append(files, a.lock.Installer()) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if a.cached(file) == nil {
			continue
		}
		directory := filepath.Join(a.wheels, file.SHA256)
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return failure("the wheel cache cannot be written")
		}
		if err := place(ctx, a.carriedSource(), file, directory, a.cachePath(file), 0o600); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (a *Adapter) carries(file runtimelock.File) bool {
	info, err := fs.Stat(a.carried, file.Filename)
	return err == nil && info.Mode().IsRegular() && info.Size() == file.Size
}

func notCarried(file runtimelock.File) error {
	return &prerequisites.ScopedFailure{
		Message:    "this Bootwright executable does not carry the locked wheel " + file.Filename + ", so it was built without its wheels",
		Correction: "Install a Bootwright executable built with make build, which places and checks the locked wheels",
		Command:    "repeat this command with that executable",
	}
}

func (a *Adapter) cachePath(file runtimelock.File) string {
	return filepath.Join(a.wheels, file.SHA256, file.Filename)
}

// cached proves a cached wheel is a regular file of its locked size and SHA-256.
func (a *Adapter) cached(file runtimelock.File) error {
	if verifiedFile(a.cachePath(file), file) != nil {
		return failure("the wheel cache does not hold " + file.Filename)
	}
	return nil
}

func verifiedFile(path string, file runtimelock.File) error {
	handle, err := openRegular(path, file.Size)
	if err != nil {
		return failure(file.Filename + " is not a regular file of its locked size")
	}
	defer handle.Close()
	return verifyWheel(file, handle)
}

func openRegular(path string, size int64) (*os.File, error) {
	handle, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	info, err := handle.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != size {
		handle.Close()
		return nil, errors.New("not a regular file of its locked size")
	}
	return handle, nil
}

func verifyWheel(file runtimelock.File, reader io.Reader) error {
	hash := sha256.New()
	written, err := io.Copy(hash, io.LimitReader(reader, file.Size+1))
	if err != nil || written != file.Size || hex.EncodeToString(hash.Sum(nil)) != file.SHA256 {
		return failure(file.Filename + " does not match its locked size and SHA-256")
	}
	return nil
}

type wheelSource func(ctx context.Context, file runtimelock.File, destination io.Writer) error

func (a *Adapter) carriedSource() wheelSource {
	return func(ctx context.Context, file runtimelock.File, destination io.Writer) error {
		if !a.carries(file) {
			return notCarried(file)
		}
		handle, err := a.carried.Open(file.Filename)
		if err != nil {
			return notCarried(file)
		}
		defer handle.Close()
		if written, err := io.Copy(destination, io.LimitReader(handle, file.Size+1)); err != nil || written != file.Size {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return notCarried(file)
		}
		return nil
	}
}

// place writes a wheel beside its cache name and renames it into place only
// once its size and SHA-256 match the lock.
func place(ctx context.Context, source wheelSource, file runtimelock.File, directory, final string, mode os.FileMode) error {
	partial, err := os.CreateTemp(directory, ".part-*")
	if err != nil {
		return failure("the wheel cache cannot be written")
	}
	defer func() {
		partial.Close()
		os.Remove(partial.Name())
	}()
	if err := source(ctx, file, partial); err != nil {
		return err
	}
	if _, err := partial.Seek(0, io.SeekStart); err != nil {
		return failure("the wheel cache cannot be read back")
	}
	if err := verifyWheel(file, partial); err != nil {
		return err
	}
	if partial.Chmod(mode) != nil || partial.Sync() != nil || partial.Close() != nil || os.Rename(partial.Name(), final) != nil {
		return failure("the wheel cache cannot be written")
	}
	return nil
}
