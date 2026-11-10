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

func TestTheContainerBuildPinsTheGoImageOfGoMod(t *testing.T) {
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
		t.Errorf("go.mod names Go %s but scripts/go selects Go %s; move both together", directive[1], toolchain[1])
	}
	dockerfile := read("Dockerfile")
	image := regexp.MustCompile(`(?m)^ARG GO_IMAGE=docker\.io/library/golang:(\d+\.\d+\.\d+)-trixie@sha256:[0-9a-f]{64}$`).FindStringSubmatch(dockerfile)
	if image == nil {
		t.Fatal("the Dockerfile's GO_IMAGE default is not docker.io/library/golang:<go version>-trixie pinned by @sha256:<digest>")
	}
	if image[1] != directive[1] {
		t.Errorf("the Dockerfile pins the Go image of Go %s but go.mod names Go %s; pin the digest of golang:%s-trixie from the registry", image[1], directive[1], directive[1])
	}
	if strings.Contains(dockerfile, "GO_VERSION") || strings.Contains(read("Makefile"), "GO_VERSION") {
		t.Error("a build argument or variable named GO_VERSION names a Go release beside the pinned image")
	}
	if !strings.Contains(dockerfile, " make build VULNCHECK=\"$VULNCHECK\";") {
		t.Error("the Dockerfile compiles outside the Makefile build recipe, which owns the flags and the stamp")
	}
}

// buildArguments are the only values the container build takes as build
// arguments, which an engine records in image metadata and build history;
// every other setting may carry a credential and travels as a build secret.
var buildArguments = []string{"GO_IMAGE", "VERSION", "COMMIT", "SOURCE", "VULNCHECK"}

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
printf 'notices\n' >"$dest/bootwright.notices.txt"
`

func runContainerBuild(t *testing.T, out string) (string, error) {
	t.Helper()
	return runContainerBuildWith(t, out, exportingEngine)
}

func runContainerBuildWith(t *testing.T, out, script string) (string, error) {
	t.Helper()
	engine := filepath.Join(t.TempDir(), "engine")
	if err := os.WriteFile(engine, []byte(script), 0o755); err != nil {
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
	for name, want := range map[string]string{"bootwright": "built\n", "bootwright.cdx.json": "{}\n", "bootwright.notices.txt": "notices\n", "unrelated": "kept\n"} {
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
	if len(entries) != 4 {
		t.Errorf("CONTAINER_OUT holds %d entries after the build, want bootwright, its bill of materials, its notices and unrelated alone", len(entries))
	}
}

func TestContainerBuildRefusesAnExportThatOmitsTheNotices(t *testing.T) {
	out := t.TempDir()
	previous := map[string]string{"bootwright": "old executable\n", "bootwright.cdx.json": "old bill\n", "bootwright.notices.txt": "old notices\n"}
	for name, text := range previous {
		if err := os.WriteFile(filepath.Join(out, name), []byte(text), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	script := strings.Replace(exportingEngine, "printf 'notices\\n' >\"$dest/bootwright.notices.txt\"\n", "", 1)
	if script == exportingEngine {
		t.Fatal("the test engine does not write the notices to drop")
	}
	output, err := runContainerBuildWith(t, out, script)
	if err == nil {
		t.Fatalf("container-build finished without the third-party notices:\n%s", output)
	}
	for name, text := range previous {
		data, readErr := os.ReadFile(filepath.Join(out, name))
		if readErr != nil || string(data) != text {
			t.Errorf("the refused build changed %s to %q (%v), so it no longer matches its neighbours", name, data, readErr)
		}
	}
}

func TestTheBuildRecipeWritesTheNoticesFromTheBillOfMaterials(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	_, recipe, _ := strings.Cut(string(data), "\nbuild: wheels vulngate\n")
	recipe, _, _ = strings.Cut(recipe, "\n\n")
	bom := strings.Index(recipe, "mv -f bin/bootwright.cdx.json.tmp bin/bootwright.cdx.json")
	notices := strings.Index(recipe, "./licenses notices ")
	executable := strings.Index(recipe, "mv -f bin/bootwright.tmp bin/bootwright")
	if bom < 0 || notices < bom || executable < strings.Index(recipe, "mv -f bin/bootwright.notices.txt.tmp") || !strings.Contains(recipe, "-o bin/bootwright.tmp ") || !strings.Contains(recipe, `-sbom "$(CURDIR)/bin/bootwright.cdx.json"`) || !strings.Contains(recipe, "mv -f bin/bootwright.notices.txt.tmp bin/bootwright.notices.txt") {
		t.Errorf("the build recipe does not compose the third-party notices from the bill of materials it just wrote:\n%s", recipe)
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
