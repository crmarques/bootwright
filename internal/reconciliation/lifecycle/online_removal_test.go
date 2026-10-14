package lifecycle

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

type onlineTestCapability struct {
	*testCapability
	online  map[string]bool
	probed  []Probe
	onProbe func(Probe)
}

func (c *onlineTestCapability) RequiresOnlineRemoval(block reconciliation.Block) bool {
	return c.online[block.ID]
}

func (*onlineTestCapability) ProbesQuiescence() bool { return true }

func (c *onlineTestCapability) Quiescent(ctx context.Context, probe Probe) (Quiescence, error) {
	c.probed = append(c.probed, probe)
	if c.onProbe != nil {
		c.onProbe(probe)
	}
	return c.testCapability.Quiescent(ctx, probe)
}

func onlineHarness(t *testing.T, unrelated bool) (*harness, *onlineTestCapability) {
	t.Helper()
	definitions := []reconciliation.BlockDefinition{
		stagedDefinition("host", reconciliation.StageSubstrates),
		stagedDefinition("machine", reconciliation.StageMachines, "host"),
		stagedDefinition("installation", reconciliation.StageClusters, "machine"),
		stagedDefinition("storage", reconciliation.StageClusters, "installation"),
	}
	if unrelated {
		definitions = append(definitions, stagedDefinition("unrelated", reconciliation.StageMachines, "host"))
	}
	h := newPlannedHarness(t, definitions)
	c := &onlineTestCapability{testCapability: h.capability, online: map[string]bool{"storage": true}}
	h.service.capabilities = testResolver{capability: c}
	return h, c
}

func TestOnlineRemovalPausesBeforeMachinesAndContinuesAfterOperatorStop(t *testing.T) {
	h, capability := onlineHarness(t, false)
	h.capability.outcomeFor = map[string]Result{"storage": changedWithKubeconfig()}
	completeApply(t, h)
	applied, _ := durableOperation(t, h)
	h.capability.quiescence = map[string]Quiescence{
		"machine": {State: Live, Reason: "its domain is running", Stop: "bootwright machine stop --context lab --name machine"},
	}
	preview, err := h.service.Plan(context.Background(), PlanRequest{ContextName: testContextName})
	if err != nil || slices.Contains(preview.Stops, "machine") || !slices.Equal(preview.StopsAfterOnline, []string{"machine"}) {
		t.Fatalf("online removal preview = %+v (%v)", preview, err)
	}
	result, err := destroy(h)
	if firstCode(err) != "lifecycle.live" || result == nil || result.Receipt.State != string(reconciliation.OperationPaused) {
		t.Fatalf("online boundary = %+v (%v)", result, err)
	}
	if !slices.Equal(h.capability.destroys, []string{"storage", "installation"}) || blockState(t, h, "machine") != reconciliation.BlockPending {
		t.Fatalf("before stop removed %v; machine state %s", h.capability.destroys, blockState(t, h, "machine"))
	}
	if len(kept(h)) != 1 || !strings.Contains(diagnostics.Of(err)[0].Remediation, "bootwright machine stop --context lab --name machine") {
		t.Fatalf("boundary lost custody or stop remedy: %v (%v)", kept(h), err)
	}
	for _, probe := range capability.probed {
		if probe.ApplyOperation != applied.ID || probe.Execution().ApplyOperation != applied.ID {
			t.Fatalf("probe lost owning apply identity: %+v", probe)
		}
	}
	paused := result.Receipt.Operation
	preview, err = h.service.Plan(context.Background(), PlanRequest{ContextName: testContextName})
	if err != nil || !preview.Continuation || !slices.Equal(preview.StopsAfterOnline, []string{"machine"}) {
		t.Fatalf("paused preview = %+v (%v)", preview, err)
	}
	h.capability.quiescence["machine"] = Quiescence{State: Quiescent}
	result, err = destroy(h)
	if err != nil || result.Receipt.Operation != paused || result.Receipt.State != string(reconciliation.OperationDone) {
		t.Fatalf("after operator stop = %+v (%v)", result, err)
	}
	if !slices.Equal(h.capability.destroys, []string{"storage", "installation", "machine", "host"}) || len(kept(h)) != 0 {
		t.Fatalf("continued removal = %v, custody %v", h.capability.destroys, kept(h))
	}
}

