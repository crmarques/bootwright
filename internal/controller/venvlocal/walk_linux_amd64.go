//go:build linux && amd64

package venvlocal

import (
	"os"
	"path/filepath"

	"github.com/crmarques/bootwright/internal/nativefs"
)

// ownedExecutable resolves path to an executable regular file a trusted
// account owns below directories it owns, none writable by a group or other
// account, sticky or not.
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
	info, err := a.rule().Walk(resolved)
	if fault, ok := err.(*nativefs.OwnershipFault); ok {
		return failure(fault.Path + " is writable by an account other than root, or not owned by root")
	}
	if err != nil || !info.IsDir() {
		return failure(resolved + " is writable by an account other than root, or not owned by root")
	}
	return nil
}

func (a *Adapter) rule() nativefs.Ownership {
	return nativefs.Ownership{Owners: a.trusted, Sticky: nativefs.StickyRefused, Top: a.top}
}

func (a *Adapter) owned(info os.FileInfo) bool {
	return a.rule().AdmitsInfo(info) == nativefs.Admitted
}
