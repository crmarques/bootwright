package main

import (
	"os"
	"regexp"
	"runtime/debug"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/crmarques/bootwright/internal/cli"
)

type imageScanWorkflow struct {
	Jobs map[string]struct {
		If              string            `yaml:"if"`
		ContinueOnError bool              `yaml:"continue-on-error"`
		Env             map[string]string `yaml:"env"`
		Steps           []struct {
			Run string `yaml:"run"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

func TestTheScheduledImageScanTakesItsImagesFromTheBillOfMaterialsAndPinsItsScanner(t *testing.T) {
	data, err := os.ReadFile("../../.github/workflows/vulnerability.yml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow imageScanWorkflow
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatal(err)
	}
	job, found := workflow.Jobs["images"]
	if !found {
		t.Fatal("the vulnerability workflow has no images job")
	}
	if !strings.Contains(job.If, "schedule") || strings.Contains(job.If, "push") || !job.ContinueOnError {
		t.Errorf("the image scan runs on %q or can fail a build", job.If)
	}
	if !regexp.MustCompile(`^[a-z0-9.-]+(/[a-z0-9._-]+)+:[0-9][0-9.]*@sha256:[0-9a-f]{64}$`).MatchString(job.Env["SCANNER"]) {
		t.Errorf("the scanner %q is not pinned by tag and digest", job.Env["SCANNER"])
	}
	script := ""
	for _, step := range job.Steps {
		script += step.Run + "\n"
	}
	for _, required := range []string{"bootwright sbom --format cyclonedx-json", `select(.type == "container")`, `"$SCANNER" image`, "--exit-code 0", `if ! docker run`, "continue", "::error::"} {
		if !strings.Contains(script, required) {
			t.Errorf("the image scan does not run %q", required)
		}
	}
	stamp, _ := debug.ReadBuildInfo()
	bom, err := composeBillOfMaterials(cli.BuildInfo{}, stamp)
	if err != nil {
		t.Fatal(err)
	}
	var scanned []string
	for _, component := range bom.Components {
		if component.Type == "container" {
			scanned = append(scanned, component.Name+"@"+component.Version)
		}
	}
	var pinned []string
	for _, image := range pinnedImages() {
		pinned = append(pinned, image.reference)
	}
	slices.Sort(scanned)
	slices.Sort(pinned)
	if !slices.Equal(slices.Compact(pinned), scanned) {
		t.Errorf("the images the scan reads from the bill of materials are %v, not the pinned %v", scanned, pinned)
	}
}
