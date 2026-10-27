package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"go.yaml.in/yaml/v3"
)

type diagnostic = diagnostics.Diagnostic

type commandEnvelope struct {
	SchemaVersion string       `json:"schemaVersion"`
	Command       string       `json:"command"`
	OK            bool         `json:"ok"`
	ExitCode      int          `json:"exitCode"`
	Result        jsonResult   `json:"result"`
	Diagnostics   []diagnostic `json:"diagnostics"`
	Logs          []string     `json:"logs"`
}

func writeEnvelope(out io.Writer, envelope commandEnvelope) error {
	if presentationFor(out).Format != "yaml" {
		encoder := json.NewEncoder(out)
		encoder.SetEscapeHTML(false)
		return encoder.Encode(envelope)
	}
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(envelope); err != nil {
		return err
	}
	var document yaml.Node
	if err := yaml.Unmarshal(yamlSafeJSON(encoded.Bytes()), &document); err != nil {
		return err
	}
	var blockStyle func(*yaml.Node)
	blockStyle = func(node *yaml.Node) {
		node.Style = 0
		for i, child := range node.Content {
			blockStyle(child)
			if child.Kind != yaml.ScalarNode || child.Tag != "!!str" {
				continue
			}
			if node.Kind == yaml.SequenceNode || node.Kind == yaml.MappingNode && (i%2 == 1 || !plainYAMLKey(child.Value)) {
				child.Style = yaml.DoubleQuotedStyle
			}
		}
	}
	blockStyle(&document)
	encoded.Reset()
	yamlEncoder := yaml.NewEncoder(&encoded)
	yamlEncoder.SetIndent(2)
	if err := yamlEncoder.Encode(&document); err != nil {
		return err
	}
	if err := yamlEncoder.Close(); err != nil {
		return err
	}
	n, err := out.Write(encoded.Bytes())
	if err == nil && n != encoded.Len() {
		err = io.ErrShortWrite
	}
	return err
}

func yamlSafeJSON(encoded []byte) []byte {
	var safe bytes.Buffer
	for len(encoded) > 0 {
		r, size := utf8.DecodeRune(encoded)
		if r == 0x7f || r >= 0x80 && r <= 0x9f || r == 0xfffe || r == 0xffff {
			fmt.Fprintf(&safe, "\\u%04x", r)
		} else {
			safe.Write(encoded[:size])
		}
		encoded = encoded[size:]
	}
	return safe.Bytes()
}

func plainYAMLKey(key string) bool {
	if key == "" || !(key[0] >= 'a' && key[0] <= 'z' || key[0] >= 'A' && key[0] <= 'Z') {
		return false
	}
	for _, r := range key {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_.-/", r)) {
			return false
		}
	}
	switch strings.ToLower(key) {
	case "y", "n", "yes", "no", "true", "false", "on", "off", "null":
		return false
	}
	return true
}

// jsonResult is a command's documented structured result. Only the result types this
// package declares implement it, so a domain value, whose field names and
// shape belong to its own package, can never be encoded as a result.
type jsonResult interface{ documentedResult() }

// admissionCounts are the admission counts a result reports.
type admissionCounts struct {
	FilesSeen      int `json:"filesSeen"`
	ObjectsDecoded int `json:"objectsDecoded"`
}

// resultContext is the context a result was read from.
type resultContext struct {
	Name string `json:"name"`
	Mode string `json:"mode"`
}

type validationResult struct {
	Counts                    admissionCounts `json:"counts"`
	ExcludedContainerClusters []string        `json:"excludedContainerClusters"`
	ExcludedStorageClusters   []string        `json:"excludedStorageClusters"`
	Advisories                []diagnostic    `json:"advisories"`
}

func (validationResult) documentedResult() {}

func displayCounts(counts compilation.Counts) admissionCounts {
	return admissionCounts{FilesSeen: counts.FilesSeen, ObjectsDecoded: counts.ObjectsDecoded}
}

func writeFailure(out, errOut io.Writer, command, code, message string, exitCode int, jsonMode bool, logs []string) error {
	return writeDiagnostics(out, errOut, command, []diagnostic{{Severity: "error", Code: code, Message: message}}, exitCode, jsonMode, logs)
}

