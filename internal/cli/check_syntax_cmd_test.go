package cli

import (
	"bytes"
	"encoding/json"
	"reflect"
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

func TestSyntaxCheckJSONCountsPlaybooks(t *testing.T) {
	state := v1alpha1.State{
		CustomPlaybooks: []v1alpha1.CustomPlaybook{
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
	if report.CustomPlaybooks != 1 {
		t.Fatalf("provisioningPlaybooks count = %d, want 1 (must not be omitted from the Objects summary)", report.CustomPlaybooks)
	}
	if report.ExitCode != 0 {
		t.Fatalf("valid report exitCode = %d, want 0", report.ExitCode)
	}
}

func TestStateCensusCoversEveryAuthoredKind(t *testing.T) {
	accessors := v1alpha1.AuthoredKindAccessors()
	censusType := reflect.TypeOf(stateCensus{})
	if censusType.NumField() != len(accessors) {
		t.Fatalf("stateCensus has %d keys, want one per authored kind (%d)", censusType.NumField(), len(accessors))
	}
	state := stateWithOneOfEveryAuthoredKind(t)
	census := reflect.ValueOf(newStateCensus(state))
	for i, accessor := range accessors {
		field := censusType.Field(i)
		if field.Name != accessor.StateField {
			t.Fatalf("stateCensus key %d is %q, want %q", i, field.Name, accessor.StateField)
		}
		if got := census.Field(i).Int(); got != 1 {
			t.Fatalf("stateCensus.%s = %d, want 1", field.Name, got)
		}
	}
	fields := stateCountFields(state)
	if len(fields) != len(accessors) {
		t.Fatalf("stateCountFields returned %d entries, want %d", len(fields), len(accessors))
	}
	for i, accessor := range accessors {
		if fields[i].Key != accessor.StateField || fields[i].Value != "1" {
			t.Fatalf("stateCountFields[%d] = %+v, want %s=1", i, fields[i], accessor.StateField)
		}
	}
}

func TestSyntaxCheckJSONCountsEntitlementsAndSecrets(t *testing.T) {
	state := v1alpha1.State{
		Entitlements: []v1alpha1.Entitlement{{Metadata: v1alpha1.Metadata{Name: "rhel"}}},
		Secrets:      []v1alpha1.Secret{{Metadata: v1alpha1.Metadata{Name: "pull-secret"}}},
	}
	var stdout bytes.Buffer
	if err := writeSyntaxCheckJSON(&stdout, state, desiredstate.ClusterSelectionExclusions{}, nil); err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("decode json: %v\n%s", err, stdout.String())
	}
	for _, key := range []string{"entitlements", "secrets"} {
		if decoded[key] != float64(1) {
			t.Fatalf("validate --output json %q = %v, want 1:\n%s", key, decoded[key], stdout.String())
		}
	}
}

func stateWithOneOfEveryAuthoredKind(t *testing.T) v1alpha1.State {
	t.Helper()
	var state v1alpha1.State
	target := reflect.ValueOf(&state).Elem()
	for _, accessor := range v1alpha1.AuthoredKindAccessors() {
		field := target.FieldByName(accessor.StateField)
		if !field.IsValid() || field.Kind() != reflect.Slice {
			t.Fatalf("State has no slice field %q for kind %s", accessor.StateField, accessor.Kind)
		}
		field.Set(reflect.MakeSlice(field.Type(), 1, 1))
	}
	return state
}
