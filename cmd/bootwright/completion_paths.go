package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Completion enumerates paths here rather than in a shell script, because a
// shell-side expansion of a partly typed word would evaluate it. Reads and
// results are bounded so a completion request cannot be turned into an
// unbounded read.
const (
	maxCompletionPaths       = 256
	maxCompletionEntry       = 255
	maxCompletionPrefix      = 4096
	maxCompletionEntriesRead = 4096
)

func completionPaths(kind, prefix string) []string {
	if kind != "directory" && kind != "file" || len(prefix) > maxCompletionPrefix || strings.ContainsAny(prefix, "\x00\n\r") {
		return nil
	}
	directory, partial := prefix, ""
	if index := strings.LastIndexByte(prefix, filepath.Separator); index >= 0 {
		directory, partial = prefix[:index+1], prefix[index+1:]
	} else {
		directory, partial = "", prefix
	}
	root := directory
	if root == "" {
		root = "."
	}
	listing, err := os.Open(root)
	if err != nil {
		return nil
	}
	defer listing.Close()
	return listCompletionPaths(listing.ReadDir, kind, root, directory, partial)
}

// Each read asks for no more entries than could still become candidates, so
// the directory is read no further than the first maxCompletionPaths matches
// or maxCompletionEntriesRead entries, whichever comes first.
func listCompletionPaths(read func(int) ([]os.DirEntry, error), kind, root, directory, partial string) []string {
	var candidates []string
	for examined := 0; examined < maxCompletionEntriesRead && len(candidates) < maxCompletionPaths; {
		entries, err := read(min(maxCompletionPaths-len(candidates), maxCompletionEntriesRead-examined))
		if err != nil && !errors.Is(err, io.EOF) {
			return nil
		}
		examined += len(entries)
		for _, entry := range entries {
			name := entry.Name()
			if !strings.HasPrefix(name, partial) || !safeCompletionEntry(name) {
				continue
			}
			// A dot entry is offered only once it has been asked for by name.
			if strings.HasPrefix(name, ".") && !strings.HasPrefix(partial, ".") {
				continue
			}
			isDirectory := entry.IsDir()
			if !isDirectory && entry.Type()&os.ModeSymlink != 0 {
				if info, err := os.Stat(filepath.Join(root, name)); err == nil {
					isDirectory = info.IsDir()
				}
			}
			if kind == "directory" && !isDirectory {
				continue
			}
			value := directory + name
			if isDirectory {
				value += string(filepath.Separator)
			}
			candidates = append(candidates, value)
			if len(candidates) >= maxCompletionPaths {
				break
			}
		}
		if err != nil || len(entries) == 0 {
			break
		}
	}
	slices.Sort(candidates)
	return candidates
}

// A candidate crosses a line-delimited protocol and is then inserted into a
// shell word, so anything that could not survive that intact is withheld.
func safeCompletionEntry(name string) bool {
	if name == "" || len(name) > maxCompletionEntry {
		return false
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return !strings.ContainsAny(name, "\\\"'`$&|;<>*?[](){}!~#@= \t")
}
