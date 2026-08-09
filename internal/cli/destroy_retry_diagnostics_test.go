package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/converge"
)

func TestPostDestroyRecordCleanupNamesTheExactRetry(t *testing.T) {
	retry := retryCommand{args: []string{
		"bootwright", "destroy",
		"--authorize", "protected,data-loss",
		"--yes",
		"--stage", "infra",
		"--machines", "worker-0,worker-1",
		"--purge-history",
		"--context", "prod context",
	}}
	var out bytes.Buffer
	err := printConvergeRecordResetProblems(&out, []error{errors.New("remove runs/safety/machine/worker-0.json: permission denied")}, retry)
	if err == nil {
		t.Fatal("record cleanup failure unexpectedly returned nil")
	}
	command := "bootwright destroy --authorize protected,data-loss --yes --stage infra --machines worker-0,worker-1 --purge-history --context 'prod context'"
	for _, text := range []string{out.String(), err.Error()} {
		if !strings.Contains(text, "`"+command+"`") {
			t.Fatalf("record cleanup diagnostic lost exact retry %q: %s", command, text)
		}
		if strings.Contains(text, "re-run destroy") || strings.Contains(text, "re-run bootwright destroy") {
			t.Fatalf("record cleanup diagnostic retained a scope-widening generic retry: %s", text)
		}
	}
}

func TestPartialDestroyWarningNamesTheExactRetry(t *testing.T) {
	retry := retryCommand{args: []string{
		"bootwright", "destroy",
		"--authorize", "data-loss",
		"--yes",
		"--clusters", "ceph-prd",
		"--context", "prod",
	}}
	partial := converge.PartialStorageDestroy{
		Recorded: []string{"ceph-prd"},
		Reasons:  []string{"ceph-prd-osd-2: connection timed out"},
	}
	var out bytes.Buffer
	printPartialStorageDestroyWarning(&out, partial, errors.New("write partial state: permission denied"), retry)
	command := "`bootwright destroy --authorize data-loss --yes --clusters ceph-prd --context prod`"
	if count := strings.Count(out.String(), command); count != 2 {
		t.Fatalf("partial destroy warning must use the same exact retry for teardown and evidence repair, got %d in %q", count, out.String())
	}
	for _, want := range []string{"ceph-prd-osd-2", "connection timed out", "keeps serving", "Repair the reported context state"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("partial destroy warning missing %q: %s", want, out.String())
		}
	}
}
