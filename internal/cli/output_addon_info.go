package cli

import (
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
	var text display
	text.headline("", p.Name)
	text.lines([]string{p.Summary})
	text.section("")
	text.fields(
		field{Label: "Provides", Value: joinedOrNone(p.Provides)},
		field{Label: "Requires", Value: joinedOrNone(p.Requires), Note: "  (" + addOnRequiresMeaning + ")"},
		field{Label: "OLM package", Value: textOrNone(p.OLMPackage)},
		field{Label: "Default source", Value: textOrNone(p.DefaultSource)},
		field{Label: "Secret namespaces", Value: joinedOrNone(p.Namespaces)},
	)
	text.section("Channel and CSV lookup")
	text.commands([]string{textOrNone(p.Lookup)})
	rows := make([][]string, 0, len(p.Configuration))
	for _, field := range p.Configuration {
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
		rows = append(rows, []string{field.Path, kind, required, textOrNone(field.Default)})
	}
	text.section("Configuration under spec.packages." + p.Name)
	text.resourceTable([]string{"FIELD", "TYPE", "REQUIRED", "DEFAULT"}, rows, tableStyle{Kind: "field", Identity: 0})
	text.section("Binding skeleton (replace each <field>)")
	if err := text.writeTo(out); err != nil {
		return err
	}
	_, err := io.WriteString(out, p.Binding)
	return err
}

func groupShort(part string) string {
	return map[string]string{
		"add-ons":    "Inspect add-on packages supported by this build",
		"cluster":    "Inspect and reach a context's clusters",
		"policy":     "Manage delegated session access",
		"completion": "Generate shell completion scripts",
		"context":    "Create, select, update and delete contexts",
		"controller": "Install, rebind or remove this host's controller",
		"catalog":    "Browse installer media, built-in add-ons and the desired-state schema",
		"check":      "Check input, this controller or an environment's prerequisites",
		"plan":       "Preview an apply or a destroy without executing it",
		"encryption": "Initialize, inspect and rotate the Secret store's key",
		"machine":    "Inspect, reach, trust and power a context's Machines",
		"media":      "Manage installer media shared by contexts on this controller",
		"render":     "Render effective desired state and external-tool artifacts",
		"secret":     "Set, generate, check and reveal a context's Secrets",
	}[part]
}
