package main

import (
	"fmt"
	"slices"
	"strings"

	"golang.org/x/mod/modfile"
)

func goModModules(data []byte, entry pin, ref string) ([]clientModule, error) {
	file, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return nil, fmt.Errorf("the %s %s go.mod does not parse: %w", entry.Kind, entry.Default.Version, err)
	}
	var modules []clientModule
	for _, need := range file.Require {
		path, version := need.Mod.Path, need.Mod.Version
		replace := replacementOf(file.Replace, path, version)
		if replace != nil {
			if replace.New.Version == "" {
				if !strings.HasPrefix(replace.New.Path, "./") {
					return nil, fmt.Errorf("the %s %s go.mod replaces %s with %s, a local path outside its own tree, which this gate cannot judge", entry.Kind, entry.Default.Version, path, replace.New.Path)
				}
				continue
			}
			path, version = replace.New.Path, replace.New.Version
		}
		candidate := clientModule{module: path, version: version, ref: ref, detail: path + "@" + version}
		if !slices.Contains(modules, candidate) {
			modules = append(modules, candidate)
		}
	}
	if len(modules) == 0 {
		return nil, fmt.Errorf("the %s %s go.mod requires no module", entry.Kind, entry.Default.Version)
	}
	return modules, nil
}

func replacementOf(replaces []*modfile.Replace, path, version string) *modfile.Replace {
	var wildcard *modfile.Replace
	for _, replace := range replaces {
		switch {
		case replace.Old.Path != path:
		case replace.Old.Version == version:
			return replace
		case replace.Old.Version == "" && wildcard == nil:
			wildcard = replace
		}
	}
	return wildcard
}
