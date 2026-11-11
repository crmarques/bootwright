package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

const (
	maxWheelLicenseFiles = 64
	maxWheelNoticeBytes  = 4 << 20
)

type sbomComponent struct {
	Ref      string `json:"bom-ref"`
	Name     string `json:"name"`
	Version  string `json:"version"`
	Scope    string `json:"scope"`
	PURL     string `json:"purl"`
	Licenses []struct {
		Expression string `json:"expression"`
		License    struct {
			ID string `json:"id"`
		} `json:"license"`
	} `json:"licenses"`
}

type sbomDocument struct {
	Metadata struct {
		Component struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"component"`
	} `json:"metadata"`
	Components []sbomComponent `json:"components"`
}

type noticeSection struct {
	name, version, license, source string
	wheels                         []string
	text                           string
}

var topLevelLicense = regexp.MustCompile(`(?i)^(licen[sc]e|copying|notice)([-._].*)?$`)

func (c sbomComponent) license() string {
	var parts []string
	for _, entry := range c.Licenses {
		parts = append(parts, entry.Expression+entry.License.ID)
	}
	return strings.Join(parts, " AND ")
}

func wheelTexts(wheel string) ([]string, []string, error) {
	archive, err := zip.OpenReader(wheel)
	if err != nil {
		return nil, nil, err
	}
	defer archive.Close()
	var names, texts []string
	total := 0
	collect := func(match func(directory, name string) bool) error {
		for _, member := range archive.File {
			directory, name := path.Split(member.Name)
			if member.FileInfo().IsDir() || !match(strings.TrimSuffix(directory, "/"), name) {
				continue
			}
			if len(names) == maxWheelLicenseFiles || member.UncompressedSize64 > maxLicenseBytes {
				return errors.New(wheel + " holds more or larger license files than a notice may reproduce")
			}
			reader, err := member.Open()
			if err != nil {
				return err
			}
			data, err := io.ReadAll(io.LimitReader(reader, maxLicenseBytes+1))
			reader.Close()
			if err != nil {
				return err
			}
			if total += len(data); total > maxWheelNoticeBytes {
				return errors.New(wheel + " holds more license text than a notice may reproduce")
			}
			names, texts = append(names, member.Name), append(texts, string(data))
		}
		return nil
	}
	inLicenses := func(directory, _ string) bool {
		parts := strings.Split(directory, "/")
		return len(parts) >= 2 && strings.HasSuffix(parts[0], ".dist-info") && parts[1] == "licenses"
	}
	if err := collect(inLicenses); err != nil {
		return nil, nil, err
	}
	if len(names) == 0 {
		err = collect(func(directory, name string) bool {
			return strings.HasSuffix(directory, ".dist-info") && !strings.Contains(directory, "/") && topLevelLicense.MatchString(name)
		})
		if err != nil {
			return nil, nil, err
		}
	}
	if len(names) == 0 {
		return nil, nil, errors.New(wheel + " carries no license file under its dist-info")
	}
	return names, texts, nil
}

func buildNotices(sbom []byte, table moduleTable, wheels string) ([]byte, error) {
	var document sbomDocument
	if err := json.Unmarshal(sbom, &document); err != nil || document.Metadata.Component.Name == "" {
		return nil, errors.New("the bill of materials is no CycloneDX document of an executable")
	}
	modules := map[string]moduleEntry{}
	for _, entry := range table.Modules {
		modules[entry.Path+"@"+entry.Version] = entry
	}
	sections := map[string]*noticeSection{}
	for _, component := range document.Components {
		if component.Scope != "required" {
			continue
		}
		kind, _, _ := strings.Cut(component.Ref, ":")
		if kind == "ansible" {
			continue
		}
		if component.license() == "" {
			return nil, fmt.Errorf("the embedded component %s names no license", component.Ref)
		}
		switch kind {
		case "go":
			version := component.Version
			if component.Name == stdlibPath {
				version = ""
			}
			entry, found := modules[component.Name+"@"+version]
			if !found || entry.Text == "" {
				return nil, fmt.Errorf("the license table holds no text for %s; run make licenses", component.Ref)
			}
			if entry.License != component.license() {
				return nil, fmt.Errorf("%s is licensed %s in the bill of materials and %s in the license table", component.Ref, component.license(), entry.License)
			}
			text := entry.Text
			if entry.Notice != "" {
				text += "\n" + entry.Notice
			}
			sections[component.Ref] = &noticeSection{name: component.Name, version: component.Version, license: component.license(), source: component.PURL, text: text}
		case "pypi":
			key := "pypi:" + component.Name + "@" + component.Version
			filename := strings.TrimPrefix(component.Ref, "pypi:")
			if section, seen := sections[key]; seen {
				section.wheels = append(section.wheels, filename)
				continue
			}
			if filename != filepath.Base(filename) || filename == "." || filename == ".." {
				return nil, fmt.Errorf("the embedded component %s names a wheel with a path", component.Ref)
			}
			names, texts, err := wheelTexts(filepath.Join(wheels, filename))
			if err != nil {
				return nil, fmt.Errorf("the embedded wheel %s: %v", filename, err)
			}
			var text strings.Builder
			for index, name := range names {
				if index > 0 {
					text.WriteString("\n")
				}
				fmt.Fprintf(&text, "--- %s ---\n\n%s", name, strings.TrimRight(texts[index], "\n")+"\n")
			}
			sections[key] = &noticeSection{name: component.Name, version: component.Version, license: component.license(), source: component.PURL, wheels: []string{filename}, text: text.String()}
		default:
			return nil, fmt.Errorf("the embedded component %s is of a kind this tool has no license source for", component.Ref)
		}
	}
	if len(sections) == 0 {
		return nil, errors.New("the bill of materials names no embedded third-party component")
	}
	var out bytes.Buffer
	fmt.Fprintf(&out, "Third-party notices of %s %s\n\nThe executable embeds the components below, each under the license named here. The\nlicense texts follow as their authors published them.\n\nBill of materials: sha256:%x\n", document.Metadata.Component.Name, document.Metadata.Component.Version, sha256.Sum256(sbom))
	for _, key := range slices.Sorted(func(yield func(string) bool) {
		for key := range sections {
			if !yield(key) {
				return
			}
		}
	}) {
		section := sections[key]
		fmt.Fprintf(&out, "\n%s\n%s %s\nLicense: %s\n", strings.Repeat("=", 78), section.name, section.version, section.license)
		if section.source != "" {
			fmt.Fprintf(&out, "Package: %s\n", section.source)
		}
		if len(section.wheels) != 0 {
			fmt.Fprintf(&out, "Wheels: %s\n", strings.Join(section.wheels, ", "))
		}
		fmt.Fprintf(&out, "\n%s", strings.TrimRight(section.text, "\n"))
		out.WriteString("\n")
	}
	return out.Bytes(), nil
}

func writeNotices(sbomPath, tablePath, wheels, output string) error {
	sbom, err := os.ReadFile(sbomPath)
	if err != nil {
		return err
	}
	table, err := readTable(tablePath)
	if err != nil {
		return err
	}
	notices, err := buildNotices(sbom, table, wheels)
	if err != nil {
		return err
	}
	return os.WriteFile(output, notices, 0o644)
}
