package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/crmarques/bootwright/internal/addons/catalog"
)

const addOnRequiresMeaning = "another instance selected for the same cluster must provide each required capability"

type addOnInfoPresentation struct {
	Name            string                `json:"name"`
	Summary         string                `json:"summary"`
	Provides        []string              `json:"provides"`
	Requires        []string              `json:"requires"`
	RequiresMeaning string                `json:"requiresMeaning"`
	OLMPackage      string                `json:"olmPackage"`
	DefaultSource   string                `json:"defaultSource"`
	Namespaces      []string              `json:"secretNamespaces"`
	Lookup          string                `json:"lookup"`
	Configuration   []addOnFieldPresented `json:"configuration"`
	Binding         string                `json:"binding"`
}
type addOnFieldPresented struct {
	Path           string   `json:"path"`
	Type           string   `json:"type"`
	Required       bool     `json:"required"`
	RequiredWithin string   `json:"requiredWithin"`
	Default        string   `json:"default"`
	Values         []string `json:"values"`
}
type addOnInfoDocument struct {
	Package addOnInfoPresentation `json:"package"`
}

func (addOnInfoDocument) documentedResult() {}

func writeAddOnInfo(out io.Writer, command string, result *catalog.DescribeResult, structured bool) error {
	p := result.Package
	shown := addOnInfoPresentation{Name: escapeDisplayLine(p.Name), Summary: escapeDisplayLine(p.Summary), Provides: displayNames(p.Provides), Requires: displayNames(p.Requires), RequiresMeaning: addOnRequiresMeaning,
		OLMPackage: escapeDisplayLine(p.OLMPackage), DefaultSource: escapeDisplayLine(p.DefaultSource), Namespaces: displayNames(p.Namespaces), Lookup: escapeDisplayLine(p.Lookup), Configuration: []addOnFieldPresented{}, Binding: p.Binding}
	for _, field := range p.Configuration {
		shown.Configuration = append(shown.Configuration, addOnFieldPresented{Path: escapeDisplayLine(field.Path), Type: escapeDisplayLine(field.Type), Required: field.Required, RequiredWithin: escapeDisplayLine(field.RequiredWithin), Default: escapeDisplayLine(field.Default), Values: displayNames(field.Values)})
	}
	if structured {
		return writeStructuredResult(out, command, addOnInfoDocument{Package: shown}, []diagnostic{}, []string{}, 0)
	}
	var text strings.Builder
	fmt.Fprintf(&text, "%s\n%s\n\n", shown.Name, shown.Summary)
	fmt.Fprintf(&text, "Provides: %s\nRequires: %s\nRequires means: %s\n", joinedOrNone(shown.Provides), joinedOrNone(shown.Requires), shown.RequiresMeaning)
	fmt.Fprintf(&text, "OLM package: %s\nDefault source: %s\nSecret namespaces: %s\nChannel and CSV lookup: %s\n\n", textOrNone(shown.OLMPackage), textOrNone(shown.DefaultSource), joinedOrNone(shown.Namespaces), textOrNone(shown.Lookup))
	rows := [][4]string{{"FIELD", "TYPE", "REQUIRED", "DEFAULT"}}
	widths := [3]int{}
	for _, field := range shown.Configuration {
		required := "optional"
		switch {
		case field.Required:
			required = "required"
		case field.RequiredWithin != "":
			required = "required in " + field.RequiredWithin
		}
		kind := field.Type
		if len(field.Values) > 0 {
			kind += " (" + strings.Join(field.Values, "|") + ")"
		}
		rows = append(rows, [4]string{field.Path, kind, required, textOrNone(field.Default)})
	}
	for _, row := range rows {
		for i := range widths {
			widths[i] = max(widths[i], len(row[i]))
		}
	}
	fmt.Fprintf(&text, "Configuration under spec.packages.%s:\n", shown.Name)
	for _, row := range rows {
		fmt.Fprintf(&text, "  %-*s  %-*s  %-*s  %s\n", widths[0], row[0], widths[1], row[1], widths[2], row[2], row[3])
	}
	fmt.Fprintf(&text, "\nBinding skeleton (replace each <field>):\n%s", shown.Binding)
	_, err := io.WriteString(out, text.String())
	return err
}

func writeAddOnResult(out io.Writer, path string, result commandResult, structured bool) (bool, error) {
	switch {
	case path == "add-ons list" && result.addOns != nil:
		return true, writeAddOns(out, path, result.addOns, structured)
	case path == "add-ons info" && result.addOnInfo != nil:
		return true, writeAddOnInfo(out, path, result.addOnInfo, structured)
	}
	return false, nil
}

func groupShort(part string) string {
	if part == "add-ons" {
		return "Inspect built-in add-on packages"
	}
	return "Manage " + part
}
