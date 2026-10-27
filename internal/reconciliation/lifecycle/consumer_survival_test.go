package lifecycle

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

type consumingTestCapability struct {
	*retiringTestCapability
	inputs   map[string][]ProducedInput
	unread   map[string]bool
	outputs  map[string][]string
	homes    map[string]reconciliation.ObjectRef
	nodes    map[string][]string
	admitted bool
}

func (c *consumingTestCapability) ProducedInputs(block reconciliation.Block) ([]ProducedInput, error) {
	if c.unread[block.ID] {
		return nil, errors.New("unreadable produced inputs")
	}
	return c.inputs[block.ID], nil
}

func (c *consumingTestCapability) ProducedOutputs(block reconciliation.Block) ([]string, error) {
	return c.outputs[block.ID], nil
}

func (c *consumingTestCapability) HostObject(block reconciliation.Block) (reconciliation.ObjectRef, error) {
	return c.homes[block.ID], nil
}

func (c *consumingTestCapability) NodeMachines(block reconciliation.Block) ([]string, error) {
	return c.nodes[block.ID], nil
}

// Quiescent admits every block when admitted is set, so a refusal the test then
// sees is the engine's own, whatever the capability answers.
func (c *consumingTestCapability) Quiescent(ctx context.Context, probe Probe) (Quiescence, error) {
	if c.admitted {
		c.probed = append(c.probed, probe)
		return Quiescence{State: Quiescent}, nil
	}
	return c.retiringTestCapability.Quiescent(ctx, probe)
}

type deletingMachine struct {
	machineBinding
	deletes map[string]bool
}

func (m deletingMachine) DeletesMachine(block reconciliation.Block) bool { return m.deletes[block.ID] }

func consumerDefinitions() []reconciliation.BlockDefinition {
	return []reconciliation.BlockDefinition{
		stagedDefinition("host", reconciliation.StageSubstrates),
		onMachine(stagedDefinition("machine", reconciliation.StageMachines, "host"), "rhel-01"),
		onMachine(stagedDefinition("installation", reconciliation.StageClusters, "machine"), "rhel-01"),
		stagedDefinition("core", reconciliation.StageClusters, "installation"),
		stagedDefinition("storage", reconciliation.StageClusters, "core"),
		onMachine(stagedDefinition("node", reconciliation.StageMachines, "host"), "ocp-01"),
		stagedDefinition("cluster", reconciliation.StageClusters, "node"),
		stagedDefinition("consumer", reconciliation.StageClusters, "cluster", "storage"),
	}
}

func consumerHarness(t *testing.T) (*harness, *consumingTestCapability, deletingMachine) {
	t.Helper()
	h := newPlannedHarness(t, consumerDefinitions())
	c := &consumingTestCapability{
		retiringTestCapability: &retiringTestCapability{
			onlineTestCapability: &onlineTestCapability{testCapability: h.capability, online: map[string]bool{"storage": true}},
			hosts:                map[string][]string{"core": {"rhel-01"}, "storage": {"rhel-01"}},
			removes:              map[string]bool{"machine": true, "node": true},
		},
	}
	machines := deletingMachine{machineBinding{c.retiringTestCapability}, map[string]bool{"machine": true, "node": true}}
	h.service.capabilities = boundResolver{
		{Kind: "ArtifactServer", Implementation: "artifact-server-nginx-v1"}: c,
		{Kind: "Machine", Implementation: "artifact-server-nginx-v1"}:        machines,
	}
	return h, c, machines
}

func consumed(c *consumingTestCapability) {
	c.inputs = map[string][]ProducedInput{"consumer": {{Kind: "ArtifactServer", Object: "storage", Output: "details"}}}
	c.outputs = map[string][]string{"storage": {"details"}}
	c.homes = map[string]reconciliation.ObjectRef{"consumer": {Kind: "ArtifactServer", Object: "cluster"}}
	c.nodes = map[string][]string{"cluster": {"ocp-01"}}
}

// arrangedPreview shows the fresh apply's plan with the consumption arranged,
// then applies with none, since the test capability publishes no output. A
// plan whose consumer inputs cannot be read refuses, so it warns of nothing.
func arrangedPreview(t *testing.T, h *harness, c *consumingTestCapability, arrange func(*consumingTestCapability, deletingMachine), machines deletingMachine) ([]diagnostics.Diagnostic, error) {
	t.Helper()
	consumed(c)
	arrange(c, machines)
	preview, err := h.service.Plan(context.Background(), PlanRequest{ContextName: testContextName})
	if err == nil && preview.Verb != string(reconciliation.Apply) {
		t.Fatalf("apply preview = %+v", preview)
	}
	c.inputs, c.unread = nil, nil
	completeApply(t, h)
	consumed(c)
	arrange(c, machines)
	if err != nil {
		return nil, err
	}
	return preview.Warnings, nil
}

