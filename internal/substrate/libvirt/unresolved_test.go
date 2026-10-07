package libvirt

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// remoteMachine is the lab machine on a provider host reached over SSH, so an
// explanation names the host and the address it is reached at.
func remoteMachine(t *testing.T) (MachineRequest, reconciliation.Block) {
	t.Helper()
	remote := provider(field("libvirt", provider().Spec().Get("libvirt").With("machineRef", api.StringValue("hypervisor"))))
	requests, err := MachineRequests(catalogOf(controller(), remoteHost(), remote, networkConfig(), guest("rhel-01")), "controller", testContext)
	if err != nil || len(requests) != 1 {
		t.Fatalf("requests = %d (%v)", len(requests), err)
	}
	canonical, err := requests[0].Canonical()
	if err != nil {
		t.Fatal(err)
	}
	return requests[0], reconciliation.Block{
		BlockDefinition: reconciliation.BlockDefinition{ID: "machine-rhel-01", Kind: MachineKind, Implementation: MachineImplementation, Object: "rhel-01", Request: canonical},
		RequestDigest:   evidenceDigest,
	}
}

// goldenEvidence reads one pinned observation back as the compact bytes the
// protocol plugin publishes.
func goldenEvidence(t *testing.T, name string) json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name+".golden"))
	if err != nil {
		t.Fatal(err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, data); err != nil {
		t.Fatal(err)
	}
	return compact.Bytes()
}

// Each observation no resolution can prove anything from names why, in the
// operator's terms, with what to do before repeating the verb: a same-name
// domain this context does not own, named with its host; a hypervisor that
// did not answer, named by its connection; a listener with nothing of the
// machine behind it, named by its socket; and an observation that returned
// nothing, named by the host it ran against. Each foreign and silent golden is
// what substrate_machine_protocol.py's presence() publishes for an
// observation of that state, which every validator refuses, so its
// resolution is unknown. Evidence of an owned machine part way realized, or
// evidence that cannot be read, is left to the engine's general reason.
func TestUnresolvedMachineObservationsMatchTheirGoldensAndNameWhy(t *testing.T) {
	request, block := remoteMachine(t)
	gone := make([]DiskEvidence, 0, len(request.Disks))
	for _, disk := range request.Disks {
		gone = append(gone, DiskEvidence{Name: disk.Name})
	}
	pinned := map[string]MachineEvidence{
		// A domain of the same name without this context's ownership
		// metadata, running, whose uuid the observation reports as the system.
		"foreign": {
			Answered: true, Disks: gone, Domain: request.Domain, Listener: observed(false), Request: evidenceDigest,
			State: "running", System: "0f5c6d7e-8a9b-4c0d-9e1f-2a3b4c5d6e7f",
		},
		// A hypervisor that did not answer for the domain, with nothing else
		// of the machine present and nothing listening on its socket.
		"silent": {Disks: gone, Listener: observed(false), Request: evidenceDigest},
	}
	for name, evidence := range pinned {
		canonical, err := reconciliation.Freeze(evidence, "machine evidence")
		if err != nil {
			t.Fatal(err)
		}
		matchesGolden(t, "machine-evidence-"+name, canonical)
	}
	host := "Machine hypervisor at 192.0.2.5"
	for name, test := range map[string]struct {
		evidence json.RawMessage
		want     lifecycle.Unresolved
		explains bool
	}{
		"no evidence": {nil, lifecycle.Unresolved{
			Reason: "its observation returned no evidence from " + host,
			Remedy: "read why in the resolution log and restore " + host + " so the observation reaches it",
		}, true},
		"foreign": {goldenEvidence(t, "machine-evidence-foreign"), lifecycle.Unresolved{
			Reason: "domain " + request.Domain + " on " + host + " does not carry this context's ownership",
			Remedy: "remove or rename domain " + request.Domain + " on " + host + ", which this context does not own",
		}, true},
		"silent": {goldenEvidence(t, "machine-evidence-silent"), lifecycle.Unresolved{
			Reason: "the libvirt connection qemu:///system on " + host + " did not answer for domain " + request.Domain,
			Remedy: "restore the libvirt connection qemu:///system on " + host + " so it answers for that domain",
		}, true},
		"held-socket": {goldenEvidence(t, "machine-evidence-held-socket"), lifecycle.Unresolved{
			Reason: "something listens on the controller socket 192.0.2.1:8000 on " + host + ", yet none of domain " + request.Domain + ", its controller unit and its disks is present",
			Remedy: "stop what listens on 192.0.2.1:8000 on " + host,
		}, true},
		"an owned machine part way realized": {goldenEvidence(t, "machine-evidence-partial-disks"), lifecycle.Unresolved{}, false},
		"evidence for another request":       {encode(t, MachineEvidence{Answered: true, Domain: request.Domain, Listener: observed(false), Request: "other"}), lifecycle.Unresolved{}, false},
		"malformed evidence":                 {json.RawMessage(`{"domain":`), lifecycle.Unresolved{}, false},
	} {
		t.Run(name, func(t *testing.T) {
			got, explains := NewMachine(nil).Unresolved(reconciliation.Apply, block, test.evidence)
			if explains != test.explains || got != test.want {
				t.Fatalf("unresolved = %+v (%t), want %+v (%t)", got, explains, test.want, test.explains)
			}
			if !test.explains || test.evidence == nil {
				return
			}
			runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: test.evidence}}
			for verb, observe := range map[string]func(context.Context, lifecycle.Execution) (lifecycle.Observation, error){
				"apply": NewMachine(runner).Observe, "destroy": NewMachine(runner).ObserveRemoval,
			} {
				observation, err := observe(context.Background(), lifecycle.Execution{Block: block})
				if err != nil || observation.Effect != reconciliation.EffectUnknown {
					t.Fatalf("the %s resolution read %s (%v), want unknown", verb, observation.Effect, err)
				}
			}
		})
	}
}

