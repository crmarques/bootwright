package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

type diagnostic struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

type failureEnvelope struct {
	SchemaVersion string       `json:"schemaVersion"`
	Command       string       `json:"command"`
	OK            bool         `json:"ok"`
	ExitCode      int          `json:"exitCode"`
	Result        any          `json:"result"`
	Diagnostics   []diagnostic `json:"diagnostics"`
	Logs          []string     `json:"logs"`
}

func writeFailure(out, errOut io.Writer, command, code, message string, exitCode int, jsonMode bool) error {
	if jsonMode {
		encoder := json.NewEncoder(out)
		encoder.SetEscapeHTML(false)
		return encoder.Encode(failureEnvelope{
			SchemaVersion: "v1alpha1",
			Command:       escapeDisplayLine(command),
			ExitCode:      exitCode,
			Diagnostics:   []diagnostic{{Severity: "error", Code: code, Message: escapeDisplayLine(message)}},
			Logs:          []string{},
		})
	}
	_, err := fmt.Fprintf(errOut, "[FAIL] %s: %s\n", code, escapeDisplayLine(message))
	return err
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
