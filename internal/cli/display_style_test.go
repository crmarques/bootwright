package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func styledSample(t *testing.T, options PresentationOptions) string {
	t.Helper()
	var text display
	text.resourceTable([]string{"NAME", "SIZE", "STATE"}, [][]string{{"boot.iso", "997 MiB", "verified"}, {"dvd.iso", "12 GiB", "mismatch"}}, tableStyle{Kind: "media", Identity: 0, States: []int{2}})
	text.section("")
	text.fields(field{Label: "Mode", Value: "ready", Tone: "state"}, field{Label: "Revision", Value: "rev-1"}, field{Label: "Next", Value: "bootwright check input --context lab", Tone: "command"})
	text.mutedFields(field{Label: "Contact", Value: "rhel-01.lab.example.test"}, field{Label: "Fingerprint", Value: "AB:CD", Tone: "muted"})
	var out bytes.Buffer
	if err := text.writeTo(WithPresentation(&out, options)); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestValuesReadAsTextWhileLabelsAndHeadersStayMuted(t *testing.T) {
	for _, width := range []int{24, 96} {
		options := PresentationOptions{Color: "always", Width: width}
		colored := styledSample(t, options)
		options.Color = "never"
		if plain := styledSample(t, options); ansi.Strip(colored) != plain {
			t.Fatalf("%d: styling changed the content or layout:\n%s", width, plain)
		}
		options.Color = "always"
		for _, want := range []string{
			styleIdentity("boot.iso", options), styleState("verified", options), styleState("mismatch", options),
			styleState("ready", options), styleCommand("bootwright check input --context lab", options),
			styleMuted("AB:CD", options),
		} {
			if !strings.Contains(colored, want) {
				t.Errorf("%d: output lacks %q:\n%s", width, want, colored)
			}
		}
		for _, value := range []string{"997 MiB", "rev-1", "rhel-01.lab.example.test"} {
			if strings.Contains(colored, styleMuted(value, options)) || !strings.Contains(colored, value) {
				t.Errorf("%d: value %q is muted or missing:\n%s", width, value, colored)
			}
		}
		if width >= 96 && !strings.Contains(colored, "\x1b[38;5;245mContact") {
			t.Errorf("%d: a nested label is not muted:\n%s", width, colored)
		}
	}
}

func TestHeaderlessRowsWrapTheirLastColumnUnderItsOwnStart(t *testing.T) {
	var text display
	text.stateRows([][]string{{"[UNKNOWN]", "Controller runtime", "required Python 3.14 with ansible-core; observed Python 3.14.8; runtime not verified without privilege"}}, 0)
	var out bytes.Buffer
	if err := text.writeTo(WithPresentation(&out, PresentationOptions{Width: 72})); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	start := strings.Index(lines[0], "required")
	if len(lines) < 2 || start < 0 {
		t.Fatalf("rows = %q", out.String())
	}
	for _, line := range lines[1:] {
		if strings.TrimLeft(line, " ") == "" || len(line)-len(strings.TrimLeft(line, " ")) != start {
			t.Fatalf("continuation %q does not start at column %d:\n%s", line, start, out.String())
		}
	}
}
