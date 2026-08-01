package desiredstate

import (
	"reflect"
	"testing"
)

const strayExcludedYAML = `apiVersion: bootwright.io/v1alpha1
kind: CustomPlaybook
metadata: { name: stray-play }
spec: {}
---
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata: { name: stray-secret }
spec: {}
`

const excludedAddonYAML = `apiVersion: bootwright.io/v1alpha1
kind: ClusterAddon
metadata: { name: extra-addon }
spec: {}
`

func baselineResourceSelectionFiles() map[string]string {
	files := newBaselineFiles()
	files["environment.yaml"] = newEnvironmentYAMLWithResources(
		"service-machines.yaml", "network.yaml", "provider.yaml", "infra-component.yaml", "cluster.yaml")
	return files
}

func TestValidateReportsFilesEnvironmentResourcesExclude(t *testing.T) {
	dir := t.TempDir()
	files := baselineResourceSelectionFiles()
	files["add-ons/extra.yaml"] = excludedAddonYAML
	files["stray.yaml"] = strayExcludedYAML
	writeFiles(t, dir, files)

	_, exclusions, err := LoadNormalizeValidateWithExclusions([]string{dir})
	if err != nil {
		t.Fatalf("LoadNormalizeValidateWithExclusions: %v", err)
	}
	if exclusions.Empty() {
		t.Fatal("exclusions with excluded input files report Empty")
	}
	want := []ExcludedResourceFile{
		{
			Environment: "env",
			Path:        "add-ons/extra.yaml",
			Objects:     []string{"ClusterAddon/extra-addon"},
			LoadPath:    "add-ons",
		},
		{
			Environment: "env",
			Path:        "stray.yaml",
			Objects:     []string{"CustomPlaybook/stray-play", "Secret/stray-secret"},
			LoadPath:    "stray.yaml",
		},
	}
	if !reflect.DeepEqual(exclusions.Resources, want) {
		t.Fatalf("excluded resource files = %+v, want %+v", exclusions.Resources, want)
	}
	wantWarnings := []string{
		`Environment/env spec.resources excludes add-ons/extra.yaml (ClusterAddon/extra-addon); remove the path from spec.resources or add "add-ons" to load it`,
		`Environment/env spec.resources excludes stray.yaml (CustomPlaybook/stray-play, Secret/stray-secret); remove the path from spec.resources or add "stray.yaml" to load it`,
	}
	for i, excluded := range exclusions.Resources {
		if got := excluded.Warning(); got != wantWarnings[i] {
			t.Errorf("warning[%d] = %q, want %q", i, got, wantWarnings[i])
		}
	}
}

func TestExcludedResourceFilesAreReportedInPathOrder(t *testing.T) {
	dir := t.TempDir()
	files := baselineResourceSelectionFiles()
	files["zulu.yaml"] = excludedAddonYAML
	files["alpha.yaml"] = strayExcludedYAML
	files["mid/nested.yaml"] = excludedAddonYAML
	writeFiles(t, dir, files)

	_, exclusions, err := LoadNormalizeValidateWithExclusions([]string{dir})
	if err != nil {
		t.Fatalf("LoadNormalizeValidateWithExclusions: %v", err)
	}
	var paths []string
	for _, excluded := range exclusions.Resources {
		paths = append(paths, excluded.Path)
	}
	want := []string{"alpha.yaml", "mid/nested.yaml", "zulu.yaml"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("excluded paths = %v, want %v", paths, want)
	}
}

func TestNoResourceExclusionWithoutAnEnvironmentResourceList(t *testing.T) {
	dir := t.TempDir()
	files := newBaselineFiles()
	files["extras/spare-secret.yaml"] = secretDoc("spare-secret", "token")
	writeFiles(t, dir, files)

	_, exclusions, err := LoadNormalizeValidateWithExclusions([]string{dir})
	if err != nil {
		t.Fatalf("LoadNormalizeValidateWithExclusions: %v", err)
	}
	if len(exclusions.Resources) != 0 {
		t.Fatalf("omitted spec.resources excludes nothing, got %+v", exclusions.Resources)
	}
}

func TestExcludedFileWithoutAuthoredObjectsIsNotReported(t *testing.T) {
	dir := t.TempDir()
	files := baselineResourceSelectionFiles()
	files["notes.yaml"] = "some: value\nother: [1, 2]\n"
	writeFiles(t, dir, files)

	_, exclusions, err := LoadNormalizeValidateWithExclusions([]string{dir})
	if err != nil {
		t.Fatalf("LoadNormalizeValidateWithExclusions: %v", err)
	}
	if len(exclusions.Resources) != 0 {
		t.Fatalf("a YAML file with no authored objects must not be reported, got %+v", exclusions.Resources)
	}
}