// The specs/ceph-resources.md export refusal row "Removing an export while
// a consuming container cluster survives the removal": virtual storage hosts
// and a consumer that outlives the destroy refuse before registration, though
// the capability admits every block, and the apply plan warns of exactly that
// consumer.
func TestRetirementRefusesBeforeRegistrationWhileAConsumerSurvives(t *testing.T) {
	for name, tc := range map[string]struct {
		arrange  func(*consumingTestCapability, deletingMachine)
		consumer string
	}{
		"a consumer node the removal keeps": {func(c *consumingTestCapability, m deletingMachine) {
			c.removes["node"], m.deletes["node"] = false, false
		}, "ArtifactServer/cluster"},
		"a consumer node the removal erases without deleting": {func(_ *consumingTestCapability, m deletingMachine) { m.deletes["node"] = false }, "ArtifactServer/cluster"},
		"a consumer home without nodes":                       {func(c *consumingTestCapability, _ deletingMachine) { delete(c.nodes, "cluster") }, "ArtifactServer/cluster"},
		"a consumer naming no home":                           {func(c *consumingTestCapability, _ deletingMachine) { delete(c.homes, "consumer") }, "ArtifactServer/consumer"},
		"a consumer whose inputs cannot be read":              {func(c *consumingTestCapability, _ deletingMachine) { c.unread = map[string]bool{"consumer": true} }, "ArtifactServer/consumer"},
	} {
		t.Run(name, func(t *testing.T) {
			h, capability, machines := consumerHarness(t)
			warnings, err := arrangedPreview(t, h, capability, tc.arrange, machines)
			pair := tc.consumer + " consumes ArtifactServer/storage"
			if unread := capability.unread["consumer"]; unread != (err != nil) || !unread && !slices.ContainsFunc(warnings, func(d diagnostics.Diagnostic) bool {
				return d.Severity == "warning" && d.Code == "lifecycle.surviving-consumer" && strings.Contains(d.Message, pair+" and outlives")
			}) {
				t.Fatalf("apply plan warnings = %+v (%v), want %s", warnings, err, pair)
			}
			before, _ := durableOperation(t, h)
			capability.admitted = true
			_, err = destroy(h)
			after, _ := durableOperation(t, h)
			if firstCode(err) != "lifecycle.live" || len(h.capability.destroys) != 0 || after.ID != before.ID {
				t.Fatalf("surviving consumer admitted retirement: %v (%v)", h.capability.destroys, err)
			}
			reported := diagnostics.Of(err)[0]
			_, retired, _ := strings.Cut(reported.Message, " takes back ")
			for _, id := range []string{"storage", "core"} {
				if !strings.Contains(retired, "ArtifactServer/"+id) {
					t.Fatalf("refusal = %+v, want %s named", reported, id)
				}
			}
			if !strings.Contains(reported.Message, pair+" and survives this destroy, which takes back ") || reported.Remediation != survivingConsumerRemedy {
				t.Fatalf("refusal = %+v, want %s", reported, pair)
			}
			if len(capability.probed) != 0 {
				t.Fatalf("a surviving consumer still probed %d blocks", len(capability.probed))
			}
		})
	}
}

func TestRetirementProceedsWhenTheConsumerLeavesWithItsNodes(t *testing.T) {
	h, capability, machines := consumerHarness(t)
	if warnings, err := arrangedPreview(t, h, capability, func(*consumingTestCapability, deletingMachine) {}, machines); err != nil || len(warnings) != 0 {
		t.Fatalf("a leaving consumer warned: %+v (%v)", warnings, err)
	}
	result, err := destroy(h)
	if err != nil || result.Receipt.State != string(reconciliation.OperationDone) {
		t.Fatalf("virtual consumer removal = %+v (%v)", result, err)
	}
	if flagged := retiredProbes(capability.retiringTestCapability); !flagged["storage"] || !flagged["core"] {
		t.Fatalf("retirement flag = %v, want storage and core retired", flagged)
	}
	if !slices.Contains(h.capability.destroys, "node") || !slices.Contains(h.capability.destroys, "storage") {
		t.Fatalf("removal = %v", h.capability.destroys)
	}
}

// A leaving consumer whose node Machine waits behind an online effect keeps
// its provider online: the hosts' stop proof cannot stand in for a consumer
// still running when the provider is removed.
func TestALeavingConsumerWhoseNodesWaitUnretiresItsProvider(t *testing.T) {
	h, capability, _ := consumerHarness(t)
	completeApply(t, h)
	consumed(capability)
	capability.online["cluster"] = true
	_, err := destroy(h)
	if firstCode(err) != "lifecycle.live" || len(h.capability.destroys) != 0 {
		t.Fatalf("deferred consumer nodes admitted retirement: %v (%v)", h.capability.destroys, err)
	}
	if reported := diagnostics.Of(err)[0]; !strings.Contains(reported.Message, "ArtifactServer/storage (its hosts outlive this removal)") || strings.Contains(reported.Message, "survives this destroy") {
		t.Fatalf("refusal = %+v, want the unretired storage named", reported)
	}
	if flagged := retiredProbes(capability.retiringTestCapability); flagged["storage"] || flagged["core"] {
		t.Fatalf("retirement flag = %v, want storage and core online", flagged)
	}
}

// A consumer whose block started but never completed is still taken back by
// the removal, so it still makes the removal refuse while it survives.
func TestAConsumerThatNeverCompletedStillRefusesRetirement(t *testing.T) {
	h, capability, machines := consumerHarness(t)
	h.capability.outcomeFor = map[string]Result{"consumer": {Outcome: reconciliation.OutcomeFailed}}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
		t.Fatal("the failing consumer completed its apply")
	}
	consumed(capability)
	machines.deletes["node"] = false
	capability.admitted = true
	_, err := destroy(h)
	if firstCode(err) != "lifecycle.live" || !strings.Contains(diagnostics.Of(err)[0].Message, "ArtifactServer/cluster consumes ArtifactServer/storage and survives this destroy") || len(h.capability.destroys) != 0 {
		t.Fatalf("a failed surviving consumer admitted retirement: %v (%v)", h.capability.destroys, diagnostics.Of(err))
	}
}
