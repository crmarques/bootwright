package cli

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestHelpGolden(t *testing.T) {
	root, err := newCommandTree(New(Config{}))
	if err != nil {
		t.Fatal(err)
	}
	var walk func(*cobra.Command)
	walk = func(command *cobra.Command) {
		if command.Hidden {
			return
		}
		path := strings.TrimSpace(strings.TrimPrefix(command.CommandPath(), "bootwright"))
		name := "root"
		if path != "" {
			name = strings.ReplaceAll(path, " ", "-")
		}
		t.Run(name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			args := append(strings.Fields(path), "--help")
			code := New(Config{Out: &out, ErrOut: &errOut}).Run(context.Background(), args)
			if code != 0 || errOut.Len() != 0 {
				t.Fatalf("help: %d %q", code, errOut.String())
			}
			matchesTextGolden(t, "help-"+name, out.Bytes())
			if (specFor(path).implemented || presentationCommand(path)) && !strings.Contains(out.String(), "Examples:") {
				t.Error("available command has no example")
			}
			for _, flag := range specFor(path).flags {
				for _, value := range flag.enum {
					if !strings.Contains(out.String(), value) {
						t.Errorf("--%s hides value %s", flag.name, value)
					}
				}
			}
		})
		for _, child := range command.Commands() {
			walk(child)
		}
	}
	walk(root)
}

func TestHelpMarksUnavailableCommands(t *testing.T) {
	for _, spec := range commandCatalog() {
		if spec.implemented || presentationCommand(spec.path) {
			continue
		}
		code, out, errOut, record := runRecorded(append(strings.Fields(spec.path), "--help"))
		if code != 0 || errOut != "" || record.calls != 0 || !strings.Contains(out, "recognized but not implemented") {
			t.Fatalf("%s: %d %q %q", spec.path, code, out, errOut)
		}
		if slices.Contains(strings.Fields(spec.path), "cluster") && strings.Contains(out, "Examples:") {
			t.Errorf("unavailable command %s offers execution example", spec.path)
		}
	}
}

func TestScalarEnumRefusalListsAllowedValues(t *testing.T) {
	code, out, errOut, record := runRecorded([]string{"validate", "--output", "unsupported"})
	if code != 2 || out != "" || record.calls != 0 || !strings.Contains(errOut, "--output must be one of: text, json") {
		t.Fatalf("%d %q %q", code, out, errOut)
	}
}
