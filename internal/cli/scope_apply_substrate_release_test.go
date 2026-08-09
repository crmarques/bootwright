package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func TestInstalledContainerClusterMachineReleaseRequiresWholeClusterSequence(t *testing.T) {
	runsDir := t.TempDir()
	clustersDir := t.TempDir()
	if err := workflow.SaveClusterInstallRecord(clustersDir, workflow.ClusterInstallRecord{
		Cluster: "ocp", Status: workflow.ClusterInstallStatusInstalled,
	}); err != nil {
		t.Fatalf("save installed record: %v", err)
	}
	if err := workflow.MarkSubstrateMachinesReleased(runsDir, "ocp", []string{"m0"}, time.Now()); err != nil {
		t.Fatalf("mark release: %v", err)
	}
	tasks := []workflow.ApplyTask{{Entry: workflow.TaskLedgerEntry{
		ID: "infra.ocp", Kind: workflow.ApplyTaskKindMachineInfraPrepare, Cluster: "ocp",
	}}}
	releases, err := workflow.ConsumableSubstrateReleases(runsDir, tasks)
	if err != nil {
		t.Fatalf("load consumable releases: %v", err)
	}
	refusal := installedContainerClusterMachineReleaseRefusal(clustersDir, releases)
	if refusal == nil {
		t.Fatal("installed ContainerCluster machine release must fail closed")
	}
	message := refusal.Error()
	for _, want := range []string{
		"bootwright destroy --clusters ocp --yes",
		"bootwright apply --clusters ocp --authorize data-loss --yes",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("refusal %q missing executable remedy %q", message, want)
		}
	}
	for _, wrong := range []string{
		"destroy --clusters ocp --authorize data-loss",
		"apply --clusters ocp --yes",
	} {
		if strings.Contains(message, wrong) {
			t.Fatalf("refusal puts data-loss authorization on the wrong command: %q", message)
		}
	}
	if got, err := workflow.ReleasedSubstrateClusters(runsDir); err != nil || strings.Join(got, ",") != "ocp" {
		t.Fatalf("refusal must retain release: clusters=%v err=%v", got, err)
	}

	if err := workflow.RemoveClusterInstallState(clustersDir, "test", "ocp"); err != nil {
		t.Fatalf("remove install state after whole-cluster destroy: %v", err)
	}
	if err := installedContainerClusterMachineReleaseRefusal(clustersDir, releases); err != nil {
		t.Fatalf("whole-cluster destroy must make the release consumable by a fresh apply: %v", err)
	}
	if err := workflow.ConsumeSubstrateRelease(runsDir, "ocp", []string{"m0"}, []string{"m0"}); err != nil {
		t.Fatalf("consume release after successful rebuild work: %v", err)
	}
	if got, err := workflow.ReleasedSubstrateClusters(runsDir); err != nil || len(got) != 0 {
		t.Fatalf("successful rebuild must clear release exactly once: clusters=%v err=%v", got, err)
	}
}
