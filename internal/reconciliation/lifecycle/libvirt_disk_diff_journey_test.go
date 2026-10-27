package lifecycle_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate/libvirt"
)

type resizingHypervisor struct {
	request     libvirt.MachineRequest
	observedGiB int
	stopped     bool
}

func (h resizingHypervisor) Run(_ context.Context, run lifecycle.RunRequest) (lifecycle.RunResult, error) {
	size := h.request.Disks[0].SizeGiB
	outcome := "changed"
	if run.Operation == "observe" {
		size, outcome = h.observedGiB, "unchanged"
	}
	evidence := libvirt.MachineEvidence{
		Answered: true, Controller: h.request.Controller.Image, Domain: h.request.Domain, Listener: held(true), Owned: true,
		Disks:         []libvirt.DiskEvidence{{Name: "root", Present: true, SizeGiB: size}},
		Postcondition: run.Operation != "observe" || !h.stopped,
		Power:         "On", Request: run.Digest, State: "running", System: h.request.UUID, Unit: "active",
	}
	if run.Operation == "observe" && h.stopped {
		evidence.Unit = "inactive"
	}
	data, err := json.Marshal(evidence)
	return lifecycle.RunResult{Outcome: outcome, Evidence: data}, err
}

func libvirtDeclaration(diskGiB string) []api.Object {
	field := func(name string, value api.Value) api.FieldValue { return api.FieldValue{Name: name, Value: value} }
	text := func(name, value string) api.FieldValue { return field(name, api.StringValue(value)) }
	return []api.Object{
		api.NewObject(api.InfraProvider, "lab-libvirt", api.Value{}, api.MapValue(field("libvirt", api.MapValue(
			field("machineProfiles", api.ListValue(api.MapValue(
				text("name", "rhel"), field("diskGiB", api.IntegerValue(diskGiB)),
			))),
		)))),
		api.NewObject(api.Machine, "rhel-01", api.Value{}, api.MapValue(
			field("substrate", api.MapValue(text("providerRef", "lab-libvirt"), field("libvirt", api.MapValue(text("profileRef", "rhel"))))),
		)),
	}
}

func diskDiffJourney(t *testing.T, hypervisor resizingHypervisor) *lifecycle.CapabilityJourney {
	t.Helper()
	request := lostMachineRequest()
	hypervisor.request = request
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	journey := lifecycle.NewCapabilityJourney(t, libvirt.NewMachine(hypervisor),
		lifecycle.CapabilityBinding{Kind: libvirt.MachineKind, Implementation: libvirt.MachineImplementation},
		lifecycle.CapabilityPlan{
			Definitions: []reconciliation.BlockDefinition{{
				ID: lostMachine, Description: "realize the virtual machine rhel-01 and its emulated BMC", Stage: reconciliation.StageMachines,
				Kind: libvirt.MachineKind, Object: "rhel-01", Implementation: libvirt.MachineImplementation,
				ContentDigest: strings.Repeat("d", 64), Request: canonical,
				Groups: []reconciliation.Group{{ID: "define-domain", Description: "define the domain", Machines: []string{"rhel-01"}}},
			}},
			Reservations: []prerequisites.HostReservation{{Context: lifecycle.JourneyContext, Kind: "substrate-machine", Service: "rhel-01", Keys: request.ReservationKeys()}},
			Secrets:      request.SecretReferences(),
		})
	if _, err := journey.Service.Apply(context.Background(), lifecycle.ApplyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true}); err != nil {
		t.Fatalf("apply = %v", diagnostics.Of(err))
	}
	journey.Declaring(libvirtDeclaration("60")...)
	return journey
}

func observedObject(t *testing.T, journey *lifecycle.CapabilityJourney, kind api.Kind, name string) lifecycle.ObjectDiff {
	t.Helper()
	result, err := journey.Service.Diff(context.Background(), lifecycle.DiffRequest{ContextName: lifecycle.JourneyContext, Observe: true})
	if err != nil {
		t.Fatalf("diff --observe = %v", err)
	}
	for _, object := range result.Objects {
		if object.Kind == kind && object.Name == name {
			return object
		}
	}
	t.Fatalf("the diff names no %s/%s: %+v", kind, name, result.Objects)
	return lifecycle.ObjectDiff{}
}

func TestALibvirtDiskResizedByHandShowsAsAHunk(t *testing.T) {
	journey := diskDiffJourney(t, resizingHypervisor{observedGiB: 100, stopped: true})
	provider := observedObject(t, journey, api.InfraProvider, "lab-libvirt")
	if provider.Real.State != "partial" || provider.Real.Source != "observed" || provider.Real.Object == nil || provider.Desired == nil {
		t.Fatalf("the provider reads %+v", provider.Real)
	}
	realized := provider.Real.Object.Spec().Get("libvirt", "machineProfiles").Items()
	declared := provider.Desired.Spec().Get("libvirt", "machineProfiles").Items()
	if len(realized) != 1 || len(declared) != 1 || realized[0].Get("name").Text() != "rhel" ||
		realized[0].Get("diskGiB").Text() != "100" || declared[0].Get("diskGiB").Text() != "60" {
		t.Fatalf("realized %+v, declared %+v", realized, declared)
	}
	if provider.Desired.Spec().Equal(provider.Real.Object.Spec()) {
		t.Fatal("the resized disk shows no difference")
	}
	machine := observedObject(t, journey, api.Machine, "rhel-01")
	if got := machine.Real.Object.Spec().Get("substrate", "providerRef").Text(); got != "lab-libvirt" {
		t.Fatalf("the machine reads providerRef %q", got)
	}
}
