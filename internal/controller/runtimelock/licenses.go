package runtimelock

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
)

const LicensesFormat = "bootwright.controller.runtime-licenses-v1"

//go:embed licenses.json
var embeddedLicenses []byte

var SPDXExpression = regexp.MustCompile(`^[A-Za-z0-9.+-]+( (AND|OR|WITH) [A-Za-z0-9.+-]+)*$`)

type Distribution struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	License string `json:"license"`
	Source  string `json:"source"`
}

type licensesDocument struct {
	Format        string         `json:"format"`
	Distributions []Distribution `json:"distributions"`
}

func ParseLicenses(data []byte) (map[string]Distribution, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var document licensesDocument
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("the runtime license table is not one strict JSON document: %w", err)
	}
	if len(bytes.TrimSpace(data[decoder.InputOffset():])) != 0 {
		return nil, errors.New("the runtime license table holds more than one JSON document")
	}
	if document.Format != LicensesFormat {
		return nil, fmt.Errorf("the runtime license table's format is %q, not %q", document.Format, LicensesFormat)
	}
	table := map[string]Distribution{}
	for _, entry := range document.Distributions {
		key := entry.Name + "@" + entry.Version
		if _, repeated := table[key]; repeated || !namePattern.MatchString(entry.Name) || !versionPattern.MatchString(entry.Version) ||
			!SPDXExpression.MatchString(entry.License) || entry.Source == "" {
			return nil, fmt.Errorf("the runtime license table's entry for %s is repeated or incomplete", key)
		}
		table[key] = entry
	}
	return table, nil
}

func Licenses() (map[string]Distribution, error) { return ParseLicenses(embeddedLicenses) }

func LicenseOf(file File) (string, bool) {
	table, err := Licenses()
	entry, found := table[file.Name+"@"+file.Version]
	return entry.License, err == nil && found
}
