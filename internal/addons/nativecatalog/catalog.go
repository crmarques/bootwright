package nativecatalog

import (
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"

	catalog "github.com/crmarques/bootwright/add-ons"
	"go.yaml.in/yaml/v3"
)

type Entry struct {
	Name           string         `yaml:"name"`
	Description    string         `yaml:"description"`
	DefaultVersion string         `yaml:"defaultVersion"`
	Versions       []EntryVersion `yaml:"versions"`
}

type EntryVersion struct {
	Version string `yaml:"version"`
	Channel string `yaml:"channel,omitempty"`
	Notes   string `yaml:"notes,omitempty"`
}

type Release struct {
	Entry   Entry
	Version string
}

var versionTokenRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func Entries() ([]Entry, error) {
	raw, err := catalog.Content.ReadFile("catalog.yaml")
	if err != nil {
		return nil, fmt.Errorf("read embedded add-on catalog index: %w", err)
	}
	var index struct {
		Entries []Entry `yaml:"entries"`
	}
	decoder := yaml.NewDecoder(strings.NewReader(string(raw)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&index); err != nil {
		return nil, fmt.Errorf("decode embedded add-on catalog index: %w", err)
	}
	seen := map[string]bool{}
	for _, entry := range index.Entries {
		if entry.Name == "" || seen[entry.Name] {
			return nil, fmt.Errorf("catalog entry name %q is empty or duplicated", entry.Name)
		}
		seen[entry.Name] = true
		if len(entry.Versions) == 0 {
			return nil, fmt.Errorf("catalog entry %s declares no versions", entry.Name)
		}
		versions := map[string]bool{}
		for _, version := range entry.Versions {
			if !versionTokenRe.MatchString(version.Version) {
				return nil, fmt.Errorf("catalog entry %s version %q is not a safe path segment", entry.Name, version.Version)
			}
			if versions[version.Version] {
				return nil, fmt.Errorf("catalog entry %s version %q is duplicated", entry.Name, version.Version)
			}
			versions[version.Version] = true
			if _, err := fs.Stat(catalog.Content, entry.Name+"/"+version.Version+"/add-on.yaml"); err != nil {
				return nil, fmt.Errorf("catalog entry %s version %s has no embedded add-on.yaml: %w", entry.Name, version.Version, err)
			}
		}
		if !versions[entry.DefaultVersion] {
			return nil, fmt.Errorf("catalog entry %s defaultVersion %q is not a declared version", entry.Name, entry.DefaultVersion)
		}
	}
	return index.Entries, nil
}

func Resolve(name, version string) (Release, error) {
	entries, err := Entries()
	if err != nil {
		return Release{}, err
	}
	for _, entry := range entries {
		if entry.Name != name {
			continue
		}
		if version == "" {
			version = entry.DefaultVersion
		}
		for _, candidate := range entry.Versions {
			if candidate.Version == version {
				return Release{Entry: entry, Version: version}, nil
			}
		}
		return Release{}, fmt.Errorf("add-on %q has no version %q in the catalog; run bootwright add-ons list", name, version)
	}
	return Release{}, fmt.Errorf("add-on %q not found in the catalog; run bootwright add-ons list", name)
}

func ParseNameVersion(value string) (name, version string) {
	if idx := strings.Index(value, ":"); idx >= 0 {
		return value[:idx], value[idx+1:]
	}
	return value, ""
}

type File struct {
	Path string
	Data []byte
}

func Files(release Release) ([]File, error) {
	root := release.Entry.Name + "/" + release.Version
	var out []File
	err := fs.WalkDir(catalog.Content, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := catalog.Content.ReadFile(path)
		if err != nil {
			return err
		}
		out = append(out, File{Path: strings.TrimPrefix(path, root+"/"), Data: data})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read embedded add-on %s %s: %w", release.Entry.Name, release.Version, err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}