func TestOnlineRemovalKeepsUnrelatedMachineAndClientUseAtInitialGate(t *testing.T) {
	for _, live := range []string{"unrelated", "storage"} {
		t.Run(live, func(t *testing.T) {
			h, _ := onlineHarness(t, true)
			completeApply(t, h)
			before, _ := durableOperation(t, h)
			h.capability.quiescence = map[string]Quiescence{live: {State: Live, Reason: "in use"}}
			_, err := destroy(h)
			after, _ := durableOperation(t, h)
			if firstCode(err) != "lifecycle.live" || len(h.capability.destroys) != 0 || after.ID != before.ID {
				t.Fatalf("live %s admitted removal: %v (%v)", live, h.capability.destroys, err)
			}
		})
	}
}

func TestOnlineRemovalReprovesUnknownMachineStateAtEveryContinuation(t *testing.T) {
	h, _ := onlineHarness(t, false)
	completeApply(t, h)
	h.capability.quiescence = map[string]Quiescence{"machine": {State: Unproved, Reason: "unreachable"}}
	for range 2 {
		result, err := destroy(h)
		if firstCode(err) != "lifecycle.live" || result.Receipt.State != string(reconciliation.OperationPaused) || slices.Contains(h.capability.destroys, "machine") {
			t.Fatalf("unproved Machine passed: %+v (%v)", result, err)
		}
	}
	if count := len(slices.DeleteFunc(slices.Clone(h.capability.probes), func(id string) bool { return id != "machine" })); count != 2 {
		t.Fatalf("Machine was probed %d times, want both invocations", count)
	}
}

func TestOnlineRemovalWaitsForEveryOnlineBranchBeforeMachineProof(t *testing.T) {
	h, capability := onlineHarness(t, false)
	h.capability.definitions = append(h.capability.definitions,
		stagedDefinition("machine-z", reconciliation.StageMachines, "host"),
		stagedDefinition("storage-z", reconciliation.StageClusters, "machine-z"))
	capability.online["storage-z"] = true
	h.service.options.Concurrency = 1
	completeApply(t, h)
	capability.onProbe = func(probe Probe) {
		if probe.Block.Stage == reconciliation.StageMachines && (!slices.Contains(h.capability.destroys, "storage") || !slices.Contains(h.capability.destroys, "storage-z")) {
			t.Errorf("Machine proof preceded complete online cleanup: %v", h.capability.destroys)
		}
	}
	if result, err := destroy(h); err != nil || result.Receipt.State != string(reconciliation.OperationDone) {
		t.Fatalf("two online branches = %+v (%v)", result, err)
	}
}

func TestUnknownOnlineRemovalCannotReachMachineBoundaryBeforeResolution(t *testing.T) {
	h, _ := onlineHarness(t, false)
	completeApply(t, h)
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
	result, err := destroy(h)
	if err == nil || result.Receipt.State != string(reconciliation.OperationUnknown) || slices.Contains(h.capability.probes, "machine") {
		t.Fatalf("unknown cleanup reached Machine proof: %+v, probes %v (%v)", result, h.capability.probes, err)
	}
	h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted}}
	h.capability.quiescence = map[string]Quiescence{"machine": {State: Live, Reason: "running"}}
	result, err = destroy(h)
	if firstCode(err) != "lifecycle.live" || result.Receipt.State != string(reconciliation.OperationPaused) || !slices.Equal(h.capability.destroys, []string{"storage", "installation"}) {
		t.Fatalf("resolved cleanup = %+v, removed %v (%v)", result, h.capability.destroys, err)
	}
}

