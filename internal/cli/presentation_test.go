package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestAutomaticColorUsesEachStreamsTerminal(t *testing.T) {
	for _, stdoutTTY := range []bool{false, true} {
		for _, stderrTTY := range []bool{false, true} {
			var out, errOut bytes.Buffer
			capability := func(terminal bool) func() int {
				if terminal {
					return func() int { return 96 }
				}
				return nil
			}
			runner := New(Config{Out: &out, ErrOut: &errOut,
				Presentation: PresentationOptions{Columns: capability(stdoutTTY)},
				ErrorColumns: capability(stderrTTY)})
			if code := runner.Run(context.Background(), []string{"help", "machine", "info"}); code != 0 {
				t.Fatalf("help exit=%d", code)
			}
			if code := runner.Run(context.Background(), []string{"--unknown"}); code != 2 {
				t.Fatalf("usage exit=%d", code)
			}
			if strings.Contains(out.String(), "\x1b[") != stdoutTTY || strings.Contains(errOut.String(), "\x1b[") != stderrTTY {
				t.Fatalf("stdoutTTY=%t stderrTTY=%t: stdout=%q stderr=%q", stdoutTTY, stderrTTY, out.String(), errOut.String())
			}
		}
	}
}

func TestPresentationPoliciesAndCellWidths(t *testing.T) {
	for _, width := range []int{24, 48, 96, 220} {
		for _, color := range []string{"always", "never"} {
			var out bytes.Buffer
			var text display
			text.headline("OK", "Inspection complete")
			text.resourceHeading("Machine", "界界-example", "ready")
			text.mutedFields(field{Label: "Needs", Value: "controller"}, field{Label: "Command", Value: "bootwright apply --authorize data-loss"})
			if err := text.writeTo(WithPresentation(&out, PresentationOptions{Width: width, Color: color})); err != nil {
				t.Fatal(err)
			}
			plain := ansi.Strip(out.String())
			if !strings.Contains(plain, "界界-example") || !strings.Contains(strings.Join(strings.Fields(plain), " "), "--authorize data-loss") {
				t.Fatalf("width %d lost identity or command: %q", width, out.String())
			}
			for _, line := range strings.Split(out.String(), "\n") {
				if displayCellWidth(line) >= width {
					t.Fatalf("width %d overflow: %q", width, line)
				}
			}
			if strings.Contains(out.String(), "\x1b") != (color == "always") {
				t.Fatalf("color policy %s: %q", color, out.String())
			}
		}
	}
}

func TestPresentationColorSuppressionAndOverrides(t *testing.T) {
	terminal := func() int { return 120 }
	for _, item := range []struct {
		options PresentationOptions
		colored bool
	}{
		{PresentationOptions{Columns: terminal}, true},
		{PresentationOptions{Columns: terminal, NoColor: true}, false},
		{PresentationOptions{Columns: terminal, Dumb: true}, false},
		{PresentationOptions{Columns: terminal, Color: "never"}, false},
		{PresentationOptions{Color: "always", NoColor: true, Dumb: true}, true},
		{PresentationOptions{Color: "always", Format: "json"}, false},
		{PresentationOptions{Color: "always", Format: "yaml"}, false},
		{PresentationOptions{}, false},
	} {
		if got := strings.Contains(styleIdentity("example", item.options), "\x1b"); got != item.colored {
			t.Errorf("%+v: colored=%t", item.options, got)
		}
	}
	if got := (PresentationOptions{Width: 220}).columns(); got != 219 {
		t.Fatalf("explicit pipe width capped to %d", got)
	}
	if got := (PresentationOptions{Width: 220, Columns: terminal}).columns(); got != 119 {
		t.Fatalf("terminal bound ignored: %d", got)
	}
}

func TestPresentationDiagnosticsKeepEscapedDataAndRemedy(t *testing.T) {
	raw := "name\x1b[2J\\path"
	var out bytes.Buffer
	writer := WithPresentation(&out, PresentationOptions{Width: 24, Color: "always"})
	values := displayDiagnostics([]diagnostic{{Severity: "error", Code: "cli.usage", Message: raw, Remediation: "bootwright apply --authorize data-loss"}})
	if err := writeHumanDiagnostics(writer, values); err != nil {
		t.Fatal(err)
	}
	plain := strings.Join(strings.Fields(ansi.Strip(out.String())), " ")
	if strings.Contains(out.String(), "\x1b[2J") || !strings.Contains(plain, escapeDisplayLine(raw)) || !strings.Contains(plain, "--authorize data-loss") {
		t.Fatalf("diagnostic lost or reinterpreted data: %q", out.String())
	}
}

func TestHumanBlocksNeverLeakToStructuredStreams(t *testing.T) {
	for _, format := range []string{"json", "yaml"} {
		var out bytes.Buffer
		writer := WithPresentation(&out, PresentationOptions{Format: format, Color: "always"})
		var text display
		text.headline("OK", "Plan prepared")
		text.steps([]step{{Text: "prepare example", Effects: []string{"replace owned storage"}}})
		if err := text.writeTo(writer); err != nil {
			t.Fatal(err)
		}
		if err := writeHumanDiagnostics(writer, displayDiagnostics([]diagnostic{{Severity: "warning", Code: "api.deferred", Message: "deferred"}})); err != nil || out.Len() != 0 {
			t.Fatalf("%s: human output reached structured stream: %q, %v", format, out.String(), err)
		}
	}
}
