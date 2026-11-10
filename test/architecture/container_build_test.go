package architecture_test

import (
	"os"
	"os/exec"
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
	if !strings.Contains(dockerfile, " make build VULNCHECK=\"$VULNCHECK\";") {
		t.Error("the Dockerfile compiles outside the Makefile build recipe, which owns the flags and the stamp")
	}
}

// buildArguments are the only values the container build takes as build
// arguments, which an engine records in image metadata and build history;
// every other setting may carry a credential and travels as a build secret.
var buildArguments = []string{"GO_VERSION", "GO_IMAGE", "VERSION", "COMMIT", "SOURCE", "VULNCHECK"}

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

const exportingEngine = `#!/bin/sh
set -eu
for argument in "$@"; do
	case "$argument" in
	type=local,dest=*) dest="${argument#type=local,dest=}" ;;
	esac
done
if test -n "$(ls -A "$dest")"; then
	echo "error from receiver: open $dest: permission denied" >&2
	exit 1
fi
printf 'built\n' >"$dest/bootwright"
printf '{}\n' >"$dest/bootwright.cdx.json"
`

func runContainerBuild(t *testing.T, out string) (string, error) {
	t.Helper()
	engine := filepath.Join(t.TempDir(), "engine")
	if err := os.WriteFile(engine, []byte(exportingEngine), 0o755); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("make", "-s", "-C", filepath.Join("..", ".."), "container-build", "CONTAINER_ENGINE="+engine, "CONTAINER_OUT="+out)
	output, err := command.CombinedOutput()
	return string(output), err
}

func TestContainerBuildExportsIntoAPrivateDirectory(t *testing.T) {
	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(out, "unrelated"), []byte("kept\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "bootwright"), []byte("stale\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if output, err := runContainerBuild(t, out); err != nil {
		t.Fatalf("container-build into a shared, non-empty CONTAINER_OUT failed: %v\n%s", err, output)
	}
	for name, want := range map[string]string{"bootwright": "built\n", "bootwright.cdx.json": "{}\n", "unrelated": "kept\n"} {
		data, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != want {
			t.Errorf("%s holds %q, want %q", name, data, want)
		}
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Errorf("CONTAINER_OUT holds %d entries after the build, want bootwright, its bill of materials and unrelated alone", len(entries))
	}
}

func TestContainerBuildRefusesATargetItCannotReplace(t *testing.T) {
	out := t.TempDir()
	if err := os.Mkdir(filepath.Join(out, "bootwright"), 0o755); err != nil {
		t.Fatal(err)
	}
	output, err := runContainerBuild(t, out)
	if err == nil {
		t.Fatalf("container-build replaced a directory named bootwright:\n%s", output)
	}
	if !strings.Contains(output, "is a directory or belongs to another user") {
		t.Errorf("the refusal does not name the unreplaceable target:\n%s", output)
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("the refused build left %d entries in CONTAINER_OUT, want the directory alone", len(entries))
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
