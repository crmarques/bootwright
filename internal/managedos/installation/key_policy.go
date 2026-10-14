package installation

// The placement-host adapter names only these bounded reasons. Public
// diagnostics come from the actual bound key types, never native output.
func keyPolicyRefusals(request Request, values map[string]string) map[string]error {
	remedy := "choose keys compatible with the qualified placement host SSH policy before planning this installation again"
	result := map[string]error{
		"ssh-policy-unavailable":    refusal("access.unavailable", "the installation placement host SSH crypto policy could not be evaluated before publication or boot", "restore a supported crypto-only SSH policy and the qualified SSH client on the installation placement host"),
		"ssh-policy-fleet-excluded": refusal("access.unavailable", "Secret/"+request.FleetKeyRef+" key type "+values["fleetKeyType"]+" is excluded by the installation placement host SSH fleet key policy; nothing was published or booted", remedy),
	}
	if request.Target.HostKeyRef != "" {
		result["ssh-policy-host-excluded"] = refusal("access.unavailable", "Secret/"+request.Target.HostKeyRef+" key type "+values["hostKeyType"]+" is excluded by the installation placement host SSH host key policy; nothing was published or booted", remedy)
	}
	return result
}
