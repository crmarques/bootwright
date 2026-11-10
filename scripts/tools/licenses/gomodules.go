package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

const goTableFormat = "bootwright.go-licenses-v1"

const stdlibPath = "stdlib"

const maxLicenseBytes = 1 << 20

type moduleEntry struct {
	Path    string `json:"path"`
	Version string `json:"version,omitempty"`
	License string `json:"license"`
	File    string `json:"file"`
	Text    string `json:"text"`
	Notice  string `json:"notice,omitempty"`
}

type moduleTable struct {
	Format  string        `json:"format"`
	Modules []moduleEntry `json:"modules"`
}

type moduleSource struct{ path, version, dir string }

var (
	licenseFile = regexp.MustCompile(`(?i)^(licen[sc]e|copying)([-._].*)?$`)
	noticeFile  = regexp.MustCompile(`(?i)^notice([-._].*)?$`)
	platforms   = []string{"linux", "darwin", "windows"}
)

func listModules(root string) ([]moduleSource, error) {
	seen := map[string]moduleSource{}
	for _, goos := range platforms {
		command := exec.Command(filepath.Join(root, "scripts", "go"), "-C", root, "list", "-deps", "-test", "-f",
			"{{with .Module}}{{if not .Main}}{{.Path}}\t{{.Version}}\t{{.Dir}}\t{{if .Replace}}replaced{{end}}{{end}}{{end}}", "./...")
		command.Env = append(os.Environ(), "GOOS="+goos, "GOARCH=amd64", "CGO_ENABLED=0")
		var stderr bytes.Buffer
		command.Stderr = &stderr
		out, err := command.Output()
		if err != nil {
			return nil, fmt.Errorf("go list for %s: %v: %s", goos, err, strings.TrimSpace(stderr.String()))
		}
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			fields := strings.Split(line, "\t")
			if len(fields) != 4 || fields[0] == "" {
				continue
			}
			if fields[3] != "" || fields[1] == "" || fields[2] == "" {
				return nil, errors.New("the Go module " + fields[0] + " is a local replacement or has no module directory")
			}
			seen[fields[0]+"@"+fields[1]] = moduleSource{fields[0], fields[1], fields[2]}
		}
	}
	sources := make([]moduleSource, 0, len(seen))
	for _, key := range slices.Sorted(func(yield func(string) bool) {
		for key := range seen {
			if !yield(key) {
				return
			}
		}
	}) {
		sources = append(sources, seen[key])
	}
	return sources, nil
}

func readBounded(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(data) > maxLicenseBytes {
		return "", errors.New(path + " is larger than a license text may be")
	}
	return string(data), nil
}

func entryOf(path, version, dir string, references map[string]canonicalText) (moduleEntry, error) {
	names, err := os.ReadDir(dir)
	if err != nil {
		return moduleEntry{}, err
	}
	entry := moduleEntry{Path: path, Version: version}
	for _, name := range names {
		if !name.Type().IsRegular() {
			continue
		}
		switch {
		case licenseFile.MatchString(name.Name()):
			if entry.File != "" {
				return moduleEntry{}, fmt.Errorf("%s %s names two license files, %s and %s; review them and record the module in reviewedTexts", path, version, entry.File, name.Name())
			}
			if entry.Text, err = readBounded(filepath.Join(dir, name.Name())); err != nil {
				return moduleEntry{}, err
			}
			entry.File = name.Name()
		case noticeFile.MatchString(name.Name()):
			if entry.Notice, err = readBounded(filepath.Join(dir, name.Name())); err != nil {
				return moduleEntry{}, err
			}
		}
	}
	if entry.File == "" {
		return moduleEntry{}, fmt.Errorf("%s %s holds no license file", path, version)
	}
	if id, known := classify(entry.Text, references); known {
		entry.License = id
		return entry, nil
	}
	sum := sha256.Sum256([]byte(entry.Text))
	for _, reviewed := range reviewedTexts {
		if reviewed.sha256 == hex.EncodeToString(sum[:]) {
			entry.License = reviewed.license
			return entry, nil
		}
	}
	return moduleEntry{}, fmt.Errorf("%s %s: %s is no text this tool knows (MIT, BSD-2-Clause, BSD-3-Clause, ISC, Apache-2.0, MPL-2.0); its SHA-256 is %s, so review it and add it to reviewedTexts", path, version, entry.File, hex.EncodeToString(sum[:]))
}

func composeTable(root string) ([]byte, error) {
	references, err := loadReferences()
	if err != nil {
		return nil, err
	}
	sources, err := listModules(root)
	if err != nil {
		return nil, err
	}
	goroot, err := exec.Command(filepath.Join(root, "scripts", "go"), "-C", root, "env", "GOROOT").Output()
	if err != nil {
		return nil, err
	}
	table := moduleTable{Format: goTableFormat}
	for _, source := range append([]moduleSource{{stdlibPath, "", strings.TrimSpace(string(goroot))}}, sources...) {
		entry, err := entryOf(source.path, source.version, source.dir, references)
		if err != nil {
			return nil, err
		}
		table.Modules = append(table.Modules, entry)
	}
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(table); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func readTable(path string) (moduleTable, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return moduleTable{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var table moduleTable
	if err := decoder.Decode(&table); err != nil || table.Format != goTableFormat {
		return moduleTable{}, fmt.Errorf("%s is no %s document: %v", path, goTableFormat, err)
	}
	return table, nil
}
