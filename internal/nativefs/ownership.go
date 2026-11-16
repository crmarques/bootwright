package nativefs

import (
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

type Sticky int

const (
	StickyRefused Sticky = iota
	StickyRootOwned
	StickyTrustedOwner
)

type Verdict int

const (
	Admitted Verdict = iota
	Unavailable
	ForeignOwner
	Writable
	NotDirectory
)

type Ownership struct {
	Owners func(uint32) bool
	Sticky Sticky
	Top    string
	Lstat  func(string) (fs.FileInfo, error)
}

type OwnershipFault struct {
	Path    string
	UID     uint32
	Verdict Verdict
}

func (f *OwnershipFault) Error() string {
	switch f.Verdict {
	case ForeignOwner:
		return f.Path + " is owned by uid " + strconv.FormatUint(uint64(f.UID), 10)
	case Writable:
		return f.Path + " is writable by group or others"
	case NotDirectory:
		return f.Path + " is not a directory"
	}
	return f.Path + " is unavailable"
}

func RootOwner(uid uint32) bool { return uid == 0 }

func AccountOwner(uid uint32) func(uint32) bool {
	return func(owner uint32) bool { return owner == uid }
}

func RootOrAccountOwner(uid uint32) func(uint32) bool {
	return func(owner uint32) bool { return owner == 0 || owner == uid }
}

func RawMode(raw uint32) fs.FileMode {
	mode := fs.FileMode(raw & 0o777)
	if raw&syscall.S_IFMT == syscall.S_IFDIR {
		mode |= fs.ModeDir
	}
	if raw&syscall.S_ISVTX != 0 {
		mode |= fs.ModeSticky
	}
	return mode
}

func (o Ownership) Admits(uid uint32, mode fs.FileMode) Verdict {
	if o.Owners == nil || !o.Owners(uid) {
		return ForeignOwner
	}
	if mode.Perm()&0o022 != 0 && !o.stickyExempt(uid, mode) {
		return Writable
	}
	return Admitted
}

func (o Ownership) stickyExempt(uid uint32, mode fs.FileMode) bool {
	if !mode.IsDir() || mode&fs.ModeSticky == 0 {
		return false
	}
	switch o.Sticky {
	case StickyRootOwned:
		return uid == 0
	case StickyTrustedOwner:
		return true
	}
	return false
}

func (o Ownership) AdmitsInfo(info fs.FileInfo) Verdict {
	if info == nil {
		return Unavailable
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return Unavailable
	}
	return o.Admits(stat.Uid, info.Mode())
}

func (o Ownership) Walk(path string) (fs.FileInfo, error) {
	lstat := o.Lstat
	if lstat == nil {
		lstat = os.Lstat
	}
	if !filepath.IsAbs(path) {
		return nil, &OwnershipFault{Path: path, Verdict: Unavailable}
	}
	var leaf fs.FileInfo
	for current := path; ; current = filepath.Dir(current) {
		info, err := lstat(current)
		if err != nil {
			return nil, &OwnershipFault{Path: current, Verdict: Unavailable}
		}
		if leaf == nil {
			leaf = info
		} else if !info.IsDir() {
			return nil, &OwnershipFault{Path: current, Verdict: NotDirectory}
		}
		if verdict := o.AdmitsInfo(info); verdict != Admitted {
			fault := &OwnershipFault{Path: current, Verdict: verdict}
			if stat, ok := info.Sys().(*syscall.Stat_t); ok {
				fault.UID = stat.Uid
			}
			return nil, fault
		}
		if current == "/" || current == o.Top {
			return leaf, nil
		}
	}
}
