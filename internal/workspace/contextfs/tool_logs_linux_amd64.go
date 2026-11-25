//go:build linux && amd64

package contextfs

import (
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/crmarques/bootwright/internal/reconciliation/logview"
	"github.com/crmarques/bootwright/internal/workspace/contextfs/kernel"
)

var errUnsafeToolLog = errors.New("tool log is not a singly linked regular file in its private work directory")

func OpenNativeToolLog(path string) (logview.LogFile, string, error) {
	return openNativeToolLog(productionHosted, path, 0, 0)
}

func openNativeToolLog(root, path string, uid, gid uint32) (logview.LogFile, string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, "", errUnsafeToolLog
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, "", errUnsafeToolLog
	}
	parts := strings.Split(relative, string(filepath.Separator))
	dir, err := kernel.WalkAbsolute(root, nil)
	if errors.Is(err, syscall.ENOENT) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	defer dir.Close()
	parent, err := kernel.StatHandle(dir)
	if err != nil || !kernel.Private(parent, syscall.S_IFDIR, uid, gid) {
		return nil, "", errUnsafeToolLog
	}
	held := &directory{File: dir, Path: root, Identity: parent}
	for _, part := range parts[:len(parts)-1] {
		child, err := kernel.OpenDirectory(held, part)
		if errors.Is(err, syscall.ENOENT) {
			return nil, "", nil
		}
		if err != nil {
			return nil, "", err
		}
		defer child.Close()
		held = child
	}
	file, err := kernel.OpenRelative(held, parts[len(parts)-1], syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
	if errors.Is(err, syscall.ENOENT) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	entry, err := kernel.StatHandle(file)
	if err != nil || entry.Mode&syscall.S_IFMT != syscall.S_IFREG || entry.Nlink != 1 ||
		entry.Uid != uid || entry.Gid != gid || entry.Mode&07022 != 0 || entry.Dev != parent.Dev {
		file.Close()
		return nil, "", errUnsafeToolLog
	}
	identity := strconv.FormatUint(entry.Dev, 10) + ":" + strconv.FormatUint(entry.Ino, 10)
	return &logFile{file: file}, identity, nil
}
