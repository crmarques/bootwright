package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/status"
)

func TestDestroyRunFrameListsHighLevelTeardownPhasesOnly(t *testing.T) {
	ledger := workflow.NewRunLedger("destroy-test", "destroy", "", workflow.ConcurrencyLimits{}, []workflow.TaskLedgerEntry{
		{ID: "destroy.storage-clusters", Kind: workflow.DestroyTaskKindStorageCluster, Label: "Storage clusters", ResourceKeys: []string{"ceph-prd-01"}, Status: workflow.TaskStatusOK},
		{ID: "destroy.machine-registration", Kind: workflow.DestroyTaskKindMachineRegistration, Label: "Machine registration", Status: workflow.TaskStatusOK},
		{ID: "destroy.infra-components", Kind: workflow.DestroyTaskKindInfraComponents, Label: "Infra component services", Status: workflow.TaskStatusOK},
		{ID: "destroy.machine-infra.ocp-prd-01", Kind: workflow.DestroyTaskKindMachineInfra, Label: "Machine infrastructure ocp-prd-01", ResourceKeys: []string{"ocp-prd-01", "machine:node-01", "machine:node-02"}, Status: workflow.TaskStatusRunning},
		{ID: "destroy.container-clusters", Kind: workflow.DestroyTaskKindContainerCluster, Label: "Container clusters", ResourceKeys: []string{"ocp-prd-01", "ocp-prd-02"}, Status: workflow.TaskStatusPending},
		{ID: "destroy.provider-services", Kind: workflow.DestroyTaskKindProviderServices, Label: "Provider services", Status: workflow.TaskStatusPending},
		{ID: "destroy.storage-node-access", Kind: workflow.DestroyTaskKindStorageNodeAccess, Label: "Storage node access", ResourceKeys: []string{"ceph-prd-01"}, Status: workflow.TaskStatusPending},
	}, time.Now())

	frame := destroyRunFrame(ledger)
	if frame.BarLabel != "Teardown" || frame.Total != 4 {
		t.Fatalf("bar = %q total = %d, want Teardown/4 (machine registration, node access and per-service playbooks fold into their phase)", frame.BarLabel, frame.Total)
	}
	if len(frame.Groups) != 1 {
		t.Fatalf("want one flat group: %+v", frame.Groups)
	}
	steps := frame.Groups[0].Steps
	got := make([]string, 0, len(steps))
	for _, step := range steps {
		got = append(got, step.Label)
	}
	want := []string{status.PhaseStorageClusters, status.PhaseMachines, status.PhaseSharedServices, status.PhaseContainerClusters}
	if len(got) != len(want) {
		t.Fatalf("steps = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("steps = %v, want %v", got, want)
		}
	}
	if steps[0].Status != output.StatusDone || steps[1].Status != output.StatusRunning || steps[3].Status != output.StatusPending {
		t.Fatalf("phase statuses = %+v", steps)
	}
	if steps[1].Detail != "ocp-prd-01, ceph-prd-01" {
		t.Fatalf("machines detail = %q; the row that actually destroys nodes must name the clusters it is tearing down", steps[1].Detail)
	}
	if strings.Contains(steps[1].Detail, workflow.DestroyMachineResourceKeyPrefix) {
		t.Fatalf("machines detail = %q; per-machine ownership keys are not cluster names", steps[1].Detail)
	}
	if steps[3].Detail != "ocp-prd-01, ocp-prd-02" {
		t.Fatalf("container clusters detail = %q", steps[3].Detail)
	}
}

func TestDestroyOrphanHintScopedToSweepCoverage(t *testing.T) {
	var buf bytes.Buffer
	printDestroyOrphans(&buf, []workflow.UndeclaredResource{
		{Kind: "libvirt-domain", Name: "vm-a"},
		{Kind: "bmc-emulator", Name: "bmc-a"},
		{Kind: "infra-component", Name: "InfraComponent-artifacts"},
		{Kind: "kubevirt-machine", Name: "vm-b"},
		{Kind: "storage-cluster", Name: "ceph-old"},
	})
	out := buf.String()
	for _, line := range []string{"libvirt-domain/vm-a", "bmc-emulator/bmc-a", "infra-component/InfraComponent-artifacts"} {
		if !strings.Contains(out, line) {
			t.Fatalf("missing orphan %q:\n%s", line, out)
		}
	}
	if strings.Count(out, "a full-context destroy reclaims it") != 3 {
		t.Fatalf("libvirt-domain, bmc-emulator and infra-component are swept from their ownership records, so each must promise reclaim:\n%s", out)
	}
	for _, line := range []string{"kubevirt-machine/vm-b", "storage-cluster/ceph-old"} {
		if !strings.Contains(out, line) {
			t.Fatalf("missing orphan %q:\n%s", line, out)
		}
	}
	if strings.Count(out, "does not reclaim this record") != 2 {
		t.Fatalf("kubevirt and storage orphans should each get the not-reclaimed hint:\n%s", out)
	}
}

