package installation

import (
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate"
)

// guestAgent is installed on every libvirt Machine, because the identity this
// contract proves completion with is read through it.
const guestAgent = "qemu-guest-agent"

// installationFor derives the complete unattended installation from effective
// state alone. It resolves every service the guest uses while installing, and
// records each as a requirement so those blocks complete first.
func installationFor(catalog api.Catalog, machine, profile api.Object, request Request, needs *Requirements) (Installation, error) {
	template, err := substrate.NetworkTemplate(catalog, machine)
	if err != nil {
		return Installation{}, err
	}
	address, prefix, iface, err := installAddress(machine)
	if err != nil {
		return Installation{}, err
	}
	hostname, err := machineHostname(machine)
	if err != nil {
		return Installation{}, err
	}
	nameservers, err := resolverAddresses(catalog, machine, needs)
	if err != nil {
		return Installation{}, err
	}
	timeSources, err := timeAddresses(catalog, machine, profile, needs)
	if err != nil {
		return Installation{}, err
	}
	customizations := profile.Spec().Get("customizations")
	source, err := packageSource(request)
	if err != nil {
		return Installation{}, err
	}
	return Installation{
		Address:          address,
		AdditionalLocale: customizations.Get("localization", "additionalLocales").Strings(),
		DisabledServices: SortedUnique(customizations.Get("services", "disabled").Strings()),
		EnabledServices:  SortedUnique(append(customizations.Get("services", "enabled").Strings(), "sshd")),
		ExcludeDocs:      customizations.Get("packages", "excludeDocs").Bool(),
		Firewall:         firewallChoice(customizations),
		Formats:          customizations.Get("localization", "formats").Text(),
		Gateway:          substrate.DefaultGateway(template),
		Hostname:         hostname,
		Interface:        iface,
		Keyboard:         orDefault(customizations.Get("localization", "keyboard").Text(), "us"),
		Language:         orDefault(customizations.Get("localization", "language").Text(), "en_US.UTF-8"),
		MarkerPath:       MarkerPath,
		Nameservers:      nameservers,
		NTPServers:       timeSources,
		Packages:         SortedUnique(append(customizations.Get("packages", "install").Strings(), guestAgent)),
		PackageSource:    source,
		Prefix:           prefix,
		Repositories:     repositoriesFor(customizations),
		RootDevice:       machine.Spec().Get("os", "install", "rootDeviceHints", "deviceName").Text(),
		SELinux:          customizations.Get("security", "selinux", "mode").Text(),
		Timezone:         orDefault(customizations.Get("localization", "timezone").Text(), "UTC"),
		User:             installUser,
		WeakDeps:         weakDepsChoice(customizations),
	}, nil
}

// packageSource is what Anaconda installs from: the boot media itself when the
// profile names no source, or the tree this block publishes.
func packageSource(request Request) (string, error) {
	if request.Tree == nil {
		return "cdrom", nil
	}
	if request.Tree.URL == "" {
		return "", refusal("lifecycle.state", "the hosted package tree has no published URL", "")
	}
	return "url --url=" + request.Tree.URL, nil
}

// installAddress reads the one static assignment the installer applies, from
// the Machine's own selected install address.
func installAddress(machine api.Object) (string, int, string, error) {
	network := machine.Spec().Get("network")
	reference := network.Get("installAddressRef").Text()
	entry, found := findNamed(network.Get("addresses"), "name", reference)
	if !found {
		return "", 0, "", refusal("api.reference", "the Machine's install address does not resolve", "set spec.network.installAddressRef on "+machine.Identity())
	}
	address, prefix, ok := netmaskPrefix(entry.Get("address").Text())
	if !ok {
		return "", 0, "", refusal("api.value", "the Machine's install address carries no prefix", "declare it as an IP with its prefix on "+machine.Identity())
	}
	iface := entry.Get("interface").Text()
	if iface == "" {
		return "", 0, "", refusal("api.value", "the Machine's install address names no interface", "set the interface of that address on "+machine.Identity())
	}
	return address, prefix, iface, nil
}

func machineHostname(machine api.Object) (string, error) {
	entry, found := findNamed(machine.Spec().Get("network", "addresses"), "name", "fqdn")
	if !found || entry.Get("address").Text() == "" {
		return "", refusal("api.required", "the Machine declares no fully qualified name", "declare an fqdn address on "+machine.Identity())
	}
	return entry.Get("address").Text(), nil
}

