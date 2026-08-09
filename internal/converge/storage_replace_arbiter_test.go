package converge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func writeArbiterRetirementTestResult(t *testing.T, runsDir, body string) string {
	t.Helper()
	path := arbiterRetirementPath(runsDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestArbiterRetirementResultClearsAndConsumesExactlyOnce(t *testing.T) {
	runsDir := t.TempDir()
	lease, err := workflow.AcquireCommandRunLease(context.Background(), runsDir, "replace-arbiter")
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	path := writeArbiterRetirementTestResult(t, runsDir, `{"host":"old","authorized":true,"corroborated":true,"offline":true}`)
	if err := ClearArbiterRetirement(runsDir, lease); err != nil {
		t.Fatal(err)
	}
	if _, found, err := readArbiterRetirement(runsDir); err != nil || found {
		t.Fatalf("cleared retirement result found=%v err=%v", found, err)
	}
	writeArbiterRetirementTestResult(t, runsDir, `{"host":"current","authorized":true,"corroborated":true,"offline":true}`)
	result, found, err := ConsumeArbiterRetirement(runsDir, lease)
	if err != nil || !found || result.Host != "current" || !result.Offline {
		t.Fatalf("consume current retirement = %#v found=%v err=%v", result, found, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("consumed retirement result remains at %s: %v", path, err)
	}
	if _, found, err := ConsumeArbiterRetirement(runsDir, lease); err != nil || found {
		t.Fatalf("second consume found=%v err=%v, want no stale result", found, err)
	}
}

func TestCorruptArbiterRetirementNeverBecomesAuthorizationEvidence(t *testing.T) {
	runsDir := t.TempDir()
	lease, err := workflow.AcquireCommandRunLease(context.Background(), runsDir, "replace-arbiter")
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	path := writeArbiterRetirementTestResult(t, runsDir, `{not-json`)
	result, found, err := ConsumeArbiterRetirement(runsDir, lease)
	if err == nil || found || result.Authorized || result.Offline {
		t.Fatalf("consume corrupt retirement = %#v found=%v err=%v, want fail-closed evidence", result, found, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("corrupt evidence was silently removed before the next run can explicitly clear it: %v", err)
	}
}

func TestContradictoryArbiterRetirementNeverBecomesAuthorizationEvidence(t *testing.T) {
	tests := []string{
		`{"host":"","authorized":true,"corroborated":true,"offline":true}`,
		`{"host":"old","authorized":true,"corroborated":false,"offline":true}`,
		`{"host":"old","authorized":true,"corroborated":true,"offline":false}`,
	}
	for _, body := range tests {
		t.Run(body, func(t *testing.T) {
			runsDir := t.TempDir()
			lease, err := workflow.AcquireCommandRunLease(context.Background(), runsDir, "replace-arbiter")
			if err != nil {
				t.Fatal(err)
			}
			defer lease.Close()
			path := writeArbiterRetirementTestResult(t, runsDir, body)
			result, found, err := ConsumeArbiterRetirement(runsDir, lease)
			if err == nil || found || result.Authorized || result.Corroborate || result.Offline {
				t.Fatalf("consume contradictory retirement = %#v found=%v err=%v, want fail-closed evidence", result, found, err)
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("contradictory evidence was removed before explicit next-run clear: %v", err)
			}
		})
	}
}

func TestArbiterRetirementEvidenceCannotClearOrConsumeWithoutTheLease(t *testing.T) {
	runsDir := t.TempDir()
	path := writeArbiterRetirementTestResult(t, runsDir, `{"host":"stale","authorized":true,"corroborated":true,"offline":true}`)
	if err := ClearArbiterRetirement(runsDir, nil); err == nil {
		t.Fatal("clear without a command mutation lease succeeded")
	}
	result, found, err := ConsumeArbiterRetirement(runsDir, nil)
	if err == nil || found || result.Authorized || result.Offline {
		t.Fatalf("consume without lease = %#v found=%v err=%v, want fail-closed evidence", result, found, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("lease-free operation changed retirement evidence: %v", err)
	}
}

func TestArbiterRetirementEvidenceCannotClearOrConsumeAfterLeaseClose(t *testing.T) {
	runsDir := t.TempDir()
	path := writeArbiterRetirementTestResult(t, runsDir, `{"host":"stale","authorized":true,"corroborated":true,"offline":true}`)
	lease, err := workflow.AcquireCommandRunLease(context.Background(), runsDir, "replace-arbiter")
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	if err := ClearArbiterRetirement(runsDir, lease); err == nil {
		t.Fatal("clear with a closed command mutation lease succeeded")
	}
	result, found, err := ConsumeArbiterRetirement(runsDir, lease)
	if err == nil || found || result.Authorized || result.Offline {
		t.Fatalf("consume with closed lease = %#v found=%v err=%v, want fail-closed evidence", result, found, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("closed-lease operation changed retirement evidence: %v", err)
	}
}

func TestArbiterRetirementEvidenceRequiresTheReplaceArbiterLease(t *testing.T) {
	runsDir := t.TempDir()
	path := writeArbiterRetirementTestResult(t, runsDir, `{"host":"stale","authorized":true,"corroborated":true,"offline":true}`)
	lease, err := workflow.AcquireCommandRunLease(context.Background(), runsDir, "apply")
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	if err := ClearArbiterRetirement(runsDir, lease); err == nil || !strings.Contains(err.Error(), "not replace-arbiter") {
		t.Fatalf("clear with apply lease = %v, want replace-arbiter lease refusal", err)
	}
	result, found, err := ConsumeArbiterRetirement(runsDir, lease)
	if err == nil || !strings.Contains(err.Error(), "not replace-arbiter") || found || result.Authorized || result.Offline {
		t.Fatalf("consume with apply lease = %#v found=%v err=%v, want fail-closed evidence", result, found, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("wrong-command lease changed retirement evidence: %v", err)
	}
}
