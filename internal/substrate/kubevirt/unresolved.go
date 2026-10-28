package kubevirt

import (
	"encoding/json"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// Unresolved says why an observation of this VirtualMachine proved nothing:
// an observation that returned nothing, a same-named VirtualMachine that is not
// this context's, and a host that did not answer are each named with the
// VirtualMachine; otherwise the check that decided names the difference.
// Evidence it cannot read, or for another request, is left to the engine.
func (MachineCapability) Unresolved(verb reconciliation.Verb, block reconciliation.Block, evidence json.RawMessage) (lifecycle.Unresolved, bool) {
	request, err := DecodeMachineRequest(block.Request)
	if err != nil {
		return lifecycle.Unresolved{}, false
	}
	vm := "VirtualMachine " + request.Namespace + "/" + request.Name
	if len(evidence) == 0 {
		return lifecycle.Unresolved{
			Reason: "its observation returned no evidence for " + vm,
			Remedy: "read why in the resolution log and restore the host cluster of InfraProvider/" + request.Provider + " so the observation reaches it",
		}, true
	}
	observed, err := decodeMachineEvidence(evidence, block.RequestDigest)
	if err != nil {
		return lifecycle.Unresolved{}, false
	}
	switch {
	case observed.Foreign:
		return lifecycle.Unresolved{
			Reason: vm + " exists and is not this context's",
			Remedy: "remove or rename " + vm + ", which this context does not own, or correct the namespace of InfraProvider/" + request.Provider,
		}, true
	case !observed.Answered:
		return lifecycle.Unresolved{
			Reason: "the host cluster of InfraProvider/" + request.Provider + " did not answer for " + vm,
			Remedy: "restore the host cluster's API, or rotate spec.kubevirt.kubeconfigRef of InfraProvider/" + request.Provider + " and continue with --authorize rebind=" + request.KubeconfigRef,
		}, true
	}
	effect, refused := machineEffect(verb, evidence, request, block.RequestDigest)
	if effect != reconciliation.EffectUnknown {
		return lifecycle.Unresolved{}, false
	}
	return lifecycle.Drifted(verb, vm, "the host cluster of InfraProvider/"+request.Provider, refused)
}

// Unresolved says why a provider proof's observation proved nothing: the
// check that failed or the field its presence check refused.
func (HostCapability) Unresolved(verb reconciliation.Verb, block reconciliation.Block, evidence json.RawMessage) (lifecycle.Unresolved, bool) {
	request, err := DecodeHostRequest(block.Request)
	if err != nil || len(evidence) == 0 || verb == reconciliation.Destroy {
		return lifecycle.Unresolved{}, false
	}
	if _, err := decodeHostEvidence(evidence, block.RequestDigest); err != nil {
		return lifecycle.Unresolved{}, false
	}
	refused := ValidateHostPresence(evidence, request, block.RequestDigest)
	if refused == nil {
		return lifecycle.Unresolved{}, false
	}
	return lifecycle.Drifted(verb, "the host cluster of InfraProvider/"+block.Object, "namespace "+request.Namespace, refused)
}
