package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func TestPlaybookDryRunUsesGenericLabel(t *testing.T) {
	tasks := []workflow.ApplyTask{{
		Entry:    workflow.TaskLedgerEntry{Kind: workflow.ApplyTaskKindPlaybook, Label: "playbook os-customizations (after machines)"},
		Limit:    "ceph-prd",
		Playbook: "/some/path/site.yml",
	}}

	buf := &bytes.Buffer{}
	printPlaybookDryRun(buf, tasks)
	out := buf.String()
	if !strings.Contains(out, "Custom playbook os-customizations (after machines)") {
		t.Fatalf("dry-run playbook label not normalized:\n%s", out)
	}
	if strings.Contains(out, "[PENDING] playbook os-customizations") {
		t.Fatalf("raw playbook label still leaked:\n%s", out)
	}
}
