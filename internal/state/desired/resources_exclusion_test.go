package desiredstate

import (
	"path/filepath"
	"reflect"
	"strings"
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

func TestEnvironmentResourcesImplicitlySelectMarkedAddonSnapshot(t *testing.T) {
	dir := t.TempDir()
	files := newBaselineFiles()
	files["environment.yaml"] = newEnvironmentYAMLWithResources(
		"service-machines.yaml", "network.yaml", "provider.yaml", "infra-component.yaml", "cluster.yaml", "binding.yaml")
	files["binding.yaml"] = `apiVersion: bootwright.io/v1alpha1
kind: ClusterAddonBinding
metadata: { name: demo-addons }
spec:
  clusterRef: sno
  addonRefs:
    - snapshot-addon
`
	files["add-ons/_store/snapshot-addon/add-on.yaml"] = `apiVersion: bootwright.io/v1alpha1
kind: ClusterAddon
metadata: { name: snapshot-addon }
spec: {}
`
	files["add-ons/_store/snapshot-addon/.bootwright-addon"] = "name=snapshot-addon\nversion=1\ncontentDigest=sha256:test\n"
	writeFiles(t, dir, files)

	discovered, err := discoverFiles([]string{dir})
	if err != nil {
		t.Fatalf("discoverFiles: %v", err)
	}
	state, exclusions, err := loadSelectedFilesReportingExclusions(discovered)
	if err != nil {
		t.Fatalf("loadSelectedFilesReportingExclusions: %v", err)
	}
	if len(state.ClusterAddons) != 1 || state.ClusterAddons[0].Metadata.Name != "snapshot-addon" {
		t.Fatalf("selected ClusterAddons = %+v, want the marked snapshot", state.ClusterAddons)
	}
	for _, exclusion := range exclusions {
		if exclusion.Path == "add-ons/_store/snapshot-addon/add-on.yaml" {
			t.Fatalf("generated add-on snapshot was reported as excluded: %+v", exclusion)
		}
	}
}

func TestEnvironmentResourcesDoNotImplicitlySelectAuthoredStoreLookalike(t *testing.T) {
	dir := t.TempDir()
	files := newBaselineFiles()
	files["environment.yaml"] = newEnvironmentYAMLWithResources(
		"service-machines.yaml", "network.yaml", "provider.yaml", "infra-component.yaml", "cluster.yaml", "binding.yaml")
	files["binding.yaml"] = `apiVersion: bootwright.io/v1alpha1
kind: ClusterAddonBinding
metadata: { name: demo-addons }
spec:
  clusterRef: sno
  addonRefs:
    - authored-lookalike
`
	files["add-ons/_store/authored-lookalike/add-on.yaml"] = `apiVersion: bootwright.io/v1alpha1
kind: ClusterAddon
metadata: { name: authored-lookalike }
spec: {}
`
	writeFiles(t, dir, files)

	discovered, err := discoverFiles([]string{dir})
	if err != nil {
		t.Fatalf("discoverFiles: %v", err)
	}
	_, _, err = loadSelectedFilesReportingExclusions(discovered)
	if err == nil || !strings.Contains(err.Error(), `spec.resources excludes ClusterAddon/authored-lookalike`) {
		t.Fatalf("unmarked authored lookalike error = %v, want normal resource-selection refusal", err)
	}
}

func TestEnvironmentResourcesSnapshotCarveoutDoesNotSelectOtherObjects(t *testing.T) {
	dir := t.TempDir()
	files := baselineResourceSelectionFiles()
	files["add-ons/_store/snapshot-addon/add-on.yaml"] = `apiVersion: bootwright.io/v1alpha1
kind: ClusterAddon
metadata: { name: snapshot-addon }
spec: {}
`
	files["add-ons/_store/snapshot-addon/secret.yaml"] = secretDoc("snapshot-secret", "token")
	files["add-ons/_store/snapshot-addon/.bootwright-addon"] = "name=snapshot-addon\nversion=1\ncontentDigest=sha256:test\n"
	writeFiles(t, dir, files)

	discovered, err := discoverFiles([]string{dir})
	if err != nil {
		t.Fatalf("discoverFiles: %v", err)
	}
	state, exclusions, err := loadSelectedFilesReportingExclusions(discovered)
	if err != nil {
		t.Fatalf("loadSelectedFilesReportingExclusions: %v", err)
	}
	if len(state.ClusterAddons) != 1 || len(state.Secrets) != 4 {
		t.Fatalf("implicit selection loaded state outside the single snapshot descriptor: addons=%d secrets=%d", len(state.ClusterAddons), len(state.Secrets))
	}
	if len(exclusions) != 1 || exclusions[0].Path != "add-ons/_store/snapshot-addon/secret.yaml" {
		t.Fatalf("snapshot sibling exclusions = %+v, want only the non-descriptor object", exclusions)
	}
}

func TestEnvironmentResourcesImplicitSnapshotRequiresExactGeneratedShape(t *testing.T) {
	cases := []struct {
		name       string
		addon      string
		markerName string
	}{
		{
			name:       "marker-name-does-not-match-directory",
			addon:      strings.Replace(excludedAddonYAML, "extra-addon", "snapshot-addon", 1),
			markerName: "different-addon",
		},
		{
			name: "descriptor-name-does-not-match-directory",
			addon: `apiVersion: bootwright.io/v1alpha1
kind: ClusterAddon
metadata: { name: different-addon }
spec: {}
`,
			markerName: "snapshot-addon",
		},
		{
			name:       "descriptor-carries-another-authored-object",
			addon:      strings.Replace(excludedAddonYAML, "extra-addon", "snapshot-addon", 1) + "---\n" + secretDoc("hidden", "token"),
			markerName: "snapshot-addon",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			files := baselineResourceSelectionFiles()
			files["add-ons/_store/snapshot-addon/add-on.yaml"] = tc.addon
			files["add-ons/_store/snapshot-addon/.bootwright-addon"] = "name=" + tc.markerName + "\nversion=1\ncontentDigest=sha256:test\n"
			writeFiles(t, dir, files)
			discovered, err := discoverFiles([]string{dir})
			if err != nil {
				t.Fatalf("discoverFiles: %v", err)
			}
			selected, _, active, err := selectResourceFiles(discovered)
			if err != nil || !active {
				t.Fatalf("selectResourceFiles active=%v err=%v", active, err)
			}
			for _, file := range selected {
				if strings.HasSuffix(filepath.ToSlash(file), "/add-ons/_store/snapshot-addon/add-on.yaml") {
					t.Fatalf("non-generated snapshot shape was selected: %s", file)
				}
			}
		})
	}
}
