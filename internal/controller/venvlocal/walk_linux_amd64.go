//go:build linux && amd64

package venvlocal

import (
	"os"
	"path/filepath"
	"syscall"
)

// ownedExecutable resolves path to an executable regular file a trusted
// account owns below directories it owns, none writable by a group or other
// account, as privilege.QualifiedSudo does for sudo.
func (a *Adapter) ownedExecutable(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", failure(path + " is not installed")
	}
	info, err := os.Lstat(resolved)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 || !a.owned(info) {
		return "", failure(resolved + " is not an executable regular file root owns without group or other write")
	}
	if err := a.ownedDirectory(filepath.Dir(resolved)); err != nil {
		return "", err
	}
	return resolved, nil
}

// ownedDirectory holds a directory and every parent to the same rule.
func (a *Adapter) ownedDirectory(path string) error {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || !filepath.IsAbs(resolved) {
		return failure(path + " is not an installed directory")
	}
	for directory := resolved; ; directory = filepath.Dir(directory) {
		info, err := os.Lstat(directory)
		if err != nil || !info.IsDir() || !a.owned(info) {
			return failure(directory + " is writable by an account other than root, or not owned by root")
		}
		if directory == "/" || directory == a.top {
			return nil
		}
	}
}

func (a *Adapter) owned(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && a.trusted(stat.Uid) && info.Mode().Perm()&0o022 == 0
}