// resolverAddresses derives the name servers the guest resolves through while
// installing, from the network configuration's own selections.
func resolverAddresses(catalog api.Catalog, machine api.Object, needs *Requirements) ([]string, error) {
	spec, err := substrate.NetworkSpec(catalog, machine)
	if err != nil {
		return nil, err
	}
	var addresses []string
	for _, selection := range spec.Get("dns").Items() {
		address, name, err := serviceEndpoint(catalog, api.DNSServer, selection, machine.Identity())
		if err != nil {
			return nil, err
		}
		needs.DNSServers = append(needs.DNSServers, name)
		if !slices.Contains(addresses, address) {
			addresses = append(addresses, address)
		}
	}
	return addresses, nil
}

// timeAddresses derives the time sources the guest uses, from the Machine's own
// override when it has one and the profile's selections otherwise.
func timeAddresses(catalog api.Catalog, machine, profile api.Object, needs *Requirements) ([]string, error) {
	selections := profile.Spec().Get("ntp")
	if override := machine.Spec().Get("os", "install", "ntp"); override.Present() {
		selections = override
	}
	var addresses []string
	for _, selection := range selections.Items() {
		address, name, err := serviceEndpoint(catalog, api.NTPServer, selection, machine.Identity())
		if err != nil {
			return nil, err
		}
		needs.NTPServers = append(needs.NTPServers, name)
		if !slices.Contains(addresses, address) {
			addresses = append(addresses, address)
		}
	}
	return addresses, nil
}

// serviceEndpoint resolves one managed service selection to the address a
// consumer reaches it at, and to the object whose block must complete first.
func serviceEndpoint(catalog api.Catalog, kind api.Kind, selection api.Value, identity string) (string, string, error) {
	reference := selection.Get("serverRef").Text()
	server, ok := catalog.Find(kind, reference)
	if !ok {
		return "", "", refusal("api.reference", "a selected "+string(kind)+" is not in the selected graph", "declare "+reference+" or correct the selection on "+identity)
	}
	if server.Spec().Get("management").Text() != "managed" {
		return "", "", refusal("lifecycle.state", "an installation uses only managed name and time services", "select a managed "+string(kind)+" on "+identity)
	}
	machine, ok := catalog.Find(api.Machine, server.Spec().Get("machineRef").Text())
	if !ok {
		return "", "", refusal("api.reference", "the service's placement Machine is not in the selected graph", "declare it or correct machineRef on "+server.Identity())
	}
	endpoints := server.Spec().Get("endpoints")
	entry, found := findNamed(endpoints, "name", selection.Get("endpointRef").Text())
	if !found {
		if items := endpoints.Items(); len(items) == 1 {
			entry, found = items[0], true
		}
	}
	if !found {
		return "", "", refusal("api.reference", "a service selection names no endpoint", "set endpointRef on the selection of "+identity)
	}
	address, err := lifecycle.MachineAddress(machine, entry.Get("addressRef").Text())
	if err != nil {
		return "", "", err
	}
	return address, server.Name(), nil
}

func repositoriesFor(customizations api.Value) []Repository {
	var repositories []Repository
	for _, entry := range customizations.Get("repositories", "configure").Items() {
		enabled := true
		if value := entry.Get("enabled"); value.Type() == api.Boolean {
			enabled = value.Bool()
		}
		repositories = append(repositories, Repository{
			BaseURL: entry.Get("baseURL").Text(), Enabled: enabled, ID: entry.Get("id").Text(),
		})
	}
	slices.SortFunc(repositories, func(x, y Repository) int { return strings.Compare(x.ID, y.ID) })
	return repositories
}

// firewallChoice and weakDepsChoice keep an absent declaration distinct from a
// declared false, because the operating system's own default is not the same
// answer as an explicit one.
func firewallChoice(customizations api.Value) string {
	value := customizations.Get("security", "firewall", "enabled")
	if value.Type() != api.Boolean {
		return ""
	}
	if value.Bool() {
		return "enabled"
	}
	return "disabled"
}

func weakDepsChoice(customizations api.Value) string {
	value := customizations.Get("packages", "installWeakDeps")
	if value.Type() != api.Boolean || value.Bool() {
		return ""
	}
	return "--exclude-weakdeps"
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
