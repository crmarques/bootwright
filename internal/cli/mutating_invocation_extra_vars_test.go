package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/converge"
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
	if err := appendMutatingInvocationExtraVars(&plan, invocation); err != nil {
		t.Fatal(err)
	}
	values := invocationExtraVarValues(t, plan)
	for _, name := range []string{
		converge.MutatingInvocationExtraVar,
		converge.ApplyReconcileInvocationExtraVar,
		converge.ApplyRebuildInvocationExtraVar,
		converge.ApplyFullInvocationExtraVar,
		converge.ApplyThroughBaseInvocationExtraVar,
	} {
		if values[name] == "" {
			t.Fatalf("missing %s in %v", name, values)
		}
		for _, want := range []string{"--clusters cluster-a", "--context 'prod west'", "--ssh-id-file '/keys/operator id'", "--ssh-user 'admin user'", "--reclaim-devices '/dev/disk/by-id/osd one'", "--authorize", "unowned-devices"} {
			if !strings.Contains(values[name], want) {
				t.Fatalf("%s = %q, missing %q", name, values[name], want)
			}
		}
	}
	if strings.Contains(values[converge.ApplyFullInvocationExtraVar], "--stage") || strings.Contains(values[converge.ApplyFullInvocationExtraVar], "--through") {
		t.Fatalf("full retry retained a stage range: %q", values[converge.ApplyFullInvocationExtraVar])
	}
	if !strings.Contains(values[converge.ApplyThroughBaseInvocationExtraVar], "--through base") || strings.Contains(values[converge.ApplyThroughBaseInvocationExtraVar], "--stage") {
		t.Fatalf("through-base retry = %q", values[converge.ApplyThroughBaseInvocationExtraVar])
	}
	if !strings.Contains(values[converge.ApplyRebuildInvocationExtraVar], "--mode rebuild") || !strings.Contains(values[converge.ApplyRebuildInvocationExtraVar], "data-loss") {
		t.Fatalf("rebuild retry = %q", values[converge.ApplyRebuildInvocationExtraVar])
	}
}

func TestMutatingInvocationExtraVarsExposeOnlyExactDestroyRetry(t *testing.T) {
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
	if err := appendMutatingInvocationExtraVars(&plan, invocation); err != nil {
		t.Fatal(err)
	}
	values := invocationExtraVarValues(t, plan)
	if len(values) != 1 {
		t.Fatalf("destroy extra vars = %v", values)
	}
	got := values[converge.MutatingInvocationExtraVar]
	for _, want := range []string{"bootwright destroy", "--machines node-a", "--purge-history", "--authorize protected", "--context prod"} {
		if !strings.Contains(got, want) {
			t.Fatalf("destroy retry = %q, missing %q", got, want)
		}
	}
}

func invocationExtraVarValues(t *testing.T, plan converge.WorkflowPlan) map[string]string {
	t.Helper()
	if len(plan.ExtraVarPairs) != 1 {
		t.Fatalf("extra vars = %v", plan.ExtraVarPairs)
	}
	var values map[string]string
	if err := json.Unmarshal([]byte(plan.ExtraVarPairs[0]), &values); err != nil {
		t.Fatalf("decode extra vars: %v", err)
	}
	return values
}