func TestOwningApplyIdentitySurvivesRenameAndReplacementRemoval(t *testing.T) {
	h := newHarness(t, "service")
	completeApply(t, h)
	created, _ := durableOperation(t, h)
	if got := h.capability.executions[0].ApplyOperation; got != created.ID {
		t.Fatalf("apply identity = %q, want %s", got, created.ID)
	}
	renamed(h, "renamed")
	h.capability.executions = nil
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeFailed}}
	request := DestroyRequest{ContextName: "renamed", SkipConfirmation: true}
	if _, err := h.service.Destroy(context.Background(), request); err == nil {
		t.Fatal("seeded removal failure did not fire")
	}
	first := h.capability.executions[0].Operation
	result, err := h.service.Destroy(context.Background(), request)
	if err != nil || result.Receipt.Operation == first || result.Receipt.State != string(reconciliation.OperationDone) {
		t.Fatalf("replacement = %+v (%v)", result, err)
	}
	for _, execution := range h.capability.executions {
		if execution.ApplyOperation != created.ID || execution.Context != "renamed" {
			t.Fatalf("replacement changed native incarnation: %+v", execution)
		}
	}
}

func TestOwningApplyIdentityAlsoReachesApplyAndRemovalResolutions(t *testing.T) {
	h := newHarness(t, "service")
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
	if _, err := apply(h); err == nil {
		t.Fatal("seeded unknown apply did not fire")
	}
	created, _ := durableOperation(t, h)
	h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted}}
	if _, err := apply(h); err != nil {
		t.Fatalf("resolve apply: %v", err)
	}
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
	if _, err := destroy(h); err == nil {
		t.Fatal("seeded unknown removal did not fire")
	}
	h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted}}
	if _, err := destroy(h); err != nil {
		t.Fatalf("resolve removal: %v", err)
	}
	for _, execution := range h.capability.executions {
		if execution.ApplyOperation != created.ID {
			t.Fatalf("attempt or resolution lost original apply: %+v", execution)
		}
	}
}

const survivingHostRemedy = "remove it in the same destroy as every host it runs on"

type retiringTestCapability struct {
	*onlineTestCapability
	hosts   map[string][]string
	removes map[string]bool
}

func (c *retiringTestCapability) HostMachines(block reconciliation.Block) ([]string, error) {
	return c.hosts[block.ID], nil
}

func (c *retiringTestCapability) RemovesHost(block reconciliation.Block) bool {
	return c.removes[block.ID]
}

func (c *retiringTestCapability) Quiescent(ctx context.Context, probe Probe) (Quiescence, error) {
	if len(c.hosts[probe.Block.ID]) != 0 && !probe.RetiredWithHosts {
		c.probed = append(c.probed, probe)
		return Quiescence{State: Unproved, Reason: "its hosts outlive this removal", Remedy: survivingHostRemedy}, nil
	}
	return c.onlineTestCapability.Quiescent(ctx, probe)
}

func onMachine(definition reconciliation.BlockDefinition, machine string) reconciliation.BlockDefinition {
	definition.Kind, definition.Object = "Machine", machine
	return definition
}

func retiringHarness(t *testing.T, definitions []reconciliation.BlockDefinition, hosts []string) (*harness, *retiringTestCapability) {
	t.Helper()
	h := newPlannedHarness(t, definitions)
	c := &retiringTestCapability{
		onlineTestCapability: &onlineTestCapability{testCapability: h.capability, online: map[string]bool{"storage": true}},
		hosts:                map[string][]string{"storage": hosts},
		removes:              map[string]bool{"machine": true, "machine-z": true},
	}
	h.service.capabilities = boundResolver{
		{Kind: "ArtifactServer", Implementation: "artifact-server-nginx-v1"}: c,
		{Kind: "Machine", Implementation: "artifact-server-nginx-v1"}:        machineBinding{c},
	}
	return h, c
}

type machineBinding struct{ *retiringTestCapability }

func (machineBinding) Plan(context.Context, PlanInput) (CapabilityPlan, error) {
	return CapabilityPlan{}, nil
}

func hostedDefinitions(extra ...reconciliation.BlockDefinition) []reconciliation.BlockDefinition {
	return append([]reconciliation.BlockDefinition{
		stagedDefinition("host", reconciliation.StageSubstrates),
		onMachine(stagedDefinition("machine", reconciliation.StageMachines, "host"), "rhel-01"),
		onMachine(stagedDefinition("installation", reconciliation.StageClusters, "machine"), "rhel-01"),
		stagedDefinition("storage", reconciliation.StageClusters, "installation"),
	}, extra...)
}

