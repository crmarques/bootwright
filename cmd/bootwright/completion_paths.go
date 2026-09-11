package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Completion enumerates paths here rather than in a shell script, because a
// shell-side expansion of a partly typed word would evaluate it. Results are
// bounded so a completion request cannot be turned into an unbounded read.
const (
	maxCompletionPaths = 256
	maxCompletionEntry = 255
)

func completionPaths(kind, prefix string) []string {
	if kind != "directory" && kind != "file" || len(prefix) > 4096 || strings.ContainsAny(prefix, "\x00\n\r") {
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
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var candidates []string
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
	return !strings.ContainsAny(name, "\\\"'`$&|;<>*?[](){}!~# \t")
}
