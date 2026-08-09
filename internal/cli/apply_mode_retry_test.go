package cli

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func TestCreateStructuralRefusalEmitsARebuildThatClearsTheNamedGates(t *testing.T) {
	objects := classifyRetryObject(t, workflow.ConvergeSafetyOwner, "sha256:stale")
	preflightErr := workflow.EvaluateApplyModePreflight(workflow.ApplyModeCreate, objects)
	if preflightErr == nil {
		t.Fatal("create must refuse a structurally drifted object")
	}
	invocation := resolvedInvocation{
		verb:        invocationApply,
		contextName: "matrix",
		flags: invocationFlags{
			mode:            workflow.ApplyModeCreate,
			selection:       runSelection{stage: "clusters", clusters: "ceph"},
			authorizations:  []string{authorizeForeignDaemons},
			yes:             true,
			askBecomePass:   false,
			trustOnFirstUse: true,
		},
	}
	command := mustRetry(t, invocation, retryIntent{mode: workflow.ApplyModeRebuild, requiredAuthorizations: []string{authorizeDataLoss}})
	message := applyModePreflightRefusal(preflightErr, invocation).Error()
	if !strings.Contains(message, "`"+command.String()+"`") {
		t.Fatalf("create structural refusal must name its exact rebuild:\n%s", message)
	}
	if err := workflow.EvaluateApplyModePreflight(workflow.ApplyModeRebuild, objects); err != nil {
		t.Fatalf("emitted rebuild must clear the structural preflight: %v", err)
	}
	auth, err := parseAuthorizations([]string{authorizeForeignDaemons, authorizeDataLoss}, authorizeVerbApply)
	if err != nil {
		t.Fatal(err)
	}
	if err := destructiveOverrideYesGuard([]string{"StorageCluster/ceph"}, true, auth.has(authorizeDataLoss), invocation); err != nil {
		t.Fatalf("emitted data-loss token must clear the next destructive gate: %v", err)
	}
}

func TestCreateForeignRefusalEmitsNoBootwrightBypass(t *testing.T) {
	objects := classifyRetryObject(t, "another-manager", "sha256:foreign")
	preflightErr := workflow.EvaluateApplyModePreflight(workflow.ApplyModeCreate, objects)
	if preflightErr == nil {
		t.Fatal("create must refuse a foreign object")
	}
	invocation := resolvedInvocation{
		verb:        invocationApply,
		contextName: "matrix",
		flags: invocationFlags{
			mode:            workflow.ApplyModeCreate,
			selection:       runSelection{stage: "clusters", clusters: "ceph"},
			yes:             true,
			askBecomePass:   false,
			trustOnFirstUse: true,
		},
	}
	message := applyModePreflightRefusal(preflightErr, invocation).Error()
	for _, want := range []string{"--mode create", "another manager", "use the recorded manager", "No bootwright apply mode or authorization token adopts"} {
		if !strings.Contains(message, want) {
			t.Fatalf("create foreign refusal missing %q:\n%s", want, message)
		}
	}
	if strings.Contains(message, "re-run `bootwright apply") {
		t.Fatalf("foreign ownership has no sanctioned Bootwright bypass:\n%s", message)
	}
	for _, mode := range []workflow.ApplyMode{workflow.ApplyModeReconcile, workflow.ApplyModeRebuild} {
		if err := workflow.EvaluateApplyModePreflight(mode, objects); err == nil {
			t.Fatalf("--mode %s must not bypass foreign ownership", mode)
		}
	}
}

func classifyRetryObject(t *testing.T, owner, recordedHash string) []workflow.ObjectClassification {
	t.Helper()
	task := workflow.ApplyTask{Entry: workflow.TaskLedgerEntry{ID: "storage.ceph", Kind: workflow.ApplyTaskKindStorageCluster, Label: "storage.ceph", Cluster: "ceph"}}
	runsDir := t.TempDir()
	if err := workflow.SaveConvergeSafetyRecord(runsDir, workflow.ConvergeSafetyRecord{
		APIVersion:   workflow.ConvergeSafetyAPIVersion,
		ResourceID:   workflow.ApplyTaskKindStorageCluster + "/storage.ceph",
		ResourceKind: workflow.ApplyTaskKindStorageCluster,
		TaskID:       task.Entry.ID,
		TaskKind:     task.Entry.Kind,
		DesiredHash:  recordedHash,
		HashSchema:   workflow.ConvergeHashSchema,
		Owner:        workflow.ConvergeSafetyOwnerIdentity{Manager: owner},
	}); err != nil {
		t.Fatal(err)
	}
	objects, err := workflow.ClassifyApplyObjects([]workflow.ApplyTask{task}, runsDir)
	if err != nil {
		t.Fatal(err)
	}
	return objects
}
