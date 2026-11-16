package nativeexec

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/crmarques/bootwright/internal/nativefs"
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
	info, err := nativefs.Ownership{Owners: nativefs.RootOwner, Sticky: nativefs.StickyRefused, Lstat: stat}.Walk(resolved)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", errUnqualified
	}
	return resolved, nil
}
