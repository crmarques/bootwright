package venvlocal

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/controller/runtimelock"
)

const exportedLockName = "lock.json"

func (a *Adapter) Export(ctx context.Context, destination string, egress prerequisites.SetupEgress, mirror prerequisites.WheelMirror) (prerequisites.WheelExport, error) {
	export, err := a.export(ctx, destination, egress, mirror)
	var scoped *prerequisites.ScopedFailure
	if errors.As(err, &scoped) && scoped.Command == "" {
		scoped.Command = "repeat this command"
	}
	return export, err
}

func (a *Adapter) export(ctx context.Context, destination string, egress prerequisites.SetupEgress, mirror prerequisites.WheelMirror) (prerequisites.WheelExport, error) {
	directory, err := filepath.Abs(destination)
	if err != nil || os.MkdirAll(directory, 0o755) != nil {
		return prerequisites.WheelExport{}, exportFailure("the directory "+destination+" cannot be created", "Pass a directory this account can write")
	}
	if info, err := os.Stat(directory); err != nil || !info.IsDir() {
		return prerequisites.WheelExport{}, exportFailure(directory+" is not a directory", "Pass a directory this account can write")
	}
	export := prerequisites.WheelExport{Directory: directory}
	origins := map[string]string{}
	var pending []runtimelock.File
	remote := false
	for _, file := range a.exportedFiles() {
		origin := prerequisites.OriginPresent
		switch {
		case verifiedFile(filepath.Join(directory, file.Filename), file) == nil:
		case a.cached(file) == nil:
			origin = prerequisites.OriginCache
		case mirror.Set():
			origin, remote = prerequisites.OriginMirror, true
		default:
			origin, remote = prerequisites.OriginDownload, true
		}
		if origin != prerequisites.OriginPresent {
			pending = append(pending, file)
		}
		origins[file.Filename] = origin
		export.Files = append(export.Files, prerequisites.ExportedWheel{Filename: file.Filename, SHA256: file.SHA256, Size: file.Size, Origin: origin})
	}
	var network wheelSource
	if remote {
		if network, err = a.source(ctx, egress, mirror); err != nil {
			return prerequisites.WheelExport{}, err
		}
	}
	err = a.fetchAll(ctx, pending, func(ctx context.Context, file runtimelock.File) error {
		source := network
		if origins[file.Filename] == prerequisites.OriginCache {
			source = a.cacheSource()
		}
		return place(ctx, source, file, directory, filepath.Join(directory, file.Filename), 0o644)
	})
	if err != nil {
		return prerequisites.WheelExport{}, err
	}
	if err := writeAtomically(directory, exportedLockName, a.lock.Source()); err != nil {
		return prerequisites.WheelExport{}, err
	}
	return export, nil
}

func (a *Adapter) exportedFiles() []runtimelock.File {
	var files []runtimelock.File
	for _, abi := range a.lock.PythonABIs() {
		locked, _ := a.lock.Files(abi)
		files = append(files, locked...)
	}
	files = append(files, a.lock.Installer())
	slices.SortFunc(files, func(left, right runtimelock.File) int { return strings.Compare(left.Filename, right.Filename) })
	return slices.CompactFunc(files, func(left, right runtimelock.File) bool { return left.Filename == right.Filename })
}

func (a *Adapter) cacheSource() wheelSource {
	return func(ctx context.Context, file runtimelock.File, destination io.Writer) error {
		handle, err := openRegular(a.cachePath(file), file.Size)
		if err != nil {
			return failure("the wheel cache does not hold " + file.Filename)
		}
		defer handle.Close()
		if written, err := io.Copy(destination, io.LimitReader(handle, file.Size+1)); err != nil || written != file.Size {
			return errorOr(ctx, failure("the wheel cache does not hold "+file.Filename))
		}
		return nil
	}
}

func errorOr(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func writeAtomically(directory, name string, data []byte) error {
	failed := exportFailure(name+" cannot be written in "+directory, "Pass a directory this account can write")
	partial, err := os.CreateTemp(directory, ".part-*")
	if err != nil {
		return failed
	}
	defer func() {
		partial.Close()
		os.Remove(partial.Name())
	}()
	if _, err := partial.Write(data); err != nil || partial.Chmod(0o644) != nil || partial.Sync() != nil || partial.Close() != nil ||
		os.Rename(partial.Name(), filepath.Join(directory, name)) != nil {
		return failed
	}
	return nil
}

func exportFailure(message, correction string) error {
	return &prerequisites.ScopedFailure{Message: message, Correction: correction, Command: "repeat this command"}
}
