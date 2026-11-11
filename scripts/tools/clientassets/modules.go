package clientassets

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sync"
)

const ModulesFormat = "bootwright.client-build-modules-v1"

const maxModulesBytes = 1 << 20

var ErrUnpinned = errors.New("no go.mod is pinned for that release")

// Module is the publisher's go.mod at a client release's tag, which names the
// modules the release was built from, with the size and SHA-256 that pin it.
type Module struct {
	Kind, Version, URL string
	Bytes              int64
	SHA256             string
}

type modulesDocument struct {
	Format   string `json:"format"`
	Releases []struct {
		Kind    string `json:"kind"`
		Version string `json:"version"`
		Bytes   int64  `json:"bytes"`
		SHA256  string `json:"sha256"`
	} `json:"releases"`
}

//go:embed buildmodules.json
var embeddedModules []byte

var (
	sha256Pattern  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	versionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	pinned         = sync.OnceValues(func() ([]Module, error) { return ParseModules(embeddedModules) })
)

func moduleURL(kind, version string) (string, bool) {
	if kind == "virtctl" {
		return "https://raw.githubusercontent.com/kubevirt/kubevirt/v" + version + "/go.mod", true
	}
	return "", false
}

// ParseModules reads one strict document of pinned go.mod files.
func ParseModules(data []byte) ([]Module, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var document modulesDocument
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("the build modules file is not one strict JSON document: %w", err)
	}
	if len(bytes.TrimSpace(data[decoder.InputOffset():])) != 0 {
		return nil, errors.New("the build modules file holds more than one JSON document")
	}
	if document.Format != ModulesFormat {
		return nil, fmt.Errorf("the build modules file's format is %q, not %q", document.Format, ModulesFormat)
	}
	var modules []Module
	seen := map[string]bool{}
	for _, release := range document.Releases {
		url, known := moduleURL(release.Kind, release.Version)
		switch {
		case !known:
			return nil, fmt.Errorf("the build modules file pins %s, whose publisher go.mod this gate does not know", release.Kind)
		case !versionPattern.MatchString(release.Version):
			return nil, fmt.Errorf("the build modules file names %s release %q, which is no MAJOR.MINOR.PATCH", release.Kind, release.Version)
		case !sha256Pattern.MatchString(release.SHA256) || release.Bytes <= 0 || release.Bytes > maxModulesBytes:
			return nil, fmt.Errorf("the build modules file's %s %s names no lowercase SHA-256 or a size outside its bound", release.Kind, release.Version)
		case seen[release.Kind+"@"+release.Version]:
			return nil, fmt.Errorf("the build modules file pins %s %s twice", release.Kind, release.Version)
		}
		seen[release.Kind+"@"+release.Version] = true
		modules = append(modules, Module{Kind: release.Kind, Version: release.Version, URL: url, Bytes: release.Bytes, SHA256: release.SHA256})
	}
	return modules, nil
}

// Modules returns every pinned go.mod.
func Modules() ([]Module, error) { return pinned() }

// ModulesOf returns the go.mod pinned for a client's release, or an error
// wrapping ErrUnpinned.
func ModulesOf(kind, version string) (Module, error) {
	all, err := pinned()
	if err != nil {
		return Module{}, err
	}
	for _, module := range all {
		if module.Kind == kind && module.Version == version {
			return module, nil
		}
	}
	return Module{}, fmt.Errorf("%w: pin the SHA-256 of its release tag's go.mod in scripts/tools/clientassets/buildmodules.json, then run make tool-table: %s %s", ErrUnpinned, kind, version)
}
