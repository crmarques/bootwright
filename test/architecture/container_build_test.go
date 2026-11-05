package architecture_test

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
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
	if !strings.Contains(containerBuildRecipe(t, makefile), "--build-arg GO_VERSION='$(or $(GO_VERSION),") {
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

// buildArguments are the only values the container build takes as build
// arguments, which an engine records in image metadata and build history;
// every other setting may carry a credential and travels as a build secret.
var buildArguments = []string{"GO_VERSION", "GO_IMAGE", "VERSION", "COMMIT", "SOURCE"}

func TestContainerBuildPassesSettingsOnlyAsASecret(t *testing.T) {
	root := filepath.Join("..", "..")
	dockerfile, err := os.ReadFile(filepath.Join(root, "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	for _, match := range regexp.MustCompile(`(?m)^ARG ([A-Za-z_]+)`).FindAllStringSubmatch(string(dockerfile), -1) {
		if !slices.Contains(buildArguments, match[1]) {
			t.Errorf("the Dockerfile declares build argument %s; a setting that may carry a credential belongs in the build-env secret", match[1])
		}
	}
	if !strings.Contains(string(dockerfile), "--mount=type=secret,id=build-env") {
		t.Error("the Dockerfile does not read the build-env secret")
	}
	makefile, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	recipe := containerBuildRecipe(t, string(makefile))
	for _, match := range regexp.MustCompile(`--build-arg ([A-Za-z_$(]+)`).FindAllStringSubmatch(recipe, -1) {
		if !slices.Contains(buildArguments, match[1]) {
			t.Errorf("the container-build recipe passes build argument %s; a setting that may carry a credential belongs in the build-env secret", match[1])
		}
	}
	if !strings.Contains(recipe, "--secret id=build-env,src=") {
		t.Error("the container-build recipe does not pass the build-env secret")
	}
}

func containerBuildRecipe(t *testing.T, makefile string) string {
	t.Helper()
	_, recipe, found := strings.Cut(makefile, "\ncontainer-build:\n")
	if !found {
		t.Fatal("the Makefile has no container-build target")
	}
	recipe, _, _ = strings.Cut(recipe, "\n\n")
	return recipe
}
