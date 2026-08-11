package cli

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/remedy"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func TestMutatingInvocationExtraVarsPreserveResolvedApplyIntent(t *testing.T) {
	invocation := resolvedInvocation{
		verb:                  invocationApply,
		contextName:           "prod west",
		sshIdentityFile:       "/keys/operator id",
		sshUser:               "admin user",
		sshAskSudoPassword:    true,
		sshUserForProvisioned: true,
		flags: invocationFlags{
			mode:            workflow.ApplyModeCreate,
			selection:       runSelection{stage: "clusters", clusters: "cluster-a"},
			reclaimDevices:  "/dev/disk/by-id/osd one",
			authorizations:  []string{authorizeUnownedDevices},
			yes:             true,
			askBecomePass:   true,
			trustOnFirstUse: true,
			verbose:         true,
		},
	}
	plan := converge.WorkflowPlan{}
	if err := appendMutatingInvocationExtraVars(&plan, invocation, "/dev/disk/by-id/osd one,/dev/sdc"); err != nil {
		t.Fatal(err)
	}
	values := invocationExtraVarValues(t, plan)
	for _, name := range []string{
		converge.MutatingInvocationExtraVar,
		converge.ApplyReconcileInvocationExtraVar,
		converge.ApplyRebuildInvocationExtraVar,
		converge.ApplyReclaimInvocationExtraVar,
		converge.ApplyControllerDNSInvocationExtraVar,
		converge.ApplyFullInvocationExtraVar,
		converge.ApplyThroughBaseInvocationExtraVar,
	} {
		value, _ := values[name].(string)
		if value == "" {
			t.Fatalf("missing %s in %v", name, values)
		}
		for _, want := range []string{"--clusters cluster-a", "--context 'prod west'", "--ssh-id-file '/keys/operator id'", "--ssh-user 'admin user'", "--reclaim-devices '/dev/disk/by-id/osd one'", "--authorize", "unowned-devices"} {
			if name == converge.ApplyReclaimInvocationExtraVar && strings.Contains(want, "/dev/disk/by-id/osd one") {
				want = converge.ApplyReclaimInvocationSentinel
			}
			if name == converge.ApplyFullInvocationExtraVar && strings.Contains(want, "/dev/disk/by-id/osd one") {
				continue
			}
			if !strings.Contains(value, want) {
				t.Fatalf("%s = %q, missing %q", name, value, want)
			}
		}
	}
	full := values[converge.ApplyFullInvocationExtraVar].(string)
	throughBase := values[converge.ApplyThroughBaseInvocationExtraVar].(string)
	rebuild := values[converge.ApplyRebuildInvocationExtraVar].(string)
	if strings.Contains(full, "--stage") || strings.Contains(full, "--through") {
		t.Fatalf("full retry retained a stage range: %q", values[converge.ApplyFullInvocationExtraVar])
	}
	if strings.Contains(full, "--reclaim-devices") || !strings.Contains(full, "--mode reconcile") {
		t.Fatalf("full recovery retry retained one-shot device reclamation or omitted reconcile mode: %q", full)
	}
	if !strings.Contains(throughBase, "--through base") || strings.Contains(throughBase, "--stage") {
		t.Fatalf("through-base retry = %q", values[converge.ApplyThroughBaseInvocationExtraVar])
	}
	if !strings.Contains(rebuild, "--mode rebuild") || !strings.Contains(rebuild, "data-loss") {
		t.Fatalf("rebuild retry = %q", values[converge.ApplyRebuildInvocationExtraVar])
	}
	if got := values[converge.ApplyReclaimDevicesExtraVar]; !reflect.DeepEqual(got, []any{"/dev/disk/by-id/osd one", "/dev/sdc"}) {
		t.Fatalf("preserved reclaim devices = %#v", got)
	}
}

