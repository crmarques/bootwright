package workflow

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/remedy"
)

func assertClusterInstallRemedy(t *testing.T, err error, action remedy.Action, cluster string) remedy.Request {
	t.Helper()
	var remedial remedy.Error
	if !errors.As(err, &remedial) {
		t.Fatalf("error %T does not carry typed remedy metadata: %v", err, err)
	}
	request := remedial.Remedy()
	if request.Action != action {
		t.Fatalf("remedy action = %q, want %q", request.Action, action)
	}
	if len(request.Targets) != 1 || request.Targets[0].Role != remedy.TargetRoleContainerCluster || request.Targets[0].Name != cluster {
		t.Fatalf("remedy targets = %#v, want ContainerCluster/%s", request.Targets, cluster)
	}
	return request
}

func TestStaleAgentISOGatesCarryTypedNarrowRemediesWithoutArgv(t *testing.T) {
	const cluster = "ocp"
	tasks := []ApplyTask{{Entry: TaskLedgerEntry{Cluster: cluster, Kind: ApplyTaskKindNodeBoot}}}
	tests := []struct {
		name      string
		record    ClusterInstallRecord
		found     bool
		matches   bool
		action    remedy.Action
		condition ClusterInstallCondition
	}{
		{name: "missing record", found: false, matches: false, action: remedy.ActionRegenerateClusterISO, condition: ClusterInstallConditionMissingISORecord},
		{name: "preboot input drift", record: ClusterInstallRecord{Status: ClusterInstallStatusInstalling, Phase: ClusterInstallPhaseISOCreated}, found: true, matches: false, action: remedy.ActionRegenerateClusterISO, condition: ClusterInstallConditionISOInputDrift},
		{name: "postboot input drift", record: ClusterInstallRecord{Status: ClusterInstallStatusFailed, Phase: ClusterInstallPhaseNodesBooted}, found: true, matches: false, action: remedy.ActionRebuildCluster, condition: ClusterInstallConditionPostBootInputDrift},
		{name: "incomplete ISO", record: ClusterInstallRecord{Status: ClusterInstallStatusInstalling, Phase: ClusterInstallPhaseCreatingISO}, found: true, matches: true, action: remedy.ActionRegenerateClusterISO, condition: ClusterInstallConditionISOIncomplete},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := guardStaleAgentISOBoot(tasks, cluster, tc.record, tc.found, tc.matches)
			assertClusterInstallRemedy(t, err, tc.action, cluster)
			var stateErr *ClusterInstallStateError
			if !errors.As(err, &stateErr) || stateErr.Condition != tc.condition {
				t.Fatalf("condition = %v, want %q", err, tc.condition)
			}
			assertInstallErrorHasNoArgv(t, err)
		})
	}
}

func TestExpiredPublishedAgentISOCarriesTypedEvidenceWithoutArgv(t *testing.T) {
	now := time.Date(2026, 8, 9, 18, 0, 0, 0, time.UTC)
	err := guardPublishedAgentISOFresh(ClusterInstallRecord{UpdatedAt: now.Add(-25 * time.Hour)}, "ocp", now)
	assertClusterInstallRemedy(t, err, remedy.ActionRegenerateClusterISO, "ocp")
	var ageErr *ClusterInstallISOAgeError
	if !errors.As(err, &ageErr) || ageErr.PublishedAt != now.Add(-25*time.Hour) || ageErr.ObservedAt != now || ageErr.FreshWindow != publishedAgentISOFreshWindow {
		t.Fatalf("expired ISO error lost typed age evidence: %#v", ageErr)
	}
	assertInstallErrorHasNoArgv(t, err)
}

func TestInstallWorkflowSourceNeverEmbedsStateChangeArgv(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "install") || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"bootwright apply", "bootwright destroy"} {
			if strings.Contains(string(source), forbidden) {
				t.Fatalf("%s embeds state-changing argv %q; return an internal/converge/remedy action and let internal/cli render the resolved invocation", name, forbidden)
			}
		}
	}
}

func TestUnreadableLegacyInstallEvidenceCarriesTypedRebuild(t *testing.T) {
	const cluster = "ocp"
	record := ClusterInstallRecord{
		Cluster:    cluster,
		HashSchema: ConvergeHashSchema - 1,
		Status:     ClusterInstallStatusInstalled,
		Phase:      ClusterInstallPhaseComplete,
		RunID:      "missing-run",
	}
	task := &ApplyTask{Entry: TaskLedgerEntry{ID: "wait.ocp", Kind: ApplyTaskKindInstallWait, Cluster: cluster}}
	_, _, err := clusterInstallRecordInputsMatch(t.TempDir(), record, cluster, task, "desired", "structural", []byte("input"))
	assertClusterInstallRemedy(t, err, remedy.ActionRebuildCluster, cluster)
	var stateErr *ClusterInstallStateError
	if !errors.As(err, &stateErr) || stateErr.Condition != ClusterInstallConditionLegacyInstallEvidenceUnreadable || !errors.Is(err, stateErr.Cause) {
		t.Fatalf("legacy evidence error lost typed condition or cause: %#v", stateErr)
	}
	assertInstallErrorHasNoArgv(t, err)
}

func TestMissingPostSuccessInstallRecordCarriesTypedRebuild(t *testing.T) {
	const cluster = "ocp"
	task := ApplyTask{
		Entry: TaskLedgerEntry{Kind: ApplyTaskKindInstallWait, Cluster: cluster},
		State: v1alpha1.State{ContainerClusters: []v1alpha1.ContainerCluster{{Metadata: v1alpha1.Metadata{Name: cluster}}}},
	}
	err := ClusterInstallPostSuccessError(t.TempDir(), task)
	assertClusterInstallRemedy(t, err, remedy.ActionRebuildCluster, cluster)
	var stateErr *ClusterInstallStateError
	if !errors.As(err, &stateErr) || stateErr.Condition != ClusterInstallConditionMissingPostSuccessRecord || !strings.HasSuffix(stateErr.RecordPath, ClusterInstallRecordFileName) {
		t.Fatalf("missing-record error lost typed evidence: %#v", stateErr)
	}
	assertInstallErrorHasNoArgv(t, err)
}

func assertInstallErrorHasNoArgv(t *testing.T, err error) {
	t.Helper()
	for _, forbidden := range []string{"bootwright apply", "bootwright destroy"} {
		if strings.Contains(err.Error(), forbidden) {
			t.Fatalf("workflow error contains executable argv %q: %v", forbidden, err)
		}
	}
}
