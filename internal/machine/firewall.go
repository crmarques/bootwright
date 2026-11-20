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
	profile, found := catalog.Find(api.MachineInstallProfile, api.MachineInstallProfileRef(machine))
	return found && profile.Spec().Get("customizations", "security", "firewall", "enabled").Bool()
}

func validateFirewall(machine api.Object, catalog api.Catalog) []api.Issue {
	if machine.Spec().Get("firewall", "management").Text() != "managed" {
		return nil
	}
	if api.MachineProvided(machine) {
		if _, err := PlacementFor(machine, controllerForPlacement(catalog)); err != nil {
			return []api.Issue{invariant("$.spec.firewall.management", "managed firewall preparation on a remote host requires pinned lifecycle SSH access", "author spec.access.lifecycle or a pinned root login in spec.access.ssh")}
		}
	}
	if !api.MachineProvided(machine) {
		profile, found := catalog.Find(api.MachineInstallProfile, api.MachineInstallProfileRef(machine))
		if !Installed(machine) || !found || !profile.Spec().Get("customizations", "security", "firewall", "enabled").Bool() {
			return []api.Issue{invariant("$.spec.firewall.management", "managed firewall policy requires an OS-ready host or a Bootwright installation with its firewall explicitly enabled", "set security.firewall.enabled: true on the install profile, or omit the Machine firewall policy")}
		}
	}
	return nil
}
