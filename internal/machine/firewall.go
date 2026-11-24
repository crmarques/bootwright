package machine

import (
	"fmt"
	"net/netip"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

const (
	FirewallFamily = "os-host-firewall"
	MaxSSHSources  = 64
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
	if machine.Spec().Has("firewall", "ssh") {
		if machine.Spec().Get("firewall", "management").Text() == "external" || !Installed(machine) && !ManagesFirewall(catalog, machine.Name()) {
			return []api.Issue{invariant("$.spec.firewall.ssh", "SSH source admission requires a Bootwright-installed Machine or a provided Machine with a managed firewall", "select managed firewall on a provided Machine or remove the SSH policy")}
		}
		if !api.MachineProvided(machine) && !Installed(machine) {
			return []api.Issue{invariant("$.spec.firewall.ssh", "the downstream installer owns this Machine's SSH admission", "remove the Bootwright SSH policy")}
		}
		if Installed(machine) {
			profile, found := catalog.Find(api.MachineInstallProfile, api.MachineInstallProfileRef(machine))
			if found && !slices.Contains(profile.Spec().Get("customizations", "packages", "install").Strings(), "nftables") {
				return []api.Issue{invariant("$.spec.firewall.ssh", "the SSH filter requires nftables on the installed Machine", "add nftables to the install profile packages")}
			}
		}
		if _, issue := SSHSourcePrefixes(machine, catalog); issue != nil {
			return []api.Issue{*issue}
		}
	}
	if machine.Spec().Get("firewall", "management").Text() != "managed" {
		return nil
	}
	if api.MachineProvided(machine) {
		if _, err := PlacementFor(machine, ControllerName(catalog)); err != nil {
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

// SSHSourcePrefixes resolves authored source records to sorted, unique IP prefixes.
func SSHSourcePrefixes(machine api.Object, catalog api.Catalog) ([]string, *api.Issue) {
	field := "$.spec.firewall.ssh.allowedSources"
	sources := machine.Spec().Get("firewall", "ssh", "allowedSources")
	if !sources.Present() {
		return nil, nil
	}
	entries := sources.Items()
	if len(entries) == 0 || len(entries) > MaxSSHSources {
		issue := invariant(field, "SSH source admission requires 1 to 64 sources", "author a nonempty list of approved SSH sources")
		return nil, &issue
	}
	prefixes := make([]string, 0, len(entries))
	for index, entry := range entries {
		prefix, valid := sshSourcePrefix(entry, catalog)
		if !valid {
			issue := invariant(fmt.Sprintf("%s[%d]", field, index), "the SSH source does not select a supported IP or network prefix", "use an unzoned IP, a CIDR with host bits cleared, or a named numeric Machine address")
			return nil, &issue
		}
		prefixes = append(prefixes, prefix.String())
	}
	slices.Sort(prefixes)
	return slices.Compact(prefixes), nil
}

func sshSourcePrefix(entry api.Value, catalog api.Catalog) (netip.Prefix, bool) {
	if text := entry.Get("cidr").Text(); text != "" {
		prefix, err := netip.ParsePrefix(text)
		return prefix, err == nil && prefix == prefix.Masked() && !prefix.Addr().Is4In6()
	}
	text := entry.Get("ip").Text()
	if selection := entry.Get("machine"); selection.Present() {
		source, found := catalog.Find(api.Machine, selection.Get("machineRef").Text())
		if !found {
			return netip.Prefix{}, false
		}
		for _, address := range source.Spec().Get("network", "addresses").Items() {
			if address.Get("name").Text() == selection.Get("addressRef").Text() {
				text = address.Get("address").Text()
				break
			}
		}
	}
	address, err := netip.ParseAddr(text)
	address = address.Unmap()
	return netip.PrefixFrom(address, address.BitLen()), err == nil && address.Zone() == ""
}
