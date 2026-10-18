package cli

import (
	"bytes"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestCarryPresentationNeverExceedsArgumentBounds(t *testing.T) {
	for _, limit := range []string{"count", "bytes"} {
		args := []string{"machine", "exec", "--name", "example", "--", "printf"}
		if limit == "count" {
			for len(args) < maxArguments {
				args = append(args, "")
			}
		} else {
			total := 0
			for _, arg := range args {
				total += len(arg)
			}
			for total < maxTotalArgumentBytes {
				size := min(maxArgumentBytes, maxTotalArgumentBytes-total)
				args = append(args, strings.Repeat("x", size))
				total += size
			}
		}
		if reason := rawBounds(args); reason != "" {
			t.Fatal("invalid boundary fixture", reason)
		}
		carried := CarryPresentation(args, PresentationOptions{NoColor: true})
		if rawBounds(carried) != "" || !slices.Equal(carried, args) {
			t.Fatalf("color preference changed an admitted %s boundary invocation", limit)
		}
	}
}

func TestCarryPresentationPreservesArgumentsAndResolvedColor(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		base PresentationOptions
		want []string
	}{
		{"default", []string{"context", "list"}, PresentationOptions{NoColor: true}, []string{"context", "list", "--color=never"}},
		{"prefix", []string{"--context", "demo", "context", "list"}, PresentationOptions{NoColor: true}, []string{"--context", "demo", "context", "list", "--color=never"}},
		{"explicit auto", []string{"--color", "auto", "machine", "list"}, PresentationOptions{NoColor: true}, []string{"--color", "auto", "machine", "list", "--color=never"}},
		{"final auto", []string{"machine", "--color=always", "list", "--color", "auto"}, PresentationOptions{NoColor: true}, []string{"machine", "--color=always", "list", "--color", "auto", "--color=never"}},
		{"explicit always", []string{"machine", "list", "--color", "always"}, PresentationOptions{NoColor: true}, []string{"machine", "list", "--color", "always"}},
		{"explicit never", []string{"machine", "list", "--color=never"}, PresentationOptions{NoColor: true}, []string{"machine", "list", "--color=never"}},
		{"automatic without suppression", []string{"machine", "list"}, PresentationOptions{}, []string{"machine", "list"}},
		{"injected always", []string{"machine", "list"}, PresentationOptions{NoColor: true, Color: "always"}, []string{"machine", "list"}},
		{"separator", []string{"machine", "exec", "--name", "demo", "--", "uptime"}, PresentationOptions{NoColor: true}, []string{"machine", "exec", "--name", "demo", "--color=never", "--", "uptime"}},
		{"implicit payload", []string{"machine", "exec", "--name", "demo", "uptime"}, PresentationOptions{NoColor: true}, []string{"machine", "exec", "--name", "demo", "--color=never", "uptime"}},
		{"payload color literal", []string{"machine", "exec", "--name", "demo", "printf", "", "--color", "always", "a b"}, PresentationOptions{NoColor: true}, []string{"machine", "exec", "--name", "demo", "--color=never", "printf", "", "--color", "always", "a b"}},
		{"flag-shaped payload", []string{"cluster", "exec", "--name", "demo", "--", "--color", "always"}, PresentationOptions{NoColor: true}, []string{"cluster", "exec", "--name", "demo", "--color=never", "--", "--color", "always"}},
		{"payload separator literal", []string{"cluster", "exec", "--name", "demo", "--", "--", "--color", "never"}, PresentationOptions{NoColor: true}, []string{"cluster", "exec", "--name", "demo", "--color=never", "--", "--", "--color", "never"}},
		{"secret value", []string{"secret", "set", "--name", "demo", "--username", "private value --color always"}, PresentationOptions{NoColor: true}, []string{"secret", "set", "--name", "demo", "--username", "private value --color always", "--color=never"}},
		{"flag-shaped secret value", []string{"secret", "set", "--name", "demo", "--username", "--color"}, PresentationOptions{NoColor: true}, []string{"secret", "set", "--name", "demo", "--username", "--color", "--color=never"}},
		{"separator secret value", []string{"secret", "set", "--name", "demo", "--username", "--"}, PresentationOptions{NoColor: true}, []string{"secret", "set", "--name", "demo", "--username", "--", "--color=never"}},
		{"separator target value", []string{"machine", "exec", "--name", "--", "uptime"}, PresentationOptions{NoColor: true}, []string{"machine", "exec", "--name", "--", "--color=never", "uptime"}},
		{"invalid syntax", []string{"machine", "list", "--unknown"}, PresentationOptions{NoColor: true}, []string{"machine", "list", "--unknown"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			original := slices.Clone(test.args)
			carried := CarryPresentation(test.args, test.base)
			if !reflect.DeepEqual(carried, test.want) || !reflect.DeepEqual(test.args, original) {
				t.Fatalf("carried %q want %q; original %q", carried, test.want, test.args)
			}
			if test.name == "invalid syntax" {
				return
			}
			before := parsedCommand(t, test.args)
			after := parsedCommand(t, carried)
			if !slices.Equal(before.Flags().Args(), after.Flags().Args()) {
				t.Fatalf("payload changed: %q to %q", before.Flags().Args(), after.Flags().Args())
			}
			for _, name := range []string{"context", "name", "username"} {
				if stringValue(before.Flags(), name) != stringValue(after.Flags(), name) {
					t.Fatalf("--%s value changed", name)
				}
			}
			if len(carried) > len(test.args) && stringValue(after.Flags(), "color") != "never" {
				t.Fatal("override did not resolve to never")
			}
			if len(carried) > 0 {
				carried[0] = "changed"
			}
			if !reflect.DeepEqual(test.args, original) {
				t.Fatal("returned arguments alias the original")
			}
		})
	}
}

func TestPrivilegeDiagnosticUsesParsedPresentationOptions(t *testing.T) {
	classification := ClassifyInvocation([]string{"context", "list", "--color", "always", "--ascii", "--width", "32"})
	if classification.Presentation.Columns != nil || classification.Presentation.NoColor || classification.Presentation.Dumb {
		t.Fatal("classification acquired process capabilities")
	}
	var out, errOut bytes.Buffer
	if code := classification.Diagnostic(&out, &errOut, diagnostic{Severity: "error", Code: "runtime.privilege", Message: "a long refusal message with → a next observation"}, 1); code != 1 || out.Len() != 0 {
		t.Fatalf("code %d stdout %q", code, out.String())
	}
	if !strings.Contains(errOut.String(), "\x1b[") || strings.Contains(errOut.String(), "→") || !strings.Contains(errOut.String(), "->") || strings.Count(errOut.String(), "\n") < 3 {
		t.Fatalf("presentation flags were ignored: %q", errOut.String())
	}
	for _, line := range strings.Split(strings.TrimRight(errOut.String(), "\n"), "\n") {
		if displayCellWidth(line) > 31 {
			t.Fatalf("width ignored: %q", line)
		}
	}
	classification = ClassifyInvocation([]string{"context", "list"})
	classification.Presentation.Columns = func() int { return 120 }
	classification.Presentation.NoColor = true
	errOut.Reset()
	classification.Diagnostic(&out, &errOut, diagnostic{Severity: "error", Code: "runtime.privilege", Message: "refused"}, 1)
	if strings.Contains(errOut.String(), "\x1b[") {
		t.Fatal("injected color suppression was lost")
	}
}
