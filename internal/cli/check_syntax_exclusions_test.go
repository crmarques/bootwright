package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/state/desired"
)

func excludedResourceFixture() desiredstate.ClusterSelectionExclusions {
	return desiredstate.ClusterSelectionExclusions{
		Resources: []desiredstate.ExcludedResourceFile{
			{
				Environment: "env",
				Path:        "add-ons/extra.yaml",
				Objects:     []string{"ClusterAddon/extra-addon"},
				LoadPath:    "add-ons",
			},
		},
	}
}

func TestValidateWarnsAboutFilesResourcesExclude(t *testing.T) {
	checks := environmentSelectionChecks(excludedResourceFixture())
	if len(checks) != 1 {
		t.Fatalf("environmentSelectionChecks = %d checks, want 1", len(checks))
	}
	check := checks[0]
	if check.Name != "add-ons/extra.yaml" {
		t.Errorf("check name = %q, want the excluded file path", check.Name)
	}
	if !strings.Contains(check.Evidence, "Environment/env spec.resources") {
		t.Errorf("check evidence = %q, want the owning Environment and field", check.Evidence)
	}
	if !strings.Contains(check.Evidence, "ClusterAddon/extra-addon") {
		t.Errorf("check evidence = %q, want the objects the excluded file declares", check.Evidence)
	}
	if !strings.Contains(check.Remediation, `add "add-ons" to load it`) {
		t.Errorf("check remediation = %q, want the path to add", check.Remediation)
	}
}

func TestSyntaxCheckJSONCarriesExcludedResourceFiles(t *testing.T) {
	var stdout bytes.Buffer
	if err := writeSyntaxCheckJSON(&stdout, v1alpha1.State{}, excludedResourceFixture(), nil); err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &wire); err != nil {
		t.Fatalf("decode json: %v\n%s", err, stdout.String())
	}
	entries, ok := wire["excludedResourceFiles"].([]any)
	if !ok || len(entries) != 1 {
		t.Fatalf("excludedResourceFiles = %v, want one entry", wire["excludedResourceFiles"])
	}
	entry, ok := entries[0].(map[string]any)
	if !ok {
		t.Fatalf("excludedResourceFiles[0] = %v, want an object", entries[0])
	}
	for _, key := range []string{"environment", "path", "objects", "loadPath"} {
		if _, present := entry[key]; !present {
			t.Errorf("excludedResourceFiles[0] has no %q key: %v", key, entry)
		}
	}
	for _, key := range []string{"Environment", "Path", "Objects", "LoadPath"} {
		if _, present := entry[key]; present {
			t.Errorf("excludedResourceFiles[0] carries PascalCase key %q; the report is lowerCamelCase", key)
		}
	}
	if entry["path"] != "add-ons/extra.yaml" {
		t.Errorf("excluded path = %v, want add-ons/extra.yaml", entry["path"])
	}
}