// writeDiagnostics reports a failure. Its JSON envelope lists the private logs
// the invocation created before it failed, because a JSON invocation reports
// no progress that could name them; text named them while the work ran.
func writeDiagnostics(out, errOut io.Writer, command string, diagnostics []diagnostic, exitCode int, jsonMode bool, logs []string) error {
	diagnostics = displayDiagnostics(diagnostics)
	if jsonMode {
		return writeEnvelope(out, commandEnvelope{
			SchemaVersion: "v1alpha1",
			Command:       escapeDisplayLine(command),
			ExitCode:      exitCode,
			Diagnostics:   diagnostics,
			Logs:          displayLines(logs),
		})
	}
	return writeHumanDiagnostics(errOut, diagnostics)
}

func writeValidation(out, errOut io.Writer, command string, report *compilation.Report, jsonMode bool) error {
	diagnostics := displayDiagnostics(report.Diagnostics)
	advisories := []diagnostic{}
	for _, d := range diagnostics {
		if (d.Code == "api.deferred" || d.Code == "api.unrealized") && d.Severity == "warning" {
			advisories = append(advisories, d)
		}
	}
	result := validationResult{
		Counts:                    displayCounts(report.Counts),
		ExcludedContainerClusters: displayNames(report.ExcludedContainerClusters),
		ExcludedStorageClusters:   displayNames(report.ExcludedStorageClusters),
		Advisories:                advisories,
	}
	if jsonMode {
		return writeEnvelope(out, commandEnvelope{SchemaVersion: "v1alpha1", Command: escapeDisplayLine(command), OK: true, ExitCode: 0, Result: result, Diagnostics: diagnostics, Logs: []string{}})
	}
	if err := writeHumanDiagnostics(errOut, diagnostics); err != nil {
		return err
	}
	var text display
	text.headline("OK", fmt.Sprintf("Desired state is valid (files seen: %d, objects decoded: %d)", report.Counts.FilesSeen, report.Counts.ObjectsDecoded))
	return text.writeTo(out)
}

func displayNames(values []string) []string {
	values = compilation.SortedNames(values)
	for i := range values {
		values[i] = escapeDisplayLine(values[i])
	}
	return values
}

func displayDiagnostics(values []diagnostic) []diagnostic {
	values = append([]diagnostic{}, values...)
	diagnostics.Sort(values)
	return escapeDiagnostics(values)
}

func displayDiagnosticsInOrder(values []diagnostic) []diagnostic {
	return escapeDiagnostics(append([]diagnostic{}, values...))
}

func escapeDiagnostics(values []diagnostic) []diagnostic {
	for i, d := range values {
		d = diagnostics.WithRemediation(d)
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
	var text display
	for _, d := range diagnostics {
		location := ""
		if d.Source != nil && d.Source.Path != "" {
			location = d.Source.Path
			if d.Source.Line > 0 {
				location += fmt.Sprintf(":%d", d.Source.Line)
				if d.Source.Column > 0 {
					location += fmt.Sprintf(":%d", d.Source.Column)
				}
			}
		}
		subject := d.Code
		if d.Object != nil {
			subject = d.Object.Kind + "/" + d.Object.Name
		}
		text.custom(func(r *displayRenderer) {
			r.gap()
			r.styledParagraph(styleState(d.Severity, r.options)+"  "+styleIdentity(subject, r.options), "  ", "plain")
			if subject != d.Code {
				r.styledParagraph(d.Code, "    ", "muted")
			}
			if location != "" {
				r.styledParagraph(location, "    ", "muted")
			}
			r.styledParagraph(d.Message, "    ", "plain")
			if d.Field != "" {
				r.styledParagraph("Field: "+d.Field, "    ", "muted")
			}
			if d.Remediation != "" {
				r.styledParagraph("Next: "+d.Remediation, "    ", "plain")
			}
		})
	}
	return text.writeTo(out)
}

// displayLines escapes each entry of a list a result names, and is never nil,
// because an envelope field that lists nothing still lists it.
func displayLines(values []string) []string {
	escaped := make([]string, 0, len(values))
	for _, value := range values {
		escaped = append(escaped, escapeDisplayLine(value))
	}
	return escaped
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
