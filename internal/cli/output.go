package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
)

type diagnostic = desiredstate.Diagnostic

type commandEnvelope struct {
	SchemaVersion string       `json:"schemaVersion"`
	Command       string       `json:"command"`
	OK            bool         `json:"ok"`
	ExitCode      int          `json:"exitCode"`
	Result        any          `json:"result"`
	Diagnostics   []diagnostic `json:"diagnostics"`
	Logs          []string     `json:"logs"`
}

func writeFailure(out, errOut io.Writer, command, code, message string, exitCode int, jsonMode bool) error {
	return writeDiagnostics(out, errOut, command, []diagnostic{{Severity: "error", Code: code, Message: message}}, exitCode, jsonMode)
}

func writeDiagnostics(out, errOut io.Writer, command string, diagnostics []diagnostic, exitCode int, jsonMode bool) error {
	diagnostics = displayDiagnostics(diagnostics)
	if jsonMode {
		encoder := json.NewEncoder(out)
		encoder.SetEscapeHTML(false)
		return encoder.Encode(commandEnvelope{
			SchemaVersion: "v1alpha1",
			Command:       escapeDisplayLine(command),
			ExitCode:      exitCode,
			Diagnostics:   diagnostics,
			Logs:          []string{},
		})
	}
	return writeHumanDiagnostics(errOut, diagnostics)
}

func writeValidation(out, errOut io.Writer, command string, report *compilation.Report, jsonMode bool) error {
	diagnostics := displayDiagnostics(report.Diagnostics)
	advisories := []diagnostic{}
	for _, d := range diagnostics {
		if d.Code == "api.deferred" && d.Severity == "warning" {
			advisories = append(advisories, d)
		}
	}
	result := compilation.Report{
		Counts:                    report.Counts,
		ExcludedContainerClusters: displayNames(report.ExcludedContainerClusters),
		ExcludedStorageClusters:   displayNames(report.ExcludedStorageClusters),
		ExcludedResourceFiles:     displayNames(report.ExcludedResourceFiles),
		Advisories:                advisories,
	}
	if jsonMode {
		encoder := json.NewEncoder(out)
		encoder.SetEscapeHTML(false)
		return encoder.Encode(commandEnvelope{SchemaVersion: "v1alpha1", Command: escapeDisplayLine(command), OK: true, ExitCode: 0, Result: result, Diagnostics: diagnostics, Logs: []string{}})
	}
	if err := writeHumanDiagnostics(errOut, diagnostics); err != nil {
		return err
	}
	_, err := fmt.Fprintf(out, "[OK] Desired state is valid (files seen: %d, objects decoded: %d)\n", report.Counts.FilesSeen, report.Counts.ObjectsDecoded)
	return err
}

func displayNames(values []string) []string {
	values = append([]string{}, values...)
	slices.Sort(values)
	values = slices.Compact(values)
	for i := range values {
		values[i] = escapeDisplayLine(values[i])
	}
	return values
}

func displayDiagnostics(values []diagnostic) []diagnostic {
	values = append([]diagnostic{}, values...)
	desiredstate.SortDiagnostics(values)
	for i, d := range values {
		d.Severity = escapeDisplayLine(d.Severity)
		d.Code = escapeDisplayLine(d.Code)
		d.Message = escapeDisplayLine(d.Message)
		d.Field = escapeDisplayLine(d.Field)
		d.Remediation = escapeDisplayLine(d.Remediation)
		if d.Source != nil {
			source := *d.Source
			source.Path = escapeDisplayLine(source.Path)
			d.Source = &source
		}
		if d.Object != nil {
			object := *d.Object
			object.APIVersion = escapeDisplayLine(object.APIVersion)
			object.Kind = escapeDisplayLine(object.Kind)
			object.Name = escapeDisplayLine(object.Name)
			d.Object = &object
		}
		values[i] = d
	}
	return values
}

// Values reaching this function have already crossed the display boundary.
func writeHumanDiagnostics(out io.Writer, diagnostics []diagnostic) error {
	for _, d := range diagnostics {
		status := "FAIL"
		if d.Severity == "warning" {
			status = "WARN"
		}
		location := ""
		if d.Source != nil && d.Source.Path != "" {
			location = " " + d.Source.Path
			if d.Source.Line > 0 {
				location += fmt.Sprintf(":%d", d.Source.Line)
				if d.Source.Column > 0 {
					location += fmt.Sprintf(":%d", d.Source.Column)
				}
			}
		}
		message := d.Message
		if d.Object != nil {
			message += " [" + d.Object.Kind + "/" + d.Object.Name + "]"
		}
		if d.Field != "" {
			message += " (" + d.Field + ")"
		}
		if d.Remediation != "" {
			message += "; next: " + d.Remediation
		}
		if _, err := fmt.Fprintf(out, "[%s] %s%s: %s\n", status, d.Code, location, message); err != nil {
			return err
		}
	}
	return nil
}

func escapeDisplayLine(value string) string {
	var escaped strings.Builder
	for len(value) > 0 {
		r, size := utf8.DecodeRuneInString(value)
		if r == utf8.RuneError && size == 1 {
			fmt.Fprintf(&escaped, "\\x%02x", value[0])
			value = value[1:]
			continue
		}
		value = value[size:]
		switch r {
		case '\\':
			escaped.WriteString(`\\`)
		case '\n':
			escaped.WriteString(`\n`)
		case '\r':
			escaped.WriteString(`\r`)
		case '\t':
			escaped.WriteString(`\t`)
		default:
			if unicode.IsPrint(r) {
				escaped.WriteRune(r)
			} else if r <= 0xffff {
				fmt.Fprintf(&escaped, "\\u%04x", r)
			} else {
				highSurrogate, lowSurrogate := utf16.EncodeRune(r)
				fmt.Fprintf(&escaped, "\\u%04x\\u%04x", highSurrogate, lowSurrogate)
			}
		}
	}
	return escaped.String()
}
