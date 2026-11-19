//go:build linux && amd64

package ansiblerunner

import (
	"errors"
	"io/fs"
	"os"
	"strconv"
	"syscall"
)

const (
	stagingParent = "/var/lib/bootwright-staging"
	scratchParent = stagingParent + "/runs"
	statNoexec    = 0x8
)

func (r Runner) ensureScratchParent(create bool) error {
	owner := uint32(os.Geteuid())
	if r.stagingParent != "" {
		if !r.ownedDirectory(create, r.stagingParent, 0711, owner, func(mode fs.FileMode) bool { return mode&0022 == 0 }) {
			return r.unsafeParent(r.stagingParent, owner, "0711")
		}
	}
	if !r.ownedDirectory(create, r.scratchParent, 0700, owner, func(mode fs.FileMode) bool { return mode == 0700 }) {
		return r.unsafeParent(r.scratchParent, owner, "0700")
	}
	statfs := r.statfs
	if statfs == nil {
		statfs = syscall.Statfs
	}
	var filesystem syscall.Statfs_t
	if err := statfs(r.scratchParent, &filesystem); err != nil {
		return runnerScratchUnusable.Fail("the filesystem of the run scratch directory "+r.scratchParent+" cannot be read", "")
	}
	if filesystem.Flags&statNoexec != 0 {
		return runnerScratchUnusable.Fail("the run scratch directory "+r.scratchParent+" lies on a noexec filesystem",
			"remount the filesystem that holds "+r.scratchParent+" with exec, then repeat the command")
	}
	return nil
}

func (r Runner) ownedDirectory(create bool, path string, mode fs.FileMode, owner uint32, admits func(fs.FileMode) bool) bool {
	if create {
		if err := os.Mkdir(path, mode); err != nil && !errors.Is(err, fs.ErrExist) {
			return false
		}
	}
	listed, err := os.Lstat(path)
	if err != nil || !listed.IsDir() {
		return false
	}
	stat, ok := listed.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == owner && admits(listed.Mode().Perm())
}

func (r Runner) unsafeParent(path string, owner uint32, mode string) error {
	return runnerScratchUnusable.Fail("the run scratch parent "+path+" is not a directory this user owns and alone may write",
		"remove "+path+" or make it a directory owned by user "+strconv.FormatUint(uint64(owner), 10)+" with mode "+mode+", then repeat the command")
}
