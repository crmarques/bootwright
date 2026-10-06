package cli

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

// A service that refuses how it was invoked, as secret set refuses a flag set
// its Secret's type does not take, fails as a usage error: its own typed
// diagnostic, then the concise help, and exit 2. Every other service refusal
// still exits 1.
func TestServiceUsageRefusalExitsTwoWithConciseHelp(t *testing.T) {
	refusal := diagnostics.Diagnostic{
		Severity: "error", Code: "secret.input", Message: "Secret pull is of type dockerConfigJson; secret set takes --value-file <path> or --value-stdin",
		Object:      &diagnostics.ObjectIdentity{APIVersion: "bootwright.io/v1alpha1", Kind: "Secret", Name: "pull"},
		Remediation: "bootwright secret set --context lab --name pull --value-file <path>",
	}
	args := []string{"secret", "set", "--name", "pull", "--certificate-file", "pull.pem"}
	record := &dispatchRecord{err: &diagnostics.Failure{Diagnostics: []diagnostics.Diagnostic{refusal}, Usage: true}}
	code, out, errOut := runSecretResult(args, record)
	want := "[FAIL] secret.input: Secret pull is of type dockerConfigJson; secret set takes --value-file <path> or --value-stdin [Secret/pull]; next: bootwright secret set --context lab --name pull --value-file <path>\n" +
		"Usage: bootwright secret set [flags]\nRun 'bootwright help' for available commands.\n"
	if code != 2 || out != "" || errOut != want || record.calls != 1 {
		t.Fatalf("usage refusal code=%d stdout=%q stderr=%q calls=%d", code, out, errOut, record.calls)
	}
	record = &dispatchRecord{err: &diagnostics.Failure{Diagnostics: []diagnostics.Diagnostic{refusal}}}
	code, out, errOut = runSecretResult(args, record)
	if code != 1 || out != "" || strings.Contains(errOut, "Usage:") || !strings.HasPrefix(errOut, "[FAIL] secret.input: ") {
		t.Fatalf("operational refusal code=%d stdout=%q stderr=%q", code, out, errOut)
	}
}

// The alternatives no Secret type takes together refuse before the context is
// read; every other flag set reaches the service, which knows the type.
func TestSecretSetAlternativeConflictsRefuseBeforeTheService(t *testing.T) {
	for _, test := range []struct{ args, message string }{
		{"secret set --name pull --value-file pull.json --value-stdin", "--value-file conflicts with --value-stdin"},
		{"secret set --name bmc --username admin --password-file password --password-stdin", "--password-file conflicts with --password-stdin"},
		{"secret set --name bmc --value-stdin --password-stdin", "--value-stdin conflicts with --password-stdin"},
	} {
		t.Run(test.args, func(t *testing.T) {
			record := &dispatchRecord{}
			code, out, errOut := runSecretResult(strings.Fields(test.args), record)
			want := "[FAIL] cli.usage: " + test.message + "\nUsage: bootwright secret set [flags]\nRun 'bootwright help' for available commands.\n"
			if code != 2 || out != "" || errOut != want || record.calls != 0 {
				t.Fatalf("code=%d stdout=%q stderr=%q calls=%d", code, out, errOut, record.calls)
			}
		})
	}
	for _, args := range []string{"secret set --name pull", "secret set --name pull --username admin", "secret set --name pull --value-file v --certificate-file c"} {
		record := &dispatchRecord{}
		if code, _, errOut := runSecretResult(strings.Fields(args), record); record.calls != 1 || strings.Contains(errOut, "cli.usage") {
			t.Fatalf("%s did not reach the service: code=%d stderr=%q", args, code, errOut)
		}
	}
}
