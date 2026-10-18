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
			if (specFor(path).implemented || presentationCommand(path)) && !strings.Contains(out.String(), "  Examples\n") {
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
		if code != 0 || errOut != "" || record.calls != 0 || !strings.Contains(strings.Join(strings.Fields(out), " "), "recognized but not implemented") {
			t.Fatalf("%s: %d %q %q", spec.path, code, out, errOut)
		}
		if slices.Contains(strings.Fields(spec.path), "cluster") && strings.Contains(out, "  Examples\n") {
			t.Errorf("unavailable command %s offers execution example", spec.path)
		}
	}
}

// A command group is listed as unavailable only when neither it nor any
// subcommand under it is available: render runs `render effective`, so its
// listing is not marked while its unavailable subcommands are.
func TestAGroupWithAnAvailableSubcommandIsNotListedUnavailable(t *testing.T) {
	root, err := newCommandTree(New(Config{}))
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]string{}
	var walk func(*cobra.Command)
	walk = func(command *cobra.Command) {
		for _, child := range command.Commands() {
			listed[strings.TrimPrefix(child.CommandPath(), "bootwright ")] = child.Short
			walk(child)
		}
	}
	walk(root)
	for _, spec := range commandCatalog() {
		if presentationCommand(spec.path) {
			continue
		}
		available := spec.implemented || hasAvailableSubcommand(spec.path)
		if marked := strings.HasSuffix(listed[spec.path], " (unavailable)"); marked == available {
			t.Errorf("%s is listed %q but available is %v", spec.path, listed[spec.path], available)
		}
	}
	if strings.HasSuffix(listed["render"], "(unavailable)") || !strings.HasSuffix(listed["render installer"], "(unavailable)") || !strings.HasSuffix(listed["render storage"], "(unavailable)") || strings.HasSuffix(listed["render effective"], "(unavailable)") {
		t.Errorf("render listings: %q %q %q %q", listed["render"], listed["render effective"], listed["render installer"], listed["render storage"])
	}
}

func TestScalarEnumRefusalListsAllowedValues(t *testing.T) {
	code, out, errOut, record := runRecorded([]string{"validate", "--output", "unsupported"})
	if code != 2 || out != "" || record.calls != 0 || !strings.Contains(errOut, "--output must be one of: text, json, yaml") {
		t.Fatalf("%d %q %q", code, out, errOut)
	}
}

func TestHelpExposesInheritedPresentationChoices(t *testing.T) {
	for _, path := range []string{"", "machine info", "secret info", "apply"} {
		code, out, errOut, record := runRecorded(append(strings.Fields(path), "--help"))
		if code != 0 || errOut != "" || record.calls != 0 {
			t.Fatalf("%s help: %d %q", path, code, errOut)
		}
		for _, name := range []string{"--output", "--color", "--progress", "--detail", "--ascii", "--width"} {
			if !strings.Contains(out, name) {
				t.Errorf("%s help omits inherited %s", path, name)
			}
		}
		for _, value := range []string{"yaml", "auto", "always", "never", "compact", "full"} {
			if !strings.Contains(out, value) {
				t.Errorf("%s help omits presentation value %s", path, value)
			}
		}
	}
}
