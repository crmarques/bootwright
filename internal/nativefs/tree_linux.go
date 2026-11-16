//go:build linux

package nativefs

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"slices"
	"syscall"
)

type Tree struct {
	Subject  string
	Device   uint64
	MaxDepth int
	Entries  *int
}

var ErrInvalidTree = errors.New("a tree removal needs an entry budget and a depth bound")

func RemoveTree(dir *os.Root, tree Tree, last ...string) error {
	if tree.Entries == nil || tree.MaxDepth < 0 {
		return ErrInvalidTree
	}
	return tree.empty(dir, 0, last)
}

func (t Tree) empty(dir *os.Root, depth int, last []string) error {
	if depth > t.MaxDepth {
		return errors.New(t.Subject + " depth bound")
	}
	names, err := t.names(dir)
	if err != nil {
		return err
	}
	slices.SortStableFunc(names, func(a, b string) int { return slices.Index(last, a) - slices.Index(last, b) })
	for _, name := range names {
		listed, err := dir.Lstat(name)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if stat, ok := listed.Sys().(*syscall.Stat_t); !ok || stat.Dev != t.Device {
			return errors.New(t.Subject + " crosses a filesystem")
		}
		if listed.IsDir() {
			if err := t.child(dir, name, listed, depth); err != nil {
				return err
			}
		}
		if err := dir.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (t Tree) child(parent *os.Root, name string, listed os.FileInfo, depth int) error {
	child, err := parent.OpenRoot(name)
	if err != nil {
		return err
	}
	defer child.Close()
	opened, err := child.Stat(".")
	if err != nil || !os.SameFile(listed, opened) {
		return errors.New(t.Subject + " entry changed")
	}
	return t.empty(child, depth+1, nil)
}

func (t Tree) names(dir *os.Root) ([]string, error) {
	listing, err := dir.Open(".")
	if err != nil {
		return nil, err
	}
	defer listing.Close()
	var names []string
	for {
		part, err := listing.Readdirnames(1024)
		if *t.Entries -= len(part); *t.Entries < 0 {
			return nil, errors.New(t.Subject + " entry bound")
		}
		names = append(names, part...)
		if errors.Is(err, io.EOF) {
			return names, nil
		}
		if err != nil {
			return nil, err
		}
	}
}
