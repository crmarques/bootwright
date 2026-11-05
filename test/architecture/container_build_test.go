package architecture_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestContainerBuildTakesGoFromGoMod(t *testing.T) {
	root := filepath.Join("..", "..")
	read := func(name string) string {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	toolchain := regexp.MustCompile(`(?m)^export GOTOOLCHAIN=go(\S+)$`).FindStringSubmatch(read("scripts/go"))
	if toolchain == nil {
		t.Fatal("scripts/go selects no GOTOOLCHAIN")
	}
	directive := regexp.MustCompile(`(?m)^go (\S+)$`).FindStringSubmatch(read("go.mod"))
	if directive == nil {
		t.Fatal("go.mod has no go directive")
	}
	if directive[1] != toolchain[1] {
		t.Errorf("the container build takes Go %s from go.mod but scripts/go selects Go %s; move both together", directive[1], toolchain[1])
	}
	makefile := read("Makefile")
	if !strings.Contains(makefile, "GO_VERSION = $(shell awk '$$1 == \"go\" { print $$2; exit }' go.mod)") {
		t.Error("the Makefile does not read GO_VERSION from the go directive of go.mod")
	}
	if !regexp.MustCompile(`(?m)^container-build:\n\t.* build .*--build-arg GO_VERSION=\$\(or \$\(GO_VERSION\),`).MatchString(makefile) {
		t.Error("the Makefile container-build recipe does not pass GO_VERSION to the Dockerfile")
	}
	dockerfile := read("Dockerfile")
	if !strings.Contains(dockerfile, "\nARG GO_IMAGE=docker.io/library/golang:${GO_VERSION}-") {
		t.Error("the Dockerfile GO_IMAGE default does not take its tag from GO_VERSION")
	}
	if !strings.Contains(dockerfile, " make build;") {
		t.Error("the Dockerfile compiles outside the Makefile build recipe, which owns the flags and the stamp")
	}
}
