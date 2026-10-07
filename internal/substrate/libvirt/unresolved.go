package libvirt

import (
	"encoding/json"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate"
)

// Unresolved says why an observation of this machine proved nothing, from the
// evidence it recorded, in the order an operator has to act on it: an
// observation that returned nothing names the host it runs on, a same-name
// domain without this context's ownership is named with its host, a
// hypervisor that did not answer is named by its connection, and a listener
// on the controller's socket with nothing of the machine behind it is named by
// that socket. An owned domain the hypervisor answered for, which none of the
// verb's checks accept, is named by the check that decided: for an apply the
// first difference from the frozen request, such as the controller's image,
// the system it exposes, a power state or a disk's size. Evidence it cannot
// read, or for another request, is left to the engine's general reason.
func (MachineCapability) Unresolved(verb reconciliation.Verb, block reconciliation.Block, evidence json.RawMessage) (lifecycle.Unresolved, bool) {
	request, err := DecodeMachineRequest(block.Request)
	if err != nil {
		return lifecycle.Unresolved{}, false
	}
	host := lifecycle.PlacedOn(request.Placement)
	if len(evidence) == 0 {
		return lifecycle.Unresolved{
			Reason: "its observation returned no evidence from " + host,
			Remedy: "read why in the resolution log and restore " + host + " so the observation reaches it",
		}, true
	}
	observed, err := decodeMachineEvidence(evidence, block.RequestDigest)
	if err != nil {
		return lifecycle.Unresolved{}, false
	}
	socket := substrate.ControllerSocket(request.Controller.Address, request.Controller.Port)
	switch {
	case observed.Domain != "" && !observed.Owned:
		return lifecycle.Unresolved{
			Reason: "domain " + observed.Domain + " on " + host + " does not carry this context's ownership",
			Remedy: "remove or rename domain " + observed.Domain + " on " + host + ", which this context does not own",
		}, true
	case !observed.Answered:
		return lifecycle.Unresolved{
			Reason: "the libvirt connection " + request.URI + " on " + host + " did not answer for domain " + request.Domain,
			Remedy: "restore the libvirt connection " + request.URI + " on " + host + " so it answers for that domain",
		}, true
	case observed.Listener != nil && *observed.Listener && !machineRemains(observed):
		return lifecycle.Unresolved{
			Reason: "something listens on the controller socket " + socket + " on " + host + ", yet none of domain " + request.Domain + ", its controller unit and its disks is present",
			Remedy: "stop what listens on " + socket + " on " + host,
		}, true
	}
	effect, refused := machineEffect(verb, evidence, request, block.RequestDigest)
	if effect != reconciliation.EffectUnknown {
		return lifecycle.Unresolved{}, false
	}
	return lifecycle.Drifted(verb, "domain "+request.Domain, host, refused)
}

// Unresolved says why an observation of this provider host proved nothing,
// from the evidence it recorded: evidence this request's own that none of the
// verb's checks accept is named by the check that decided. Empty evidence,
// evidence for another request and evidence that does not decode are left to
// the engine's general reason.
func (HostCapability) Unresolved(verb reconciliation.Verb, block reconciliation.Block, evidence json.RawMessage) (lifecycle.Unresolved, bool) {
	request, err := DecodeHostRequest(block.Request)
	if err != nil || len(evidence) == 0 {
		return lifecycle.Unresolved{}, false
	}
	if _, err := decodeHostEvidence(evidence, block.RequestDigest); err != nil {
		return lifecycle.Unresolved{}, false
	}
	effect, refused := hostEffect(verb, evidence, request, block.RequestDigest)
	if effect != reconciliation.EffectUnknown {
		return lifecycle.Unresolved{}, false
	}
	return lifecycle.Drifted(verb, "the networks and pool of InfraProvider "+block.Object, lifecycle.PlacedOn(request.Placement), refused)
}

// machineRemains reports whether any part of the machine an observation reads
// as this context's own is present.
func machineRemains(observed MachineEvidence) bool {
	if observed.Domain != "" || observed.Unit != "" || observed.Controller != "" {
		return true
	}
	for _, disk := range observed.Disks {
		if disk.Present {
			return true
		}
	}
	return false
}
