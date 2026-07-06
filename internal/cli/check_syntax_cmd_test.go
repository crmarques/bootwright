package cli

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/state/desired"
)

func TestSyntaxCheckJSONIncludesDiagnostics(t *testing.T) {
	checkErr := desiredstate.Validate(v1alpha1.State{})
	if checkErr == nil {
		t.Fatal("expected empty state to fail validation")
	}
	var stdout bytes.Buffer
	if err := writeSyntaxCheckJSON(&stdout, v1alpha1.State{}, desiredstate.ClusterSelectionExclusions{}, checkErr); err != nil {
		t.Fatal(err)
	}
	var report syntaxCheckReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode json: %v\n%s", err, stdout.String())
	}
	if report.OK || report.Error == "" || len(report.Diagnostics) == 0 {
		t.Fatalf("expected diagnostics in failure report: %+v", report)
	}
	if report.ExitCode != 1 {
		t.Fatalf("failure report exitCode = %d, want 1", report.ExitCode)
	}
}

func TestSyntaxCheckJSONCountsProvisioningPlaybooks(t *testing.T) {
	state := v1alpha1.State{
		ProvisioningPlaybooks: []v1alpha1.ProvisioningPlaybook{
			{Metadata: v1alpha1.Metadata{Name: "post-install"}},
		},
	}
	var stdout bytes.Buffer
	if err := writeSyntaxCheckJSON(&stdout, state, desiredstate.ClusterSelectionExclusions{}, nil); err != nil {
		t.Fatal(err)
	}
	var report syntaxCheckReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode json: %v\n%s", err, stdout.String())
	}
	if report.ProvisioningPlaybooks != 1 {
		t.Fatalf("provisioningPlaybooks count = %d, want 1 (must not be omitted from the Objects summary)", report.ProvisioningPlaybooks)
	}
	if report.ExitCode != 0 {
		t.Fatalf("valid report exitCode = %d, want 0", report.ExitCode)
	}
}
