package lifecycle

import (
	"context"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

func TestCompactRemovalNamesEveryMachineToStopBeforeRemovingAnyEffect(t *testing.T) {
	names := []string{"compact-01", "compact-02", "compact-03"}
	var definitions []reconciliation.BlockDefinition
	for _, name := range names {
		block := definition("machine-" + name)
		block.Kind, block.Object = "Machine", name
		definitions = append(definitions, block)
	}
	install := dependent("cluster-install-compact", "machine-compact-01", "machine-compact-02", "machine-compact-03")
	definitions = append(definitions, install)
	h := newPlannedHarness(t, definitions)
	h.service.capabilities = sharedResolver{
		bound: []BoundCapability{
			{Binding: CapabilityBinding{Kind: "ArtifactServer", Implementation: "artifact-server-nginx-v1"}, Capability: h.capability},
			{Binding: CapabilityBinding{Kind: "Machine", Implementation: "artifact-server-nginx-v1"}, Capability: probingCapability{h.capability}},
		},
		plan: CapabilityPlan{Definitions: definitions},
	}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	h.capability.quiescence = map[string]Quiescence{}
	for _, name := range names {
		h.capability.quiescence["machine-"+name] = Quiescence{State: Live, Stop: "bootwright machine stop --context lab --name " + name}
	}
	preview, err := h.service.Plan(context.Background(), PlanRequest{ContextName: "lab"})
	if err != nil || !slices.Equal(preview.Stops, names) {
		t.Fatal("compact removal does not name every Machine under Stop first", preview, err)
	}
	request := DestroyRequest{ContextName: "lab", SkipConfirmation: true}
	if _, err := h.service.Destroy(context.Background(), request); firstCode(err) != "lifecycle.live" || len(h.capability.destroys) != 0 {
		t.Fatal("running masters did not refuse before removal", err, h.capability.destroys)
	}
	for _, name := range names {
		h.capability.quiescence["machine-"+name] = Quiescence{State: Quiescent}
	}
	if _, err := h.service.Destroy(context.Background(), request); err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if len(h.capability.destroys) != 4 || h.capability.destroys[0] != install.ID {
		t.Fatal("compact effects did not remove after every master stopped", h.capability.destroys)
	}
}
