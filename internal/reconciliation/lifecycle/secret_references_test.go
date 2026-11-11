package lifecycle

import (
	"context"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

type unnamingCapability struct{ Capability }

type overbindingCapability struct {
	*testCapability
	extra string
}

func (c overbindingCapability) Plan(ctx context.Context, input PlanInput) (CapabilityPlan, error) {
	plan, err := c.testCapability.Plan(ctx, input)
	plan.Secrets = append(plan.Secrets, c.extra)
	return plan, err
}

func TestACapabilityThatBindsASecretNoBlockNamesRefusesToPlan(t *testing.T) {
	named := &testCapability{definitions: []reconciliation.BlockDefinition{definition("alpha"), definition("beta")}, secrets: []string{"artifact-server-tls"}}
	for name, tc := range map[string]struct {
		capability Capability
		want       string
	}{
		"a Secret no block names":     {overbindingCapability{testCapability: named, extra: "unnamed-key"}, "binds Secret unnamed-key, which none of its blocks (alpha and others) names"},
		"a capability naming nothing": {unnamingCapability{named}, "binds Secret artifact-server-tls but cannot name the Secrets its blocks reference"},
	} {
		t.Run(name, func(t *testing.T) {
			plan, err := tc.capability.Plan(context.Background(), PlanInput{})
			if err != nil {
				t.Fatal(err)
			}
			issues := diagnostics.Of(refuseUnnamedSecrets("artifact-server-nginx-v1", tc.capability, plan))
			if len(issues) != 1 || issues[0].Code != "lifecycle.state" || !strings.Contains(issues[0].Message, "the artifact-server-nginx-v1 capability "+tc.want) || !strings.Contains(issues[0].Remediation, "this build is defective") {
				t.Fatalf("refusal = %+v", issues)
			}
		})
	}
	plan, err := named.Plan(context.Background(), PlanInput{})
	if err != nil {
		t.Fatal(err)
	}
	if err := refuseUnnamedSecrets("artifact-server-nginx-v1", named, plan); err != nil {
		t.Fatalf("a capability naming every Secret it binds refuses: %v", diagnostics.Of(err))
	}
}

func TestPlanningRefusesACapabilityThatBindsASecretNoBlockNames(t *testing.T) {
	h := newHarness(t, "alpha")
	if _, err := h.service.Plan(context.Background(), PlanRequest{ContextName: testContextName}); err != nil {
		t.Fatalf("a capability naming what it binds does not plan: %v", diagnostics.Of(err))
	}
	h.service.capabilities = testResolver{capability: overbindingCapability{testCapability: h.capability, extra: "unnamed-key"}}
	_, err := h.service.Plan(context.Background(), PlanRequest{ContextName: testContextName})
	if issues := diagnostics.Of(err); len(issues) != 1 || !strings.Contains(issues[0].Message, "binds Secret unnamed-key, which none of its blocks") {
		t.Fatalf("plan = %+v", issues)
	}
}
