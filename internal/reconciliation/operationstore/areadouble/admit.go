// Package areadouble is what every in-memory operationstore.Area double
// refuses before it answers and what it lists, decided over that double's own
// records and directories, so the doubles areacontract.Verify holds share one
// copy. It imports no first-party package, so operationstore's own tests can
// use it, and no production package imports it.
package areadouble

import (
	"context"
	"errors"
	"io/fs"
	"maps"
	"path"
	"slices"
	"strings"
)

// Admit refuses, before any injected failure or hook runs, what the contract
// refuses: a cancelled context, a path outside the area, a path beneath a
// record, and a record where a directory is named or the reverse. files holds
// the double's records by path and directories the directories it was asked
// to create, since a claimed operation directory holds no file.
func Admit(ctx context.Context, files map[string][]byte, directories map[string]bool, target string, record bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !(fs.ValidPath(target) && target != ".") && (record || target != "") {
		return errors.New("path escapes the area")
	}
	for parent := path.Dir(target); target != "" && parent != "."; parent = path.Dir(parent) {
		if _, isRecord := files[parent]; isRecord {
			return errors.New("path lies beneath a record")
		}
	}
	if _, isRecord := files[target]; !record && isRecord {
		return errors.New("not a directory")
	}
	if record && IsDirectory(files, directories, target) {
		return errors.New("is a directory")
	}
	return nil
}

// Entries is what a double lists for a target Admit accepted: each name the
// directory holds directly once, in name order, a record with its size and a
// directory with none, and nothing for an absent directory. E is the port's
// entry type, which this package cannot import.
func Entries[E ~struct {
	Name      string
	Directory bool
	Size      int64
}](files map[string][]byte, directories map[string]bool, target string) []E {
	prefix := target
	if prefix != "" {
		prefix += "/"
	}
	sizes := map[string]int64{}
	for name, data := range files {
		rest, ok := strings.CutPrefix(name, prefix)
		if !ok || rest == "" {
			continue
		}
		if head, _, nested := strings.Cut(rest, "/"); nested {
			sizes[head] = -1
		} else {
			sizes[rest] = int64(len(data))
		}
	}
	for name := range directories {
		if rest, ok := strings.CutPrefix(name, prefix); ok && rest != "" && !strings.Contains(rest, "/") {
			sizes[rest] = -1
		}
	}
	entries := make([]E, 0, len(sizes))
	for _, name := range slices.Sorted(maps.Keys(sizes)) {
		if sizes[name] < 0 {
			entries = append(entries, E{Name: name, Directory: true})
		} else {
			entries = append(entries, E{Name: name, Size: sizes[name]})
		}
	}
	return entries
}

// IsDirectory reports whether target is a directory of the double: one it was
// asked to create, or one a record lies beneath.
func IsDirectory(files map[string][]byte, directories map[string]bool, target string) bool {
	if directories[target] {
		return true
	}
	for name := range files {
		if strings.HasPrefix(name, target+"/") {
			return true
		}
	}
	return false
}