func retiredProbes(c *retiringTestCapability) map[string]bool {
	flagged := map[string]bool{}
	for _, probe := range c.probed {
		flagged[probe.Block.ID] = flagged[probe.Block.ID] || probe.RetiredWithHosts
	}
	return flagged
}

func TestHostBoundEffectRetiresWithItsHostsWhichStopFirst(t *testing.T) {
	h, capability := retiringHarness(t, hostedDefinitions(), []string{"rhel-01"})
	completeApply(t, h)
	before, _ := durableOperation(t, h)
	stop := "bootwright machine stop --context lab --name rhel-01"
	h.capability.quiescence = map[string]Quiescence{"machine": {State: Live, Reason: "its domain is running", Stop: stop}}
	preview, err := h.service.Plan(context.Background(), PlanRequest{ContextName: testContextName})
	if err != nil || !slices.Contains(preview.Stops, "rhel-01") || len(preview.StopsAfterOnline) != 0 {
		t.Fatalf("retiring preview = %+v (%v)", preview, err)
	}
	_, err = destroy(h)
	after, _ := durableOperation(t, h)
	if firstCode(err) != "lifecycle.live" || len(h.capability.destroys) != 0 || after.ID != before.ID {
		t.Fatalf("running hosts admitted retirement: %v (%v)", h.capability.destroys, err)
	}
	if reported := diagnostics.Of(err)[0]; strings.Contains(reported.Message, "storage") || !strings.Contains(reported.Remediation, stop) {
		t.Fatalf("refusal = %+v, want only the host named with its stop", reported)
	}
	if flagged := retiredProbes(capability); !flagged["storage"] || flagged["machine"] || flagged["installation"] {
		t.Fatalf("retirement flag reached %v, want storage alone", flagged)
	}
	h.capability.quiescence["machine"] = Quiescence{State: Quiescent}
	result, err := destroy(h)
	if err != nil || result.Receipt.State != string(reconciliation.OperationDone) {
		t.Fatalf("after operator stop = %+v (%v)", result, err)
	}
	if !slices.Equal(h.capability.destroys, []string{"storage", "installation", "machine", "host"}) {
		t.Fatalf("retired removal = %v", h.capability.destroys)
	}
}

func TestHostBoundEffectStaysOnlineWhileAnyHostSurvives(t *testing.T) {
	for name, tc := range map[string]struct {
		definitions []reconciliation.BlockDefinition
		hosts       []string
		arrange     func(*retiringTestCapability)
	}{
		"a host outside the plan":       {hostedDefinitions(), []string{"rhel-01", "rhel-02"}, nil},
		"a host whose removal keeps it": {hostedDefinitions(), []string{"rhel-01"}, func(c *retiringTestCapability) { c.removes["machine"] = false }},
		"a host removed before the effect": {
			hostedDefinitions(onMachine(stagedDefinition("machine-z", reconciliation.StageMachines, "host"), "rhel-02")),
			[]string{"rhel-01", "rhel-02"}, nil,
		},
		"a host with only its installation": {[]reconciliation.BlockDefinition{
			stagedDefinition("host", reconciliation.StageSubstrates),
			onMachine(stagedDefinition("installation", reconciliation.StageClusters, "host"), "rhel-01"),
			stagedDefinition("storage", reconciliation.StageClusters, "installation"),
		}, []string{"rhel-01"}, nil},
		"a host deferred behind another online effect": {
			hostedDefinitions(stagedDefinition("other", reconciliation.StageClusters, "installation")),
			[]string{"rhel-01"}, func(c *retiringTestCapability) { c.online["other"] = true },
		},
	} {
		t.Run(name, func(t *testing.T) {
			h, capability := retiringHarness(t, tc.definitions, tc.hosts)
			if tc.arrange != nil {
				tc.arrange(capability)
			}
			completeApply(t, h)
			_, err := destroy(h)
			if firstCode(err) != "lifecycle.live" || len(h.capability.destroys) != 0 || !strings.Contains(diagnostics.Of(err)[0].Message, "its hosts outlive this removal") {
				t.Fatalf("surviving host admitted retirement: %v (%v)", h.capability.destroys, err)
			}
			if remedy := diagnostics.Of(err)[0].Remediation; remedy != survivingHostRemedy {
				t.Fatalf("surviving-host remedy = %q, want the capability's own %q", remedy, survivingHostRemedy)
			}
			if flagged := retiredProbes(capability); len(capability.probed) == 0 || flagged["storage"] {
				t.Fatalf("surviving host probed storage as retired: %+v", capability.probed)
			}
		})
	}
}

