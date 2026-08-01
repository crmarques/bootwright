package cli

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	cliout "github.com/crmarques/bootwright/internal/cli/output"
)

func TestScopedPreflightJSONReportsExecutedCheckResults(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")
	stdout, stderr, code := runCLI(t, "preflight", "infra", "--output", "json")
	if code != 1 {
		t.Fatalf("preflight infra --output json exit = %d, want 1 (the test context has no secret material)\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("a machine-readable preflight must report on stdout only, stderr=%q", stderr)
	}
	var report preflightResultReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode preflight report: %v\n%s", err, stdout)
	}
	if report.OK {
		t.Fatalf("a failing preflight must report ok=false: %s", stdout)
	}
	if report.Target != "infra" {
		t.Fatalf("report target = %q, want infra", report.Target)
	}
	if report.Summary.Total != len(report.Checks) || report.Summary.Total == 0 {
		t.Fatalf("summary total %d does not count the %d emitted check(s)", report.Summary.Total, len(report.Checks))
	}
	if report.Summary.Failed == 0 {
		t.Fatalf("a failing preflight must count its failures: %s", stdout)
	}
	failed := 0
	for _, check := range report.Checks {
		if check.Name == "" || check.Status == "" {
			t.Fatalf("every check must carry a name and a status: %+v", check)
		}
		if check.Status != strings.ToLower(check.Status) {
			t.Fatalf("check status %q must be machine-readable lowercase", check.Status)
		}
		if check.Status != preflightResultFail {
			continue
		}
		failed++
		if check.Detail == "" || check.Fix == "" {
			t.Fatalf("a failed check must carry the evidence and the fix the text output prints: %+v", check)
		}
	}
	if failed != report.Summary.Failed {
		t.Fatalf("summary counts %d failure(s), checks carry %d", report.Summary.Failed, failed)
	}
}

func TestScopedPreflightJSONDryRunStillDescribesThePlan(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")
	stdout, stderr, code := runCLI(t, "preflight", "infra", "--output", "json", "--dry-run")
	if code != 0 {
		t.Fatalf("preflight infra --output json --dry-run exited %d, stderr=%q", code, stderr)
	}
	var report scopeDryRunReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode dry-run report: %v\n%s", err, stdout)
	}
	if report.Action != "preflight" || len(report.Command) == 0 {
		t.Fatalf("--dry-run with json must still return the planned command graph: %s", stdout)
	}
	if strings.Contains(stdout, `"summary"`) {
		t.Fatalf("the plan report must not claim executed results: %s", stdout)
	}
}

func TestPreflightCheckResultsMirrorTheTextFields(t *testing.T) {
	results := preflightCheckResults([]preflightCheck{
		okCheck("Controller tools", "ansible-playbook", "/usr/bin/ansible-playbook"),
		failCheck("Secret material", "openshift-pull-secret", "not set", "the install cannot pull images", "bootwright secret set --name openshift-pull-secret --pull-secret <path>"),
		warnCheck("SSH host trust", "provider-01", "no record", "the run may stop on an unknown key", "bootwright machine trust"),
		{Group: "Machines", Name: "worker-0", Status: cliout.StatusSkipped},
	})
	want := []preflightCheckResult{{
		Name:   "ansible-playbook",
		Target: "Controller tools",
		Status: "ok",
		Detail: "/usr/bin/ansible-playbook",
	}, {
		Name:   "openshift-pull-secret",
		Target: "Secret material",
		Status: "fail",
		Detail: "not set",
		Impact: "the install cannot pull images",
		Fix:    "bootwright secret set --name openshift-pull-secret --pull-secret <path>",
	}, {
		Name:   "provider-01",
		Target: "SSH host trust",
		Status: "warn",
		Detail: "no record",
		Impact: "the run may stop on an unknown key",
		Fix:    "bootwright machine trust",
	}, {
		Name:   "worker-0",
		Target: "Machines",
		Status: "skip",
	}}
	if len(results) != len(want) {
		t.Fatalf("mapped %d result(s), want %d", len(results), len(want))
	}
	for i, result := range results {
		if result != want[i] {
			t.Fatalf("result %d = %+v, want %+v", i, result, want[i])
		}
	}
	if got := preflightFailedResults(results); got != 1 {
		t.Fatalf("failed count = %d, want 1; only a failed check gates the exit code", got)
	}
}

func TestAnsiblePreflightResultReportsTheRunOutcome(t *testing.T) {
	passed := ansiblePreflightResult("infra", "/runs/preflight-infra.log", nil)
	if passed.Status != "ok" || passed.Fix != "" {
		t.Fatalf("a passing Ansible preflight = %+v", passed)
	}
	failed := ansiblePreflightResult("infra", "/runs/preflight-infra.log", errors.New("ansible-playbook exited 2"))
	if failed.Status != preflightResultFail {
		t.Fatalf("a failing Ansible preflight must report status fail: %+v", failed)
	}
	if !strings.Contains(failed.Detail, "exited 2") {
		t.Fatalf("a failing Ansible preflight must carry the run error: %+v", failed)
	}
	if !strings.Contains(failed.Fix, "/runs/preflight-infra.log") {
		t.Fatalf("a failing Ansible preflight must point at its log: %+v", failed)
	}
}
