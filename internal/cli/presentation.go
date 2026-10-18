package cli

import (
	"io"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// PresentationOptions holds choices and injected capabilities; it discovers nothing.
type PresentationOptions struct {
	Format, Color, Detail, Progress, Command string
	ASCII                                    bool
	Width                                    int
	Columns                                  func() int
	NoColor, Dumb                            bool
}

// StructuredSecretInputDiagnostic refuses a structured command's terminal input.
func StructuredSecretInputDiagnostic() diagnostics.Diagnostic {
	return diagnostics.Diagnostic{
		Severity: "error", Code: "cli.usage",
		Message:     "structured output requires secret input from a pipe or file",
		Remediation: "pipe the secret into this command, or use --value-file or --password-file",
	}
}

type presentationWriter struct {
	io.Writer
	options PresentationOptions
}

// WithPresentation attaches rendering choices to a stream without changing raw writes.
func WithPresentation(out io.Writer, options PresentationOptions) io.Writer {
	if out == nil {
		out = io.Discard
	}
	return &presentationWriter{Writer: out, options: normalizePresentation(options)}
}

func normalizePresentation(options PresentationOptions) PresentationOptions {
	if options.Format == "" {
		options.Format = "text"
	}
	if options.Color == "" {
		options.Color = "auto"
	}
	if options.Detail == "" {
		options.Detail = "compact"
	}
	if options.Progress == "" {
		options.Progress = "auto"
	}
	return options
}

func presentationFor(out io.Writer) PresentationOptions {
	if configured, ok := out.(*presentationWriter); ok {
		return configured.options
	}
	return normalizePresentation(PresentationOptions{})
}

func (o PresentationOptions) columns() int {
	width, terminal := 96, 0
	if o.Columns != nil {
		terminal = o.Columns()
		if terminal > 0 {
			width = terminal
		}
	}
	if o.Width > 0 {
		width = o.Width
		if terminal > 0 {
			width = min(width, terminal)
		}
	}
	return max(1, width-1)
}

func (o PresentationOptions) colored() bool {
	if o.Format == "json" || o.Format == "yaml" || o.Color == "never" {
		return false
	}
	if o.Color == "always" {
		return true
	}
	return !o.NoColor && !o.Dumb && o.Columns != nil && o.Columns() > 0
}

func presentationStyle(value, code string, options PresentationOptions) string {
	if value == "" || !options.colored() {
		return value
	}
	return "\x1b[" + code + "m" + value + "\x1b[0m"
}

func styleIdentity(value string, options PresentationOptions) string {
	return presentationStyle(value, "1;97", options)
}

func styleMuted(value string, options PresentationOptions) string {
	return presentationStyle(value, "38;5;245", options)
}

func styleAccent(value string, options PresentationOptions) string {
	return presentationStyle(value, "1;38;5;69", options)
}

func styleState(value string, options PresentationOptions) string {
	return presentationStyle(value, stateStyleCode(value), options)
}

func styleStatusSymbol(symbol, state string, options PresentationOptions) string {
	return presentationStyle(symbol, stateStyleCode(state), options)
}

func stateStyleCode(value string) string {
	code := "38;5;245"
	state := strings.ToLower(strings.Trim(value, "[]"))
	if word, _, found := strings.Cut(state, " "); found {
		state = word
	}
	switch state {
	case "ok", "done", "ready", "success", "complete", "completed", "verified", "available", "on", "current", "unchanged", "reused", "trusted", "applied":
		code = "38;5;35"
	case "running", "active", "working", "create", "update", "observe", "preview":
		code = "38;5;69"
	case "warn", "warning", "pending", "queued", "waiting", "deferred", "unknown", "data-loss", "unchecked", "off", "paused", "skipped", "not-applied", "stale", "orphaned", "not-applicable":
		code = "38;5;172"
	case "fail", "failed", "error", "blocked", "unavailable", "missing", "mismatch", "canceled", "invalid", "not-implemented":
		code = "38;5;160"
	}
	return code
}

func displayCellWidth(value string) int { return ansi.StringWidth(value) }

func fitDisplayRow(value string, width int) string {
	return ansi.Truncate(value, max(1, width), "...")
}

func presentationASCII(value string) string {
	return strings.NewReplacer("·", "/", "→", "->", "←", "<-", "—", "-", "–", "-", "✓", "+", "○", "o", "…", "...").Replace(value)
}
