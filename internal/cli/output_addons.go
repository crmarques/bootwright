package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/crmarques/bootwright/internal/addons/catalog"
)

type addOnsPresentation struct {
	AddOns []addOnPresentation `json:"addOns"`
}
type addOnPresentation struct {
	Name          string   `json:"name"`
	Provides      []string `json:"provides"`
	Requires      []string `json:"requires"`
	OLMPackage    string   `json:"olmPackage"`
	DefaultSource string   `json:"defaultSource"`
	Configuration []string `json:"configuration"`
}

func (addOnsPresentation) documentedResult() {}
func writeAddOns(out io.Writer, command string, result *catalog.ListResult, structured bool) error {
	shown := addOnsPresentation{AddOns: []addOnPresentation{}}
	for _, entry := range result.AddOns {
		shown.AddOns = append(shown.AddOns, addOnPresentation{Name: escapeDisplayLine(entry.Name), Provides: displayNames(entry.Provides), Requires: displayNames(entry.Requires), OLMPackage: escapeDisplayLine(entry.OLMPackage), DefaultSource: escapeDisplayLine(entry.DefaultSource), Configuration: displayNames(entry.Configuration)})
	}
	if structured {
		return writeStructuredResult(out, command, shown, []diagnostic{}, []string{}, 0)
	}
	var text strings.Builder
	text.WriteString("NAME  PROVIDES  REQUIRES  OLM PACKAGE  DEFAULT SOURCE  CONFIGURATION\n")
	for _, entry := range shown.AddOns {
		fmt.Fprintf(&text, "%s  %s  %s  %s  %s  %s\n", entry.Name, joinedOrNone(entry.Provides), joinedOrNone(entry.Requires), textOrNone(entry.OLMPackage), textOrNone(entry.DefaultSource), joinedOrNone(entry.Configuration))
	}
	_, err := io.WriteString(out, text.String())
	return err
}
func joinedOrNone(values []string) string { return textOrNone(strings.Join(values, ",")) }
func textOrNone(value string) string {
	if value == "" {
		return "none"
	}
	return value
}
