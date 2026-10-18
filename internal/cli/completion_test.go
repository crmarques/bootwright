package cli

import (
	"bytes"
	"context"
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
	root.PersistentFlags().StringP("output", "o", "text", "Choose output")
	if err := root.PersistentFlags().SetAnnotation("output", "bootwright.enum", []string{"text", "json", "yaml"}); err != nil {
		t.Fatal(err)
	}
	cluster.AddCommand(list, &cobra.Command{Use: "info", Short: "Inspect cluster details"})
	validate := &cobra.Command{Use: "validate", Short: "Validate input"}
	validate.Flags().StringArrayP("file", "f", nil, "Read input")
	validate.Flags().Bool("strict", false, "Check strictly")
	root.AddCommand(cluster, validate, &cobra.Command{Use: "help"})
	if err := configureCompletion(root, completionCatalog{}); err != nil {
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
		{"help flags", []string{"help", "cluster", "list", "--"}, []string{"--context", "--help", "--output"}},
		{"inherited value", []string{"--context", "sample", "cluster", ""}, []string{"info", "list"}},
		{"enum", []string{"cluster", "list", "--output", ""}, []string{"json", "text", "yaml"}},
		{"attached enum", []string{"cluster", "list", "--output=j"}, []string{"--output=json"}},
		{"flags", []string{"validate", "-"}, []string{"--context", "--file", "--help", "--output", "--strict", "-f", "-h", "-o"}},
		{"boolean", []string{"validate", "--strict=tr"}, []string{"--strict=true"}},
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

func TestSecretEncryptionCompletionUsesConfiguredStore(t *testing.T) {
	record := &dispatchRecord{}
	var stdout, stderr bytes.Buffer
	runner := New(Config{Out: &stdout, ErrOut: &stderr, Services: dispatchSpies(record)})
	code := runner.Run(context.Background(), []string{completionRequest, "secret", "encryption", "init", "--t"})
	if code != 0 || strings.Contains(stdout.String(), "--type") || stderr.Len() != 0 || record.calls != 0 {
		t.Fatalf("removed selection surfaced: %d %q %q %#v", code, stdout.String(), stderr.String(), record)
	}
	stdout.Reset()
	code = runner.Run(context.Background(), []string{"secret", "encryption", "init", "--help"})
	if code != 0 || strings.Contains(stdout.String(), "--type") || record.calls != 0 {
		t.Fatal("help queried implementation selection")
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

func completionResponse(t *testing.T, candidates []string, args ...string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	runner := New(Config{Out: &stdout, ErrOut: &stderr, Services: dispatchSpies(&dispatchRecord{}), CompletionPaths: func(kind, prefix string) []string {
		return candidates
	}})
	if code := runner.Run(context.Background(), append([]string{completionRequestNoDescriptions}, args...)); code != 0 || stderr.Len() != 0 {
		t.Fatalf("completion = %d %q", code, stderr.String())
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	return lines[len(lines)-1]
}

// TestPathCompletionAppendsNoSpaceOnlyAfterADirectory pins the directive that
// reaches the shell: no space after a directory candidate, whose separator
// would otherwise be followed by one, and the shell's own space after a file,
// which completes the word. A mixed menu keeps no-space so Zsh still reads it.
func TestPathCompletionAppendsNoSpaceOnlyAfterADirectory(t *testing.T) {
	const plain, noSpace = ":36", ":38"
	for _, test := range []struct {
		name       string
		candidates []string
		want       string
	}{
		{"no candidate", nil, plain},
		{"files only", []string{"alpha/input.yaml", "alpha/other.yaml"}, plain},
		{"directories only", []string{"alpha/nested/", "alpha/other/"}, noSpace},
		{"mixed", []string{"alpha/input.yaml", "alpha/nested/"}, noSpace},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := completionResponse(t, test.candidates, "validate", "--file", "alpha/"); got != test.want {
				t.Fatalf("directive = %q; want %q", got, test.want)
			}
		})
	}
	if got := completionResponse(t, []string{"alpha/nested/"}, "validate", "--output", ""); got != plain {
		t.Fatalf("a value flag that is not a path reported %q", got)
	}
}

func completionCandidates(root *cobra.Command, words []string, descriptions bool) []string {
	return completionCandidatesWithCatalog(root, words, descriptions, completionCatalog{})
}

func TestProductionPresentationCompletionUsesOnlyStaticChoices(t *testing.T) {
	root, err := newCommandTree(New(Config{}))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		args []string
		want []string
	}{
		{[]string{"--output", ""}, []string{"json", "text", "yaml"}},
		{[]string{"machine", "info", "-o", ""}, []string{"json", "text", "yaml"}},
		{[]string{"secret", "info", "--color", ""}, []string{"always", "auto", "never"}},
		{[]string{"apply", "--progress", ""}, []string{"always", "auto", "never"}},
		{[]string{"cluster", "info", "--detail", ""}, []string{"compact", "full"}},
	} {
		if got := completionCandidates(root, test.args, false); !reflect.DeepEqual(got, test.want) {
			t.Errorf("%q candidates=%q want=%q", test.args, got, test.want)
		}
	}
}
