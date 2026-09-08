package cli

import (
	"bytes"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func completionFixture(t *testing.T) *cobra.Command {
	t.Helper()
	root := &cobra.Command{Use: "bootwright"}
	root.PersistentFlags().String("context", "", "Select a context")
	root.PersistentFlags().BoolP("help", "h", false, "Show help")
	cluster := &cobra.Command{Use: "cluster", Short: "Inspect clusters"}
	list := &cobra.Command{Use: "list", Short: "List clusters"}
	list.Flags().String("output", "text", "Choose output")
	if err := list.Flags().SetAnnotation("output", "bootwright.enum", []string{"text", "json"}); err != nil {
		t.Fatal(err)
	}
	cluster.AddCommand(list, &cobra.Command{Use: "info", Short: "Inspect cluster details"})
	validate := &cobra.Command{Use: "validate", Short: "Validate input"}
	validate.Flags().StringArrayP("file", "f", nil, "Read input")
	validate.Flags().Bool("watch", false, "Watch state")
	root.AddCommand(cluster, validate, &cobra.Command{Use: "help"})
	if err := configureCompletion(root); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestCompletionCandidates(t *testing.T) {
	root := completionFixture(t)
	for _, test := range []struct {
		name string
		args []string
		want []string
	}{
		{"root", []string{""}, []string{"cluster", "help", "validate"}},
		{"path", []string{"cluster", ""}, []string{"info", "list"}},
		{"prefix", []string{"cluster", "i"}, []string{"info"}},
		{"help path", []string{"help", "cluster", ""}, []string{"info", "list"}},
		{"help after global", []string{"--context", "sample", "help", "cluster", ""}, []string{"info", "list"}},
		{"help flags", []string{"help", "cluster", "list", "--"}, []string{"--context", "--help"}},
		{"inherited value", []string{"--context", "sample", "cluster", ""}, []string{"info", "list"}},
		{"enum", []string{"cluster", "list", "--output", ""}, []string{"json", "text"}},
		{"attached enum", []string{"cluster", "list", "--output=j"}, []string{"--output=json"}},
		{"flags", []string{"validate", "-"}, []string{"--context", "--file", "--help", "--watch", "-f", "-h"}},
		{"boolean", []string{"validate", "--watch=tr"}, []string{"--watch=true"}},
		{"free context", []string{"--context", ""}, nil},
		{"free path", []string{"validate", "--file", ""}, nil},
		{"free shorthand path", []string{"validate", "-f/tmp/"}, nil},
		{"payload", []string{"validate", "--", ""}, nil},
		{"unknown path", []string{"example", ""}, nil},
		{"unknown flag", []string{"cluster", "--bogus", ""}, nil},
		{"private path", []string{completionRequest, ""}, nil},
		{"hidden candidate", []string{"__"}, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := completionCandidates(root, test.args, false); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("candidates = %q; want %q", got, test.want)
			}
			withDescriptions := completionCandidates(root, test.args, true)
			for i := range withDescriptions {
				withDescriptions[i], _, _ = strings.Cut(withDescriptions[i], "\t")
			}
			if !reflect.DeepEqual(withDescriptions, test.want) {
				t.Fatalf("descriptions changed candidates: %q", withDescriptions)
			}
		})
	}
}

func TestCompletionProtocolIgnoresAmbientConfiguration(t *testing.T) {
	t.Setenv("COBRA_ACTIVE_HELP", "0")
	t.Setenv("BOOTWRIGHT_COMPLETION_DESCRIPTIONS", "false")
	debugPath := t.TempDir() + "/debug"
	t.Setenv("BASH_COMP_DEBUG_FILE", debugPath)
	for _, name := range []string{completionRequest, completionRequestNoDescriptions} {
		root := completionFixture(t)
		var stdout, stderr bytes.Buffer
		root.SetOut(&stdout)
		root.SetErr(&stderr)
		for _, command := range root.Commands() {
			if command.Name() == name {
				if err := command.RunE(command, []string{"cluster", "i"}); err != nil {
					t.Fatal(err)
				}
			}
		}
		want := "info\tInspect cluster details\n:36\n"
		if name == completionRequestNoDescriptions {
			want = "info\n:36\n"
		}
		if stdout.String() != want || stderr.Len() != 0 {
			t.Fatalf("protocol streams = %q, %q; want %q, empty", stdout.String(), stderr.String(), want)
		}
	}
	if _, err := os.Stat(debugPath); !os.IsNotExist(err) {
		t.Fatalf("completion wrote debug file: %v", err)
	}
}

func TestCompletionScripts(t *testing.T) {
	root := completionFixture(t)
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		for _, noDescriptions := range []bool{false, true} {
			var output bytes.Buffer
			if err := writeCompletion(&output, root, shell, noDescriptions); err != nil {
				t.Fatal(err)
			}
			script := output.String()
			if strings.Contains(script, "@PROTOCOL@") || !strings.HasSuffix(script, "\n") {
				t.Fatalf("invalid script framing for %s", shell)
			}
			if noDescriptions || shell == "bash" {
				if !strings.Contains(script, completionRequestNoDescriptions) {
					t.Fatalf("%s retrieves descriptions", shell)
				}
			}
			for _, forbidden := range []string{"eval ", "Invoke-Expression", "Get-ChildItem", "compgen", "_files", "BASH_COMP_DEBUG_FILE", "Getenv", "env:"} {
				if strings.Contains(script, forbidden) {
					t.Fatalf("%s has forbidden fallback %q", shell, forbidden)
				}
			}
		}
	}
}

func TestCompletionLocalFlagOwnership(t *testing.T) {
	root := completionFixture(t)
	render := &cobra.Command{Use: "render"}
	render.Flags().String("input-dir", "", "Read input")
	render.AddCommand(&cobra.Command{Use: "effective"})
	root.AddCommand(render)
	for _, args := range [][]string{
		{"render", "--input-dir", "sample", "effective", "--"},
		{"render", "--input-dir", "sample", "eff"},
		{"help", "render", "--input-dir", ""},
	} {
		if got := completionCandidates(root, args, false); len(got) != 0 {
			t.Errorf("%q offered inapplicable candidates: %q", args, got)
		}
	}
}