func TestControllerNameResolutionRetryPrependsOnlyRequiredFabric(t *testing.T) {
	for _, tc := range []struct {
		name        string
		mode        workflow.ApplyMode
		selection   runSelection
		wantStage   string
		wantThrough string
		wantMode    workflow.ApplyMode
	}{
		{name: "machines partial create", mode: workflow.ApplyModeCreate, selection: runSelection{stage: converge.PhaseMachines, machines: "node-a"}, wantStage: converge.PhaseFabric, wantThrough: converge.PhaseMachines, wantMode: workflow.ApplyModeReconcile},
		{name: "machines through base rebuild", mode: workflow.ApplyModeRebuild, selection: runSelection{stage: converge.PhaseMachines, through: converge.PhaseBase, clusters: "cluster-a"}, wantStage: converge.PhaseFabric, wantThrough: converge.PhaseBase, wantMode: workflow.ApplyModeRebuild},
		{name: "infra family reconcile", mode: workflow.ApplyModeReconcile, selection: runSelection{stage: "infra", machines: "node-a"}, wantStage: "infra", wantMode: workflow.ApplyModeReconcile},
		{name: "full graph create", mode: workflow.ApplyModeCreate, selection: runSelection{clusters: "cluster-a"}, wantMode: workflow.ApplyModeReconcile},
		{name: "cluster family create", mode: workflow.ApplyModeCreate, selection: runSelection{stage: "clusters", clusters: "cluster-a"}, wantStage: converge.PhaseFabric, wantThrough: converge.PhaseAddons, wantMode: workflow.ApplyModeReconcile},
	} {
		t.Run(tc.name, func(t *testing.T) {
			invocation := resolvedInvocation{
				verb:        invocationApply,
				contextName: "prod west",
				flags: invocationFlags{
					mode:           tc.mode,
					selection:      tc.selection,
					authorizations: []string{authorizeUnownedDevices},
					yes:            true,
				},
			}
			plan := converge.WorkflowPlan{}
			if err := appendMutatingInvocationExtraVars(&plan, invocation, ""); err != nil {
				t.Fatal(err)
			}
			values := invocationExtraVarValues(t, plan)
			got := values[converge.ApplyControllerDNSInvocationExtraVar].(string)
			for _, want := range []string{"--mode " + string(tc.wantMode), "--authorize unowned-devices", "--context 'prod west'"} {
				if !strings.Contains(got, want) {
					t.Fatalf("controller DNS retry = %q, missing %q", got, want)
				}
			}
			if tc.wantStage != "" && !strings.Contains(got, "--stage "+tc.wantStage) {
				t.Fatalf("controller DNS retry = %q, missing stage %q", got, tc.wantStage)
			}
			if tc.wantThrough != "" && !strings.Contains(got, "--through "+tc.wantThrough) {
				t.Fatalf("controller DNS retry = %q, missing through %q", got, tc.wantThrough)
			}
			if tc.selection.machines != "" && !strings.Contains(got, "--machines "+tc.selection.machines) {
				t.Fatalf("controller DNS retry = %q, lost machines %q", got, tc.selection.machines)
			}
			if tc.selection.clusters != "" && !strings.Contains(got, "--clusters "+tc.selection.clusters) {
				t.Fatalf("controller DNS retry = %q, lost clusters %q", got, tc.selection.clusters)
			}
			if tc.wantThrough != converge.PhaseAddons && strings.Contains(got, "--through add-ons") {
				t.Fatalf("controller DNS retry widened beyond the original range: %q", got)
			}
		})
	}
}

func TestControllerNameResolutionTaskInvocationFactsMatchTypedRecovery(t *testing.T) {
	invocation := resolvedInvocation{
		verb:        invocationApply,
		contextName: "prod",
		flags: invocationFlags{
			mode:      workflow.ApplyModeCreate,
			selection: runSelection{stage: converge.PhaseDeps, clusters: "cluster-a"},
		},
	}
	for _, tc := range []struct {
		name       string
		action     remedy.Action
		wantRepair bool
	}{
		{name: "selected mutation", action: remedy.ActionResumeControllerDNSMutation},
		{name: "later proof", action: remedy.ActionReconcileSharedServiceThenRetrySameSelection, wantRepair: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tasks := []workflow.ApplyTask{{
				Entry: workflow.TaskLedgerEntry{ID: "controller-name-resolution.demo", Kind: workflow.ApplyTaskKindControllerNameResolution},
				FailureRemedy: remedy.Request{
					Action:  tc.action,
					Targets: []remedy.Target{{Role: remedy.TargetRoleClusterRoot, Name: "cluster-a"}},
				},
			}}
			if err := appendControllerNameResolutionInvocationExtraVars(tasks, invocation); err != nil {
				t.Fatal(err)
			}
			var values map[string]any
			if len(tasks[0].ExtraVarPairs) != 1 {
				t.Fatalf("task extra vars = %v", tasks[0].ExtraVarPairs)
			}
			if err := json.Unmarshal([]byte(tasks[0].ExtraVarPairs[0]), &values); err != nil {
				t.Fatal(err)
			}
			if values[converge.ApplyControllerDNSInvocationExtraVar] == "" {
				t.Fatalf("task recovery facts lost exact controller retry: %v", values)
			}
			_, hasRepair := values[converge.ApplyControllerDNSRepairInvocationExtraVar]
			_, hasResume := values[converge.ApplyControllerDNSResumeInvocationExtraVar]
			if hasRepair != tc.wantRepair || hasResume != tc.wantRepair {
				t.Fatalf("task recovery facts = %v, want repair/resume=%v", values, tc.wantRepair)
			}
		})
	}
}

