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
	if err := writeSyntaxCheckJSON(&stdout, v1alpha1.State{}, checkErr); err != nil {
		t.Fatal(err)
	}
	var report syntaxCheckReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode json: %v\n%s", err, stdout.String())
	}
	if report.OK || report.Error == "" || len(report.Diagnostics) == 0 {
		t.Fatalf("expected diagnostics in failure report: %+v", report)
	}
}
