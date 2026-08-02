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
	var report syntaxCheckReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode json: %v\n%s", err, stdout.String())
	}
	if len(report.ExcludedResourceFiles) != 1 {
		t.Fatalf("excludedResourceFiles = %+v, want one entry", report.ExcludedResourceFiles)
	}
	if report.ExcludedResourceFiles[0].Path != "add-ons/extra.yaml" {
		t.Errorf("excluded path = %q, want add-ons/extra.yaml", report.ExcludedResourceFiles[0].Path)
	}
}
