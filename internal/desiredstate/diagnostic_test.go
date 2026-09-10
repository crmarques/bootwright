package desiredstate

import (
	"reflect"
	"testing"
)

func TestNewFailureWithRemediation(t *testing.T) {
	err := NewFailureWithRemediation("test.failure", "operation cannot continue", "input", "correct the input and retry")
	want := []Diagnostic{{
		Severity:    "error",
		Code:        "test.failure",
		Message:     "operation cannot continue",
		Source:      &SourceLocation{Path: "input"},
		Remediation: "correct the input and retry",
	}}
	if got := DiagnosticsOf(err); !reflect.DeepEqual(got, want) {
		t.Fatalf("diagnostics = %#v, want %#v", got, want)
	}
}
