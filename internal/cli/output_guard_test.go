package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHumanOutputUsesOutputPackage(t *testing.T) {
	allowed := map[string]bool{
		"command_tty_linux.go": true,
		"context.go":           true,
		"output/output.go":     true,
		"root.go":              true,
		"scope_print.go":       true,
		"shell_env.go":         true,
	}
	err := filepath.WalkDir(".", func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel := filepath.ToSlash(path)
		rel = strings.TrimPrefix(rel, "./")
		if allowed[rel] {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(body)
		for _, needle := range []string{"fmt.Fprint(", "fmt.Fprintf(", "fmt.Fprintln("} {
			if strings.Contains(text, needle) {
				t.Fatalf("%s uses %s for CLI output; use internal/cli/output or add a narrow allowlist reason", rel, needle)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
