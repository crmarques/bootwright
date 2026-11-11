package lifecycle

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

type provedClientReader struct {
	*testCapability
}

func (provedClientReader) ProvedClients(_ reconciliation.Block, proved BlockEvidence) []ProvedClient {
	var clients []ProvedClient
	if json.Unmarshal(proved.Evidence, &clients) != nil {
		return nil
	}
	return clients
}

func appliedClientsHarness(t *testing.T, helm string) *harness {
	t.Helper()
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{
		stagedDefinition("controller-prerequisites", reconciliation.StageController),
		stagedDefinition("artifacts", reconciliation.StageInfrastructureServices),
	})
	h.service.capabilities = testResolver{capability: provedClientReader{testCapability: h.capability}}
	proof, err := json.Marshal([]ProvedClient{{Kind: "helm", Version: helm}, {Kind: "openshift-clients", Version: "4.21.15"}})
	if err != nil {
		t.Fatal(err)
	}
	h.capability.outcomeFor = map[string]Result{
		"controller-prerequisites": {Outcome: reconciliation.OutcomeChanged, Evidence: proof},
	}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	return h
}

func helmMinimum(t *testing.T) string {
	t.Helper()
	pin, found, err := controller.PinOf("helm")
	if err != nil || !found {
		t.Fatalf("the tool table pins no helm: %v", err)
	}
	return pin.Minimum
}

func TestStatusAndDiffReportAClientTheCompletedApplyProvedBelowTheMinimum(t *testing.T) {
	h := appliedClientsHarness(t, "3.14.0")
	minimum := helmMinimum(t)
	status, err := h.service.Status(context.Background(), StatusRequest{ContextName: testContextName})
	if err != nil {
		t.Fatal(err)
	}
	diff, err := h.service.Diff(context.Background(), DiffRequest{ContextName: testContextName})
	if err != nil {
		t.Fatal(err)
	}
	apply := status.Lifecycle.Operation
	want := OutdatedClient{Kind: "helm", Release: "3.14.0", Minimum: minimum, Apply: apply}
	for name, reported := range map[string][]OutdatedClient{"status": status.OutdatedClients, "diff": diff.OutdatedClients} {
		if len(reported) != 1 || reported[0] != want {
			t.Fatalf("%s reports %+v, want %+v", name, reported, want)
		}
	}
	for name, warnings := range map[string][]string{"status": codes(status.Warnings), "diff": codes(diff.Warnings)} {
		if len(warnings) != 1 || warnings[0] != "controller.outdated-client" {
			t.Fatalf("%s warns %v", name, warnings)
		}
	}
	warning := status.Warnings[0]
	if warning.Severity != "warning" || !strings.Contains(warning.Message, "helm 3.14.0") || !strings.Contains(warning.Message, minimum) ||
		warning.Remediation != "run bootwright destroy --context "+testContextName+" and then bootwright apply --context "+testContextName+" to install this build's default helm, or keep the build that applied it" {
		t.Fatalf("warning %+v", warning)
	}
	if diff.Warnings[0] != warning {
		t.Fatalf("diff warns %+v, status %+v", diff.Warnings[0], warning)
	}
}

func TestStatusAndDiffReportNothingForAClientProvedAtTheMinimum(t *testing.T) {
	h := appliedClientsHarness(t, helmMinimum(t))
	status, err := h.service.Status(context.Background(), StatusRequest{ContextName: testContextName})
	if err != nil {
		t.Fatal(err)
	}
	diff, err := h.service.Diff(context.Background(), DiffRequest{ContextName: testContextName})
	if err != nil {
		t.Fatal(err)
	}
	if len(status.OutdatedClients)+len(diff.OutdatedClients)+len(status.Warnings)+len(diff.Warnings) != 0 {
		t.Fatalf("status %+v %+v, diff %+v %+v", status.OutdatedClients, status.Warnings, diff.OutdatedClients, diff.Warnings)
	}
}

func codes(reported []diagnostics.Diagnostic) []string {
	out := []string{}
	for _, diagnostic := range reported {
		out = append(out, diagnostic.Code)
	}
	return out
}
