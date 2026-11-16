package nativefs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"strings"
)

type Publication struct {
	Prefix string
	Mode   os.FileMode
	step   func(string) error
}

type UnsyncedError struct{ Err error }

func (e *UnsyncedError) Error() string { return e.Err.Error() }

func (e *UnsyncedError) Unwrap() error { return e.Err }

var ErrInvalidName = errors.New("name is not one path component")

func component(name string) bool {
	return name != "" && name != "." && name != ".." && len(name) <= 255 && !strings.ContainsAny(name, "/\x00")
}

var ErrInvalidMode = errors.New("a publication needs permission bits and nothing else")

var ErrStageReplaced = errors.New("the staged file was replaced before publication")

func Publish(ctx context.Context, root *os.Root, name string, data []byte, how Publication) error {
	if !component(name) {
		return ErrInvalidName
	}
	if how.Mode == 0 || how.Mode&^os.ModePerm != 0 {
		return ErrInvalidMode
	}
	stage, handle, err := createStage(root, how.Prefix)
	if err != nil {
		return err
	}
	published := false
	defer func() {
		if !published {
			discardStage(root, stage, handle)
			handle.Close()
		}
	}()
	for _, step := range []func() error{
		func() error { return how.pause("created") },
		func() error { _, err := handle.Write(data); return err },
		func() error { return handle.Chmod(how.Mode) },
		func() error { return how.pause("written") },
		handle.Sync,
		func() error { return how.pause("synced") },
		func() error { return sameStage(root, stage, handle) },
		ctx.Err,
	} {
		if err := step(); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
	}
	if err := root.Rename(stage, name); err != nil {
		return err
	}
	published = true
	if err := handle.Close(); err != nil {
		return &UnsyncedError{Err: err}
	}
	if err := how.pause("renamed"); err != nil {
		return &UnsyncedError{Err: err}
	}
	synced, err := root.Open(".")
	if err == nil {
		err = synced.Sync()
		synced.Close()
	}
	if err != nil {
		return &UnsyncedError{Err: err}
	}
	return nil
}

func (p Publication) pause(point string) error {
	if p.step == nil {
		return nil
	}
	return p.step(point)
}

func createStage(root *os.Root, prefix string) (string, *os.File, error) {
	var last error
	for range 8 {
		var random [8]byte
		if _, err := rand.Read(random[:]); err != nil {
			return "", nil, err
		}
		name := prefix + hex.EncodeToString(random[:]) + ".part"
		handle, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			return name, handle, nil
		}
		last = err
		if !errors.Is(err, fs.ErrExist) {
			break
		}
	}
	return "", nil, last
}

func sameStage(root *os.Root, stage string, handle *os.File) error {
	held, err := handle.Stat()
	if err != nil {
		return err
	}
	named, err := root.Lstat(stage)
	if err != nil || !os.SameFile(held, named) {
		return ErrStageReplaced
	}
	return nil
}

func discardStage(root *os.Root, stage string, handle *os.File) {
	held, err := handle.Stat()
	if err != nil {
		return
	}
	if named, err := root.Lstat(stage); err == nil && os.SameFile(held, named) {
		root.Remove(stage)
	}
}
