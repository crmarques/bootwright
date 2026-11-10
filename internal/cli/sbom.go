package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"strings"
)

type BillOfMaterials struct {
	Name       string
	Version    string
	Commit     string
	Source     string
	Components []BOMComponent
}

type BOMComponent struct {
	Ref          string
	Type         string
	Name         string
	Version      string
	Scope        string
	SHA256       string
	PURL         string
	Distribution string
	Properties   []BOMProperty
}

type BOMProperty struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type cycloneDocument struct {
	BOMFormat   string           `json:"bomFormat"`
	SpecVersion string           `json:"specVersion"`
	Version     int              `json:"version"`
	Metadata    cycloneMetadata  `json:"metadata"`
	Components  []cycloneElement `json:"components"`
}

type cycloneMetadata struct {
	Component cycloneElement `json:"component"`
}

type cycloneElement struct {
	Type       string        `json:"type"`
	Ref        string        `json:"bom-ref"`
	Name       string        `json:"name"`
	Version    string        `json:"version,omitempty"`
	Scope      string        `json:"scope,omitempty"`
	Hashes     []cycloneHash `json:"hashes,omitempty"`
	PURL       string        `json:"purl,omitempty"`
	References []cycloneLink `json:"externalReferences,omitempty"`
	Properties []BOMProperty `json:"properties,omitempty"`
}

type cycloneHash struct {
	Algorithm string `json:"alg"`
	Content   string `json:"content"`
}

type cycloneLink struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

func cycloneDX(bom BillOfMaterials) ([]byte, error) {
	if strings.TrimSpace(bom.Name) == "" || len(bom.Components) == 0 {
		return nil, errors.New("the bill of materials names no executable or no component")
	}
	document := cycloneDocument{BOMFormat: "CycloneDX", SpecVersion: "1.6", Version: 1, Components: []cycloneElement{}}
	document.Metadata.Component = cycloneElement{Type: "application", Ref: bom.Name, Name: bom.Name, Version: buildValue(bom.Version, "devel"),
		Properties: []BOMProperty{{Name: "bootwright:commit", Value: buildValue(bom.Commit, "unknown")}, {Name: "bootwright:source", Value: buildValue(bom.Source, "unknown")}}}
	seen := map[string]bool{}
	for _, component := range bom.Components {
		if component.Ref == "" || seen[component.Ref] {
			return nil, errors.New("the bill of materials names a component without a unique reference")
		}
		seen[component.Ref] = true
		element := cycloneElement{Type: component.Type, Ref: component.Ref, Name: component.Name, Version: component.Version, Scope: component.Scope, PURL: component.PURL, Properties: slices.Clone(component.Properties)}
		if component.SHA256 != "" {
			element.Hashes = []cycloneHash{{Algorithm: "SHA-256", Content: component.SHA256}}
		}
		if component.Distribution != "" {
			element.References = []cycloneLink{{Type: "distribution", URL: component.Distribution}}
		}
		slices.SortFunc(element.Properties, func(a, b BOMProperty) int { return strings.Compare(a.Name, b.Name) })
		document.Components = append(document.Components, element)
	}
	slices.SortFunc(document.Components, func(a, b cycloneElement) int { return strings.Compare(a.Ref, b.Ref) })
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(document); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
