package libvirt

import (
	"encoding/json"

	machineref "github.com/crmarques/bootwright/internal/machine"
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
// that socket. Evidence it cannot read, or that proves none of these, is left
// to the engine's general reason.
func (MachineCapability) Unresolved(block reconciliation.Block, evidence json.RawMessage) (lifecycle.Unresolved, bool) {
	request, err := DecodeMachineRequest(block.Request)
	if err != nil {
		return lifecycle.Unresolved{}, false
	}
	host := placedOn(request.Placement)
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
	return lifecycle.Unresolved{}, false
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

// placedOn names the host a block's placement runs its adapter against, with
// the address it is reached at when that is not this controller.
func placedOn(placement machineref.Placement) string {
	if placement.Local() || placement.Address == "" {
		return "Machine " + placement.Machine
	}
	return "Machine " + placement.Machine + " at " + placement.Address
}
