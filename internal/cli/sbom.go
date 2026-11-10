package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"strings"
)

type BillOfMaterials struct {
	Name            string
	Version         string
	Commit          string
	Source          string
	Checked         string
	Components      []BOMComponent
	Vulnerabilities []BOMVulnerability
}

type BOMVulnerability struct {
	ID            string
	Aliases       []string
	Ref           string
	State         string
	Justification string
	Reason        string
	Expires       string
}

type BOMComponent struct {
	Ref          string
	Type         string
	Name         string
	Version      string
	Scope        string
	License      string
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
	BOMFormat       string                 `json:"bomFormat"`
	SpecVersion     string                 `json:"specVersion"`
	Version         int                    `json:"version"`
	Metadata        cycloneMetadata        `json:"metadata"`
	Components      []cycloneElement       `json:"components"`
	Vulnerabilities []cycloneVulnerability `json:"vulnerabilities,omitempty"`
}

type cycloneVulnerability struct {
	ID         string          `json:"id"`
	Analysis   cycloneAnalysis `json:"analysis"`
	Affects    []cycloneAffect `json:"affects"`
	Properties []BOMProperty   `json:"properties"`
}

type cycloneAnalysis struct {
	State         string   `json:"state"`
	Justification string   `json:"justification,omitempty"`
	Response      []string `json:"response,omitempty"`
	Detail        string   `json:"detail"`
}

type cycloneAffect struct {
	Ref string `json:"ref"`
}

type cycloneMetadata struct {
	Component cycloneElement `json:"component"`
}

type cycloneElement struct {
	Type       string           `json:"type"`
	Ref        string           `json:"bom-ref"`
	Name       string           `json:"name"`
	Version    string           `json:"version,omitempty"`
	Scope      string           `json:"scope,omitempty"`
	Hashes     []cycloneHash    `json:"hashes,omitempty"`
	Licenses   []cycloneLicense `json:"licenses,omitempty"`
	PURL       string           `json:"purl,omitempty"`
	References []cycloneLink    `json:"externalReferences,omitempty"`
	Properties []BOMProperty    `json:"properties,omitempty"`
}

type cycloneLicense struct {
	License    *cycloneLicenseID `json:"license,omitempty"`
	Expression string            `json:"expression,omitempty"`
}

type cycloneLicenseID struct {
	ID string `json:"id"`
}

func cycloneLicenses(license string) []cycloneLicense {
	switch {
	case license == "":
		return nil
	case strings.ContainsAny(license, " ()"):
		return []cycloneLicense{{Expression: license}}
	}
	return []cycloneLicense{{License: &cycloneLicenseID{ID: license}}}
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
		Properties: []BOMProperty{{Name: "bootwright:commit", Value: buildValue(bom.Commit, "unknown")}, {Name: "bootwright:source", Value: buildValue(bom.Source, "unknown")}, {Name: "bootwright:vulnerability-check", Value: vulnerabilityVerdict(bom.Checked)}}}
	seen := map[string]bool{}
	for _, component := range bom.Components {
		if component.Ref == "" || seen[component.Ref] {
			return nil, errors.New("the bill of materials names a component without a unique reference")
		}
		seen[component.Ref] = true
		element := cycloneElement{Type: component.Type, Ref: component.Ref, Name: component.Name, Version: component.Version, Scope: component.Scope, Licenses: cycloneLicenses(component.License), PURL: component.PURL, Properties: slices.Clone(component.Properties)}
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
	for _, vulnerability := range bom.Vulnerabilities {
		if !seen[vulnerability.Ref] || vulnerability.ID == "" {
			return nil, errors.New("the vulnerability exception " + vulnerability.ID + " names no component of the bill of materials")
		}
		analysis := cycloneAnalysis{State: "not_affected", Justification: vulnerability.Justification, Detail: vulnerability.Reason}
		if vulnerability.State == "no_fix" {
			analysis = cycloneAnalysis{State: "exploitable", Response: []string{"can_not_fix"}, Detail: vulnerability.Reason}
		}
		properties := []BOMProperty{{Name: "bootwright:expires", Value: vulnerability.Expires}}
		if len(vulnerability.Aliases) != 0 {
			properties = append([]BOMProperty{{Name: "bootwright:aliases", Value: strings.Join(vulnerability.Aliases, ",")}}, properties...)
		}
		document.Vulnerabilities = append(document.Vulnerabilities, cycloneVulnerability{ID: vulnerability.ID, Analysis: analysis, Affects: []cycloneAffect{{Ref: vulnerability.Ref}}, Properties: properties})
	}
	slices.SortFunc(document.Vulnerabilities, func(a, b cycloneVulnerability) int {
		return strings.Compare(a.ID+"\x00"+a.Affects[0].Ref, b.ID+"\x00"+b.Affects[0].Ref)
	})
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(document); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
