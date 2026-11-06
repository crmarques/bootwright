package nativeexec

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

var errUnqualified = errors.New("unqualified system executable")

func Qualify(path string) (string, error) {
	return qualify(path, filepath.EvalSymlinks, os.Lstat)
}

func qualify(path string, resolve func(string) (string, error), stat func(string) (fs.FileInfo, error)) (string, error) {
	if !filepath.IsAbs(path) {
		return "", errUnqualified
	}
	resolved, err := resolve(path)
	if err != nil {
		return "", err
	}
	info, err := stat(resolved)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 || !trusted(info) {
		return "", errUnqualified
	}
	for parent := filepath.Dir(resolved); ; parent = filepath.Dir(parent) {
		info, err := stat(parent)
		if err != nil || !info.IsDir() || !trusted(info) {
			return "", errUnqualified
		}
		if parent == "/" {
			return resolved, nil
		}
	}
}

func trusted(info fs.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == 0 && info.Mode().Perm()&0o022 == 0
}
