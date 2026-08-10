package workflow

import (
	"errors"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/converge/ansible"
)

func TestConciseApplyTaskFailurePrefersFailureLine(t *testing.T) {
	const msg = "managed OS at 10.7.7.129 is Bootwright-owned but /etc/bootwright/install-marker.json does not match desired hash sha256:d04dffb4aeaee942a4e62effdc0899f4653f1ecbbc44e835a1f42a2614967e7b; rerun with --mode rebuild to rebuild it."
	runnerErr := "run /var/lib/bootwright/cache/ansible-venv/bin/ansible-playbook exited with error (output log: /var/lib/bootwright/contexts/nprd/runs/history/apply/tasks/osinstall.ceph-nprd/ansible-output.log):\n" +
		"  failed task: TASK [Refuse drifted Bootwright-owned managed OS without override] *\n" +
		"  failure: " + msg + "\n" +
		"  last 3 line(s) of output:\n    ...\n  underlying error: exit status 2"

	got := conciseApplyTaskFailure(runnerErr)
	if strings.Contains(got, "ansible-playbook exited with error") {
		t.Fatalf("reason must be the distilled msg, not the generic runner wrapper: %q", got)
	}
	if !strings.HasPrefix(got, "managed OS at 10.7.7.129 is Bootwright-owned") {
		t.Fatalf("reason must lead with the descriptive msg: %q", got)
	}
	if !strings.HasSuffix(got, "rerun with --mode rebuild to rebuild it.") {
		t.Fatalf("middle truncation must preserve the actionable tail: %q", got)
	}
}

func TestMiddleEllipsisKeepsBothEnds(t *testing.T) {
	long := strings.Repeat("A", 200) + "; rerun with --mode rebuild to rebuild it."
	got := middleEllipsis(long, 180)
	if len([]rune(got)) != 180 {
		t.Fatalf("truncated length = %d runes, want 180: %q", len([]rune(got)), got)
	}
	if !strings.HasPrefix(got, "AAAA") || !strings.Contains(got, "…") {
		t.Fatalf("must keep the head and an ellipsis: %q", got)
	}
	if !strings.HasSuffix(got, "rerun with --mode rebuild to rebuild it.") {
		t.Fatalf("must keep the actionable tail: %q", got)
	}
	if got := middleEllipsis("short reason", 180); got != "short reason" {
		t.Fatalf("short value must pass through unchanged: %q", got)
	}
}

func TestConciseApplyTaskFailurePreservesExactCephTimeoutRetry(t *testing.T) {
	const invocation = "bootwright apply --context nprd --clusters storage-a --stage storage --yes"
	err := &ansible.CephCommandTimeoutError{
		Err:            errors.New("run ansible-playbook exited with error"),
		Task:           "Apply Ceph OSD service spec",
		Host:           "storage-0",
		TimeoutSeconds: "600",
		ExitCode:       124,
		Invocation:     invocation,
		StateChanging:  true,
	}
	got := conciseApplyTaskFailure(err)
	if !strings.Contains(got, "outcome is unknown") || !strings.Contains(got, "`"+invocation+"`") {
		t.Fatalf("timeout failure lost its classification or exact retry: %q", got)
	}
	if strings.Contains(got, "ansible-playbook exited") {
		t.Fatalf("timeout failure exposed only the generic runner error: %q", got)
	}
}
