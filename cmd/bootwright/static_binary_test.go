package main

import (
	"debug/elf"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// make build yields an executable with no dynamic dependencies, so a binary
// built on one host runs on another whatever C library that host carries. The
// recipe disables cgo, which leaves the pure-Go resolver to serve every
// lookup; the test builds with the recipe's own environment and flags and
// reads the result.
func TestMakeBuildYieldsAStaticExecutable(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the executable")
	}
	if runtime.GOOS != "linux" {
		t.Skip("reads an ELF executable")
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	recipe := regexp.MustCompile(`(?m)^build:\n\t(.+)$`).FindSubmatch(data)
	if recipe == nil {
		t.Fatal("the Makefile has no one-line build recipe")
	}
	line := string(recipe[1])
	if !strings.HasPrefix(line, "CGO_ENABLED=0 $(GO) build ") {
		t.Fatalf("the build recipe %q does not disable cgo", line)
	}
	for _, flag := range []string{" -trimpath ", " -buildvcs=false "} {
		if !strings.Contains(line, flag) {
			t.Fatalf("the build recipe %q lacks %s", line, strings.TrimSpace(flag))
		}
	}
	environment := os.Environ()
	for _, word := range strings.Fields(line) {
		name, _, assignment := strings.Cut(word, "=")
		if !assignment || strings.HasPrefix(name, "-") || strings.HasPrefix(name, "$") {
			break
		}
		environment = append(environment, word)
	}
	binary := filepath.Join(t.TempDir(), "bootwright")
	build := exec.CommandContext(t.Context(), "go", "build", "-trimpath", "-buildvcs=false", "-o", binary, "./cmd/bootwright")
	build.Dir, build.Env = filepath.Join("..", ".."), environment
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	executable, err := elf.Open(binary)
	if err != nil {
		t.Fatal(err)
	}
	defer executable.Close()
	libraries, err := executable.ImportedLibraries()
	if err != nil {
		t.Fatal(err)
	}
	if len(libraries) != 0 {
		t.Fatalf("the executable needs %v", libraries)
	}
	for _, program := range executable.Progs {
		if program.Type == elf.PT_INTERP {
			t.Fatal("the executable names a dynamic loader")
		}
	}
}
