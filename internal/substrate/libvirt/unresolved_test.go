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
			got, explains := NewMachine(nil).Unresolved(block, test.evidence)
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
