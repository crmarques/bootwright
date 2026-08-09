package cli

import (
	"encoding/json"
	"reflect"
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
	if err := appendMutatingInvocationExtraVars(&plan, invocation, "/dev/disk/by-id/osd one,/dev/sdc"); err != nil {
		t.Fatal(err)
	}
	values := invocationExtraVarValues(t, plan)
	for _, name := range []string{
		converge.MutatingInvocationExtraVar,
		converge.ApplyReconcileInvocationExtraVar,
		converge.ApplyRebuildInvocationExtraVar,
		converge.ApplyReclaimInvocationExtraVar,
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