func TestMutatingInvocationExtraVarsExposeExactDestroyRecovery(t *testing.T) {
	invocation := resolvedInvocation{
		verb:        invocationDestroy,
		contextName: "prod",
		flags: invocationFlags{
			selection:      runSelection{machines: "node-a"},
			purgeHistory:   true,
			authorizations: []string{authorizeProtected},
			yes:            true,
		},
	}
	plan := converge.WorkflowPlan{}
	if err := appendMutatingInvocationExtraVars(&plan, invocation, ""); err != nil {
		t.Fatal(err)
	}
	values := invocationExtraVarValues(t, plan)
	if len(values) != 1 {
		t.Fatalf("destroy extra vars = %v", values)
	}
	got := values[converge.MutatingInvocationExtraVar].(string)
	for _, want := range []string{"bootwright destroy", "--machines node-a", "--purge-history", "--authorize protected", "--context prod"} {
		if !strings.Contains(got, want) {
			t.Fatalf("destroy retry = %q, missing %q", got, want)
		}
	}
	if _, exists := values[converge.ApplyFullInvocationExtraVar]; exists {
		t.Fatalf("destroy published an unusable cross-verb apply recovery: %v", values)
	}
}

func TestMutatingInvocationExtraVarsExposeExactArbiterAuthorizationRetries(t *testing.T) {
	invocation := hostileArbiterInvocation()
	plan := converge.WorkflowPlan{}
	if err := appendMutatingInvocationExtraVars(&plan, invocation, ""); err != nil {
		t.Fatal(err)
	}
	values := invocationExtraVarValues(t, plan)
	intents := map[string]retryIntent{
		converge.MutatingInvocationExtraVar:           {},
		converge.ArbiterDegradedInvocationExtraVar:    {requiredAuthorizations: []string{authorizeDegradedQuorum}},
		converge.ArbiterSameSiteInvocationExtraVar:    {requiredAuthorizations: []string{authorizeSameSiteArbiter}},
		converge.ArbiterUnreachableInvocationExtraVar: {requiredAuthorizations: []string{authorizeUnreachableNodes}},
	}
	if len(values) != len(intents) {
		t.Fatalf("replace-arbiter extra vars = %v", values)
	}
	for name, intent := range intents {
		command, err := invocation.retry(intent)
		if err != nil {
			t.Fatal(err)
		}
		if got := values[name]; got != command.String() {
			t.Fatalf("%s = %#v, want exact resolved retry %q", name, got, command.String())
		}
		if strings.Contains(command.String(), "--yes") {
			t.Fatalf("%s invented --yes: %s", name, command.String())
		}
	}
}

func TestMutatingInvocationExtraVarsEncodeNoPreservedReclaimPathsAsAList(t *testing.T) {
	invocation := resolvedInvocation{
		verb:  invocationApply,
		flags: invocationFlags{mode: workflow.ApplyModeReconcile},
	}
	plan := converge.WorkflowPlan{}
	if err := appendMutatingInvocationExtraVars(&plan, invocation, ""); err != nil {
		t.Fatal(err)
	}
	values := invocationExtraVarValues(t, plan)
	if got := values[converge.ApplyReclaimDevicesExtraVar]; !reflect.DeepEqual(got, []any{}) {
		t.Fatalf("preserved reclaim devices = %#v, want an empty JSON list rather than null", got)
	}
}

func invocationExtraVarValues(t *testing.T, plan converge.WorkflowPlan) map[string]any {
	t.Helper()
	if len(plan.ExtraVarPairs) != 1 {
		t.Fatalf("extra vars = %v", plan.ExtraVarPairs)
	}
	var values map[string]any
	if err := json.Unmarshal([]byte(plan.ExtraVarPairs[0]), &values); err != nil {
		t.Fatalf("decode extra vars: %v", err)
	}
	return values
}
