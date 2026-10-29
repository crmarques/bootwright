package diagnostics

import (
	"regexp"
	"strings"
	"testing"
)

var contextBackedCommand = regexp.MustCompile(`bootwright (status|plan|apply|destroy|diff|secret|machine|cluster)\b`)

func TestEveryDefaultNamingAContextBackedCommandCarriesTheContext(t *testing.T) {
	named := 0
	for _, code := range DefaultRemediationCodes() {
		remediation := DefaultRemediation(code)
		if !contextBackedCommand.MatchString(remediation) {
			continue
		}
		named++
		if !strings.Contains(remediation, "--context "+ContextPlaceholder) {
			t.Errorf("%s: the default %q names a context-backed command without --context %s", code, remediation, ContextPlaceholder)
		}
	}
	if named == 0 {
		t.Fatal("no default names a context-backed command, so the check proves nothing")
	}
}

func TestADefaultRemediationNamesTheInvokingContext(t *testing.T) {
	failure := Diagnostic{Severity: "error", Code: "lifecycle.state", Message: "m"}
	if got, want := WithRemediationFor(failure, "lab").Remediation, "run bootwright status --context lab to see the records and the next step"; got != want {
		t.Fatalf("remediation = %q, want %q", got, want)
	}
	if got := WithRemediation(failure).Remediation; !strings.Contains(got, "--context "+ContextPlaceholder) {
		t.Fatalf("a remediation with no invoking context = %q", got)
	}
	own := Diagnostic{Severity: "error", Code: "lifecycle.state", Message: "m", Remediation: "run bootwright status --context <context>"}
	if got := WithRemediationFor(own, "lab").Remediation; got != own.Remediation {
		t.Fatalf("an emitter's own remediation became %q", got)
	}
	free := Diagnostic{Severity: "error", Code: "context.state", Message: "m"}
	if got, want := WithRemediationFor(free, "lab").Remediation, "run bootwright context list to see the contexts"; got != want {
		t.Fatalf("a context-free default = %q, want %q", got, want)
	}
}