// An owned domain the hypervisor answered for, whose observation proves the
// adapter's postcondition but not the frozen request, names the first
// difference under an apply, and its remedy is to restore it. A removal's own
// resolution reads that same evidence as no effect: drift is never unknown for
// a removal.
func TestADriftedMachineNamesItsFirstDifference(t *testing.T) {
	request, block := remoteMachine(t)
	host := "Machine hypervisor at 192.0.2.5"
	remedy := "restore domain " + request.Domain + " on " + host + " to what its frozen request names"
	for name, test := range map[string]struct {
		drift   func(*MachineEvidence)
		refusal string
	}{
		"a controller on another image":        {func(e *MachineEvidence) { e.Controller = "docker.io/other@sha256:0" }, "the running management controller is not the frozen image"},
		"a controller exposing another system": {func(e *MachineEvidence) { e.System = "00000000-0000-0000-0000-000000000000" }, "the management controller does not expose this machine's system"},
		"a resized disk":                       {func(e *MachineEvidence) { e.Disks[0].SizeGiB++ }, "a machine disk is not the size the profile froze"},
	} {
		t.Run(name, func(t *testing.T) {
			var evidence MachineEvidence
			if err := json.Unmarshal(machineEvidence(request, evidenceDigest), &evidence); err != nil {
				t.Fatal(err)
			}
			test.drift(&evidence)
			data := encode(t, evidence)
			got, explains := NewMachine(nil).Unresolved(reconciliation.Apply, block, data)
			want := lifecycle.Unresolved{Reason: "domain " + request.Domain + " on " + host + " is not what its apply froze: " + test.refusal, Remedy: remedy}
			if !explains || got != want {
				t.Fatalf("unresolved = %+v (%t), want %+v", got, explains, want)
			}
			runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: data}}
			observation, err := NewMachine(runner).ObserveRemoval(context.Background(), lifecycle.Execution{Block: block})
			if err != nil || observation.Effect != reconciliation.EffectNoEffect {
				t.Fatalf("the removal read the drifted machine as %s (%v), want no effect", observation.Effect, err)
			}
			if _, explains := NewMachine(nil).Unresolved(reconciliation.Destroy, block, data); explains {
				t.Fatal("a removal explained evidence it reads as no effect")
			}
		})
	}
}

// A provider host this request's own that none of the verb's checks accept is
// named by the check that decided: under an apply the presence check, under a
// removal the unfinished removal's. Evidence for another request or none at
// all is left to the general reason.
func TestUnresolvedHostNamesTheRefusingCheck(t *testing.T) {
	request := hostRequest(t)
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	block := reconciliation.Block{
		BlockDefinition: reconciliation.BlockDefinition{ID: "substrate-host-lab", Kind: HostKind, Implementation: HostImplementation, Object: "lab", Request: canonical},
		RequestDigest:   evidenceDigest,
	}
	decoded := func() HostEvidence {
		var evidence HostEvidence
		if err := json.Unmarshal(hostEvidence(request, evidenceDigest), &evidence); err != nil {
			t.Fatal(err)
		}
		return evidence
	}
	subject := "the networks and pool of InfraProvider lab on Machine controller"
	noAutostart := decoded()
	noAutostart.PoolAutostart = false
	foreign := decoded()
	foreign.Postcondition, foreign.Pool, foreign.PoolOwned = false, "", false
	managed := false
	for index := range foreign.Networks {
		if foreign.Networks[index].Managed {
			foreign.Networks[index].Owned, managed = false, true
		}
	}
	if !managed {
		t.Fatal("the lab provider declares no managed network")
	}
	other := decoded()
	other.Request = "other"
	for name, test := range map[string]struct {
		verb     reconciliation.Verb
		evidence []byte
		want     lifecycle.Unresolved
		explains bool
	}{
		"an apply over a pool that does not autostart": {reconciliation.Apply, encode(t, noAutostart), lifecycle.Unresolved{
			Reason: subject + " is not what its apply froze: the provider's virtual-media pool does not start with the host",
			Remedy: "restore " + subject + " to what its frozen request names",
		}, true},
		"a removal over a foreign managed network": {reconciliation.Destroy, encode(t, foreign), lifecycle.Unresolved{
			Reason: subject + " is not what its destroy froze: a managed libvirt network exists without this context's ownership",
			Remedy: "restore " + subject + " so its observation reads it as this context's own",
		}, true},
		"a removal over a pool that does not autostart": {reconciliation.Destroy, encode(t, noAutostart), lifecycle.Unresolved{}, false},
		"evidence for another request":                  {reconciliation.Apply, encode(t, other), lifecycle.Unresolved{}, false},
		"no evidence":                                   {reconciliation.Apply, nil, lifecycle.Unresolved{}, false},
	} {
		t.Run(name, func(t *testing.T) {
			got, explains := NewHost(nil).Unresolved(test.verb, block, test.evidence)
			if explains != test.explains || got != test.want {
				t.Fatalf("unresolved = %+v (%t), want %+v (%t)", got, explains, test.want, test.explains)
			}
		})
	}
}
