package architecture_test

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

var (
	makeRule    = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9_-]*):([^=].*)?$`)
	rootCompile = regexp.MustCompile(`\$\(GO\) (test|vet|build)\b|govulncheck `)
)

// targetsMissingWheels names each Makefile target whose recipe compiles the
// root module, which embeds the runtime wheels, without wheels among its
// prerequisites, so a parallel make could compile while they are placed.
func targetsMissingWheels(makefile string) []string {
	var missing []string
	target, prerequisites, compiles := "", []string(nil), false
	finish := func() {
		if target != "" && compiles && !slices.Contains(prerequisites, "wheels") {
			missing = append(missing, target)
		}
	}
	for _, line := range strings.Split(makefile, "\n") {
		if strings.HasPrefix(line, "\t") {
			if target != "" && rootCompile.MatchString(line) && !strings.Contains(line, "-C scripts/tools") {
				compiles = true
			}
			continue
		}
		if match := makeRule.FindStringSubmatch(line); match != nil && !strings.HasPrefix(line, ".PHONY") {
			finish()
			target, prerequisites, compiles = match[1], strings.Fields(match[2]), false
			continue
		}
		if line == "" || !strings.HasPrefix(line, "#") {
			finish()
			target = ""
		}
	}
	finish()
	return missing
}

func TestEveryTargetThatCompilesTheWheelsDependsOnThem(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	if missing := targetsMissingWheels(string(data)); len(missing) != 0 {
		t.Errorf("%v compile the module that embeds internal/controller/runtimelock/wheels without depending on wheels; declare it so a parallel make cannot compile a partial wheel", missing)
	}
	planted := "vet: fmt-check\n\t$(GO) vet ./...\n\ntools: wheels\n\t$(GO) -C scripts/tools test ./...\n\nscan:\n\t./scripts/govulncheck -db x ./...\n"
	if missing := targetsMissingWheels(planted); !slices.Equal(missing, []string{"vet", "scan"}) {
		t.Errorf("the planted Makefile reported %v, want [vet scan]", missing)
	}
}