func TestUnscopedFullDestroyYesDisclosureIsExact(t *testing.T) {
	var buf bytes.Buffer
	printUnscopedFullDestroyDisclosure(&buf, true, runSelection{}, true)
	for _, want := range []string{"destroy scope", "whole context", "full lifecycle", "confirmation skipped by --yes"} {
		if !strings.Contains(buf.String(), want) {
			t.Fatalf("scope disclosure missing %q:\n%s", want, buf.String())
		}
	}
	for _, tc := range []struct {
		full      bool
		selection runSelection
		yes       bool
	}{
		{full: false, yes: true},
		{full: true, selection: runSelection{clusters: "ocp"}, yes: true},
		{full: true, yes: false},
	} {
		buf.Reset()
		printUnscopedFullDestroyDisclosure(&buf, tc.full, tc.selection, tc.yes)
		if buf.Len() != 0 {
			t.Fatalf("nonmatching scope printed disclosure: %q", buf.String())
		}
	}
}

func TestDestroyOutputNamesCoveredClusters(t *testing.T) {
	now := time.Now()
	ledger := workflow.NewRunLedger("destroy-test", "clusters destroy", "", workflow.ConcurrencyLimits{}, []workflow.TaskLedgerEntry{
		{ID: "destroy.storage-clusters", Kind: workflow.DestroyTaskKindStorageCluster, Label: "Storage clusters", ResourceKeys: []string{"ceph-storage"}, Status: workflow.TaskStatusOK},
		{ID: "destroy.container-clusters", Kind: workflow.DestroyTaskKindContainerCluster, Label: "Container clusters", ResourceKeys: []string{"dc1-metal-ocp", "dc1-child-ocp"}, Status: workflow.TaskStatusFailed, Dependencies: []string{"destroy.storage-clusters"}, Failure: "failure: agent removal failed"},
	}, now)

	frame := destroyRunFrame(ledger)
	if got := frame.Groups[0].Steps[1].Detail; !strings.Contains(got, "dc1-metal-ocp, dc1-child-ocp") {
		t.Fatalf("failed step detail = %q, want covered cluster names", got)
	}

	var buf bytes.Buffer
	printDestroyRunSummary(&buf, "/runs", ledger)
	if !strings.Contains(buf.String(), "clusters: dc1-metal-ocp, dc1-child-ocp") {
		t.Fatalf("summary missing covered clusters:\n%s", buf.String())
	}
}

func TestDestroyRunSummaryPrintsRunLogAndFailure(t *testing.T) {
	now := time.Now()
	ledger := workflow.NewRunLedger("destroy-test", "infra destroy", "", workflow.ConcurrencyLimits{}, []workflow.TaskLedgerEntry{
		{ID: "destroy.machine-infra", Kind: workflow.DestroyTaskKindMachineInfra, Label: "Machine infrastructure", Status: workflow.TaskStatusPending},
	}, now)
	ledger.MarkRunning("destroy.machine-infra", "/runs/log", now)
	ledger.MarkFailed("destroy.machine-infra", "failure: libvirt domain still running", now)
	ledger.Finish(workflow.RunStatusFailed, now)

	var buf bytes.Buffer
	printDestroyRunSummary(&buf, "/runs", ledger)
	out := buf.String()
	for _, want := range []string{"[FAILED] infra destroy", "Run log", "Machine infrastructure", "libvirt domain still running"} {
		if !strings.Contains(out, want) {
			t.Fatalf("summary missing %q:\n%s", want, out)
		}
	}
}

func TestDestroyRunSummaryOmitsUnwrittenTaskLog(t *testing.T) {
	now := time.Now()
	taskLog := filepath.Join(t.TempDir(), "ansible-output.log")
	ledger := workflow.NewRunLedger("destroy-test", "destroy", "", workflow.ConcurrencyLimits{}, []workflow.TaskLedgerEntry{
		{ID: "destroy.machine-infra", Kind: workflow.DestroyTaskKindMachineInfra, Label: "Machine infrastructure", Status: workflow.TaskStatusPending},
	}, now)
	ledger.MarkRunning("destroy.machine-infra", taskLog, now)
	ledger.MarkFailed("destroy.machine-infra", "failure: host cluster kubeconfig is missing", now)
	ledger.Finish(workflow.RunStatusFailed, now)

	var buf bytes.Buffer
	printDestroyRunSummary(&buf, "/runs", ledger)
	if strings.Contains(buf.String(), taskLog) {
		t.Fatalf("a failure raised before Ansible ran must not point at an unwritten task log:\n%s", buf.String())
	}

	if err := os.WriteFile(taskLog, []byte("TASK [assert] failed\n"), 0o600); err != nil {
		t.Fatalf("write task log: %v", err)
	}
	buf.Reset()
	printDestroyRunSummary(&buf, "/runs", ledger)
	if !strings.Contains(buf.String(), taskLog) {
		t.Fatalf("a written task log must still be surfaced:\n%s", buf.String())
	}
}
