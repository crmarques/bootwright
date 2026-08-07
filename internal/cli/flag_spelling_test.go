package cli

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

var proseCommandPattern = regexp.MustCompile(`bootwright(?: [a-z][a-z0-9-]*| --?[a-z][a-z0-9-]*(?:=[^\s` + "`" + `]+)?)+`)

var proseCommandTrees = []string{"ansible", "docs", "specs", ".agents"}

var proseCommandExemptions = map[string]string{
	"bootwright secret set --name": "the ADR 0010 flag-convention narrative quotes a flag shape, not a runnable line",
}

func proseCommandFlagIsAccepted(cmd *cobra.Command, name string, shorthand bool) bool {
	if name == "help" || name == "h" {
		return true
	}
	if shorthand {
		return cmd.Flags().ShorthandLookup(name) != nil || cmd.InheritedFlags().ShorthandLookup(name) != nil
	}
	return cmd.Flags().Lookup(name) != nil || cmd.InheritedFlags().Lookup(name) != nil
}

func checkProseCommand(t *testing.T, rel, invocation string) {
	t.Helper()
	fields := strings.Fields(invocation)
	if len(fields) < 2 {
		return
	}
	type proseFlag struct {
		name      string
		shorthand bool
	}
	var path []string
	var flags []proseFlag
	for _, field := range fields[1:] {
		if strings.HasPrefix(field, "-") {
			shorthand := !strings.HasPrefix(field, "--")
			flags = append(flags, proseFlag{name: strings.SplitN(strings.TrimLeft(field, "-"), "=", 2)[0], shorthand: shorthand})
			continue
		}
		if len(flags) == 0 {
			path = append(path, field)
		}
	}
	root := newRootCmd(bytes.NewReader(nil), &bytes.Buffer{}, &bytes.Buffer{})
	cmd, rest, err := root.Find(path)
	if err != nil || len(rest) > 0 {
		return
	}
	for _, flag := range flags {
		if proseCommandFlagIsAccepted(cmd, flag.name, flag.shorthand) {
			continue
		}
		dashes := "--"
		if flag.shorthand {
			dashes = "-"
		}
		t.Errorf("%s prints `%s`, but %q does not accept %s%s; a refusal or a doc that names a command the CLI rejects with exit 2 leaves the operator with no way forward, which is exactly what the Diagnostics contract forbids", rel, invocation, cmd.CommandPath(), dashes, flag.name)
	}
}

func TestEveryBootwrightCommandInProseParses(t *testing.T) {
	root := repoRootFromCLI(t)
	var invocations []string
	seen := map[string]bool{}
	for _, tree := range proseCommandTrees {
		base := filepath.Join(root, tree)
		err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if ext := filepath.Ext(path); ext != ".yml" && ext != ".yaml" && ext != ".md" {
				return nil
			}
			body, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			for _, match := range proseCommandPattern.FindAllString(string(body), -1) {
				normalized := strings.Join(strings.Fields(match), " ")
				key := filepath.ToSlash(rel) + "\x00" + normalized
				if seen[key] {
					continue
				}
				seen[key] = true
				invocations = append(invocations, key)
			}
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			t.Fatalf("walk %s: %v", tree, err)
		}
	}
	sort.Strings(invocations)
	for _, entry := range invocations {
		rel, invocation, _ := strings.Cut(entry, "\x00")
		if _, exempt := proseCommandExemptions[invocation]; exempt {
			continue
		}
		checkProseCommand(t, rel, invocation)
	}
}

func repoRootFromCLI(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test working directory")
		}
		dir = parent
	}
}
