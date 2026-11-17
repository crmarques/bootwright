//go:build linux && amd64

package kernel

import (
	"errors"
	"io"
	"slices"
	"syscall"
)

// ListingKind is why HeldNames refused a listing it read itself, so each
// caller can report it under its own code.
type ListingKind uint8

const (
	ListingReplaced ListingKind = iota
	ListingUnreadable
	ListingOverLimit
)

type ListingFailure struct {
	Kind    ListingKind
	Failure error
}

func (e *ListingFailure) Error() string { return e.Failure.Error() }

func (e *ListingFailure) Unwrap() error { return e.Failure }

// HeldNames lists, sorted, the directory dir holds, through a fresh handle on
// that very directory rather than a resolution of its name, so a directory
// renamed over it is never what is listed. It verifies dir's location before
// and after, so a directory moved away during the listing refuses as replaced.
func HeldNames(dir *Directory, maximum int) ([]string, error) {
	if err := dir.Verify(); err != nil {
		return nil, err
	}
	file, err := OpenWithin(dir, ".", syscall.O_RDONLY|syscall.O_DIRECTORY, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	identity, err := StatHandle(file)
	if err != nil {
		return nil, &ListingFailure{ListingUnreadable, err}
	}
	if !SameIdentity(dir.Identity, identity) {
		return nil, &ListingFailure{ListingReplaced, state("held state directory was replaced")}
	}
	names := make([]string, 0, min(maximum, 1024))
	for len(names) <= maximum {
		part, readErr := file.Readdirnames(min(1024, maximum+1-len(names)))
		names = append(names, part...)
		if len(names) > maximum {
			return nil, &ListingFailure{ListingOverLimit, state("state directory entry count exceeds its limit")}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil || len(part) == 0 {
			return nil, &ListingFailure{ListingUnreadable, state("state directory cannot be enumerated")}
		}
	}
	if err := dir.Verify(); err != nil {
		return nil, err
	}
	slices.Sort(names)
	return names, nil
}

func DirectoryNames(dir *Directory, maximum int) ([]string, error) {
	return HeldNames(dir, maximum)
}
