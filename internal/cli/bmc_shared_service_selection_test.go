package cli

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/clusteraccess"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/ownership"
	stategraph "github.com/crmarques/bootwright/internal/state/graph"
)

func TestBMCSharedServiceSelectionUsesMutationHostsForGuestSelection(t *testing.T) {
	state := loadSharedServiceTestState(t)
	refs := selectedBMCEmulatorServiceRefs(state, nil)
	if len(refs) == 0 {
		t.Fatal("fixture needs an emulated BMC provider service")
	}
	if len(refs[0].SelectionDigests) != 1 || !strings.HasPrefix(refs[0].SelectionDigests[0], "sha256:") || len(refs[0].SelectionDigests[0]) != 71 {
		t.Fatalf("BMC ref lacks one exact desired selection digest: %+v", refs[0])
	}
	host := refs[0].Host
	sel := clusteraccess.Selection{
		MachineSelection: true,
		MachineProvision: map[string]bool{"selected-guest": true},
		WorkMachines:     map[string]bool{"selected-guest": true, host: true},
	}
	selected := selectedBMCEmulatorServiceRefs(state, sel.WorkMachines)
	if len(selected) == 0 || selected[0].Host != host {
		t.Fatalf("guest selection omitted provider mutation host %q: %+v", host, selected)
	}
	if got := selectedBMCEmulatorServiceRefs(state, sel.MachineProvision); len(got) != 0 {
		t.Fatalf("test fixture does not distinguish provision from mutation hosts: %+v", got)
	}
}

func TestBMCSharedServiceSelectionDigestBindsDesiredHostConfiguration(t *testing.T) {
	state := loadSharedServiceTestState(t)
	graph := stategraph.ResolveMachineServices(state)
	var service stategraph.MachineService
	for _, candidate := range graph.Services {
		if candidate.IsProviderService() && candidate.Identity.Name == "emulated" {
			service = candidate
			break
		}
	}
	if service.Identity.Name == "" {
		t.Fatal("fixture needs an emulated BMC service")
	}
	digest := bmcSharedServiceSelectionDigest(state, service)
	if digest == "" || digest != bmcSharedServiceSelectionDigest(state, service) {
		t.Fatalf("BMC selection digest is empty or nondeterministic: %q", digest)
	}
	changed := service
	changed.Fields = make(map[string]string, len(service.Fields))
	for key, value := range service.Fields {
		changed.Fields[key] = value
	}
	changed.Fields["configKey"] += "|changed"
	if changedDigest := bmcSharedServiceSelectionDigest(state, changed); changedDigest == "" || changedDigest == digest {
		t.Fatalf("BMC selection digest did not bind rendered desired config: before=%q after=%q", digest, changedDigest)
	}
	changed = service
	changed.Fields = make(map[string]string, len(service.Fields))
	for key, value := range service.Fields {
		changed.Fields[key] = value
	}
	changed.Fields["applyRole"] = "bootwright.core.future_bmc"
	if changedDigest := bmcSharedServiceSelectionDigest(state, changed); changedDigest == "" || changedDigest == digest {
		t.Fatalf("BMC selection digest did not bind mutating role identity: before=%q after=%q", digest, changedDigest)
	}
	if changedDigest := bmcSharedServiceSelectionDigestForVersion(service, "future-version"); changedDigest == "" || changedDigest == digest {
		t.Fatalf("BMC selection digest did not bind sushy-tools version: before=%q after=%q", digest, changedDigest)
	}
}

func TestBMCEmulatorDestroyConsequenceSkipsMachineScopedPlan(t *testing.T) {
	state := loadSharedServiceTestState(t)
	sel := clusteraccess.Selection{
		RenderState:      state,
		MachineSelection: true,
		WorkMachines:     map[string]bool{"guest": true, "provider-host": true},
	}
	refs, records := bmcEmulatorDestroyConsequence(state, sel, converge.InfraScope, false, false, []ownership.ResourceRecord{{Kind: string(ownership.KindBMCEmulator), Name: "provider", Host: "provider-host"}})
	if len(refs) != 0 || len(records) != 0 {
		t.Fatalf("machine-scoped plan has no provider-services mutation consequence: refs=%+v records=%+v", refs, records)
	}
}
