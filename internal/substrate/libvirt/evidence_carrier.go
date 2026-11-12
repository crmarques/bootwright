package libvirt

// CarrierPolicyEvidence distinguishes canonical, pending and foreign device policies.
type CarrierPolicyEvidence struct {
	Answered   bool   `json:"answered"`
	Definition bool   `json:"definition"`
	Name       string `json:"name"`
	Owned      bool   `json:"owned"`
	Present    bool   `json:"present"`
}

func carrierPolicyName(request HostRequest) string {
	return "90-bootwright-" + request.Identity.Context + "-" + request.Identity.Object + "-vlan-carrier.conf"
}

func carrierHeld(observed CarrierPolicyEvidence, request HostRequest) error {
	if !observed.Answered || observed.Name != carrierPolicyName(request) {
		return refusal("lifecycle.state", "the provider host did not answer for its frozen VLAN carrier policy", "")
	}
	if len(request.Legs) == 0 {
		if observed.Present || observed.Owned {
			return refusal("lifecycle.state", "the provider host still holds an undeclared VLAN carrier policy", "")
		}
	} else if !observed.Present || !observed.Owned {
		return refusal("lifecycle.state", "the provider host no longer holds its owned VLAN carrier policy", "")
	}
	return nil
}

func matchCarrierPolicy(observed CarrierPolicyEvidence, request HostRequest) error {
	if err := carrierHeld(observed, request); err != nil {
		return err
	}
	if !observed.Definition {
		return refusal("lifecycle.state", "the provider host VLAN carrier policy is drifted or awaiting reload", "repeat the apply to restore the owned policy and reload NetworkManager configuration")
	}
	return nil
}
