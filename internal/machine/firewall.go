package machine

import api "github.com/crmarques/bootwright/api/v1alpha1"

const (
	FirewallFamily = "os-host-firewall"
)

// ManagesFirewall selects the Machine's host-owned policy.
func ManagesFirewall(catalog api.Catalog, name string) bool {
	machine, found := catalog.Find(api.Machine, name)
	if !found {
		return false
	}
	switch machine.Spec().Get("firewall", "management").Text() {
	case "external":
		return false
	case "managed":
		return true
	}
	if !Installed(machine) {
		return false
	}
	profile, found := catalog.Find(api.MachineInstallProfile, machine.Spec().Get("os", "installProfileRef").Text())
	return found && profile.Spec().Get("customizations", "security", "firewall", "enabled").Bool()
}

func validateFirewall(machine api.Object, catalog api.Catalog) []api.Issue {
	if machine.Spec().Get("firewall", "management").Text() != "managed" {
		return nil
	}
	if machine.Spec().Get("os", "provided").Bool() {
		controller := ""
		for _, environment := range catalog.OfKind(api.Environment) {
			controller = environment.Spec().Get("controller", "machineRef").Text()
		}
		ssh := machine.Spec().Get("access", "ssh")
		if machine.Name() != controller && (!ssh.Has("auth", "privateKeyRef") || !ssh.Has("knownHostsRef")) {
			return []api.Issue{invariant("$.spec.firewall.management", "managed firewall preparation on a remote host requires pinned root SSH access", "author access.ssh.auth.privateKeyRef and access.ssh.knownHostsRef on this Machine")}
		}
	}
	if !machine.Spec().Get("os", "provided").Bool() {
		profile, found := catalog.Find(api.MachineInstallProfile, machine.Spec().Get("os", "installProfileRef").Text())
		if !Installed(machine) || !found || !profile.Spec().Get("customizations", "security", "firewall", "enabled").Bool() {
			return []api.Issue{invariant("$.spec.firewall.management", "managed firewall policy requires an OS-ready host or a Bootwright installation with its firewall explicitly enabled", "set security.firewall.enabled: true on the install profile, or omit the Machine firewall policy")}
		}
	}
	return nil
}