func TestRetiredRemovalContinuesAfterAnUnknownInverseBeforeItsHost(t *testing.T) {
	h, _ := retiringHarness(t, hostedDefinitions(), []string{"rhel-01"})
	completeApply(t, h)
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeChanged}, {Outcome: reconciliation.OutcomeUnknown}}
	result, err := destroy(h)
	if err == nil || result.Receipt.State != string(reconciliation.OperationUnknown) || !slices.Equal(h.capability.destroys, []string{"storage", "installation"}) {
		t.Fatalf("seeded lost inverse did not fire: %+v, removed %v (%v)", result, h.capability.destroys, err)
	}
	h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted}}
	result, err = destroy(h)
	if err != nil || result.Receipt.State != string(reconciliation.OperationDone) {
		t.Fatalf("continuation = %+v (%v)", result, err)
	}
	if !slices.Equal(h.capability.destroys, []string{"storage", "installation", "machine", "host"}) || len(kept(h)) != 0 {
		t.Fatalf("continued retirement = %v, custody %v", h.capability.destroys, kept(h))
	}
}

func TestRetiredRemovalFlagsOnlyItsOwnDestroyAndRemovalObservation(t *testing.T) {
	h, _ := retiringHarness(t, hostedDefinitions(), []string{"rhel-01"})
	completeApply(t, h)
	applied := len(h.capability.executions)
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
	result, err := destroy(h)
	if err == nil || result.Receipt.State != string(reconciliation.OperationUnknown) || !slices.Equal(h.capability.destroys, []string{"storage"}) {
		t.Fatalf("seeded lost destroy did not fire: %+v, removed %v (%v)", result, h.capability.destroys, err)
	}
	h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted}}
	result, err = destroy(h)
	if err != nil || result.Receipt.State != string(reconciliation.OperationDone) {
		t.Fatalf("continuation = %+v (%v)", result, err)
	}
	if !slices.Contains(h.capability.calls, "observe-removal:storage") {
		t.Fatalf("the retired block's removal was never observed: %v", h.capability.calls)
	}
	flagged := map[string][]bool{}
	for _, execution := range h.capability.executions[applied:] {
		flagged[execution.Block.ID] = append(flagged[execution.Block.ID], execution.RetiredWithHosts)
	}
	if got := flagged["storage"]; len(got) < 2 || slices.Contains(got, false) {
		t.Fatalf("retired block executions = %v, want every destroy and removal observation flagged", got)
	}
	for _, id := range []string{"installation", "machine", "host"} {
		if len(flagged[id]) == 0 || slices.Contains(flagged[id], true) {
			t.Fatalf("%s executions = %v, want unflagged", id, flagged[id])
		}
	}
}

func TestAnOwnRemedyIsPrintedBesideTheGenericStopOfOtherLiveBlocks(t *testing.T) {
	h, _ := retiringHarness(t, hostedDefinitions(stagedDefinition("other", reconciliation.StageClusters, "installation")), []string{"rhel-01", "rhel-02"})
	completeApply(t, h)
	h.capability.quiescence = map[string]Quiescence{"other": {State: Live, Reason: "it serves a client", Stop: "bootwright other stop"}}
	_, err := destroy(h)
	if firstCode(err) != "lifecycle.live" || len(h.capability.destroys) != 0 {
		t.Fatalf("live blocks admitted the removal: %v (%v)", h.capability.destroys, err)
	}
	reported := diagnostics.Of(err)[0]
	want := survivingHostRemedy + "; stop it with bootwright other stop, then repeat bootwright destroy --context lab"
	if !strings.Contains(reported.Message, "its hosts outlive this removal") || !strings.Contains(reported.Message, "it serves a client") || !strings.HasPrefix(reported.Remediation, want) {
		t.Fatalf("refusal = %+v, want the own remedy then %q", reported, want)
	}
}
