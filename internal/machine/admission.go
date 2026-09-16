package machine

import (
	"fmt"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"unicode"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/substrate"
)

func Normalize(o api.Object, c api.Catalog) (api.Object, []api.Issue) {
	if o.Kind() == api.NetworkConfig {
		return o.WithSpec(normalizeConfiguration(o.Spec(), c)), nil
	}
	if o.Kind() != api.Machine {
		return o, nil
	}
	s := o.Spec()
	nics := s.Get("hardware", "nics").Items()
	for i, nic := range nics {
		if mac, ok := canonicalMAC(nic.Get("macAddress").Text()); ok {
			nics[i] = nic.With("macAddress", api.StringValue(mac))
		}
	}
	if s.Has("hardware", "nics") {
		s = s.WithPath(api.ListValue(nics...), "hardware", "nics")
	}
	network := s.Get("network")
	if network.Has("inline") {
		network = network.With("inline", normalizeConfiguration(network.Get("inline"), c))
	}
	if network.Has("overrides") {
		network = network.With("overrides", normalizeNativeMACs(network.Get("overrides")))
	}
	addresses := network.Get("addresses").Items()
	for i, address := range addresses {
		if prefix, err := netip.ParsePrefix(address.Get("address").Text()); err == nil {
			addresses[i] = address.With("address", api.StringValue(prefix.String()))
		} else if ip, err := netip.ParseAddr(address.Get("address").Text()); err == nil {
			addresses[i] = address.With("address", api.StringValue(ip.String()))
		}
	}
	if _, ok := namedValue(api.ListValue(addresses...), "fqdn"); !ok && interfaceIndex(addresses, "fqdn") == -1 {
		exists := false
		for _, a := range addresses {
			exists = exists || a.Get("name").Text() == "fqdn"
		}
		if envs := c.OfKind(api.Environment); !exists && len(envs) == 1 {
			domain := envs[0].Spec().Get("domains", "machines").Text()
			if domain == "" {
				domain = envs[0].Spec().Get("domains", "base").Text()
			}
			if domain != "" {
				addresses = append(addresses, api.MapValue(api.FieldValue{Name: "name", Value: api.StringValue("fqdn")}, api.FieldValue{Name: "address", Value: api.StringValue(o.Name() + "." + domain)}))
			}
		}
	}
	if len(addresses) != 0 || network.Has("addresses") {
		network = network.With("addresses", api.ListValue(addresses...))
	}
	provider, found := Provider(o, c)
	if found && network.Has("configRef") && !network.Has("attachmentRef") && !network.Has("interfaceAttachments") && provider.Spec().Get("networkAttachments").Len() == 1 {
		if _, ok := namedValue(provider.Spec().Get("networkAttachments"), network.Get("configRef").Text()); ok {
			network = network.With("attachmentRef", network.Get("configRef"))
		}
	}
	if network.Present() {
		s = s.With("network", network)
	}
	if bmc := s.Get("hardware", "management", "bmc"); bmc.Present() {
		if found && substrate.Variant(provider) == "baremetal" {
			defaults := provider.Spec().Get("baremetal", "defaults", "bmc")
			if bmc.Has("virtualMedia") {
				defaults = defaults.Without("virtualMedia")
			}
			bmc = inherit(bmc, defaults)
		}
		bmc = substrate.NormalizeBMCDefaults(bmc).Default("protocol", api.StringValue("redfish"))
		s = s.WithPath(bmc, "hardware", "management", "bmc")
	}
	o = o.WithSpec(s)
	if native, issues := ComposeNetwork(o, c); native.Present() && len(issues) == 0 {
		if binding, issues := resolveBindings(o, c, native); binding.Present() && len(issues) == 0 {
			network = network.With("interfaceBinding", binding)
		}
		if selected, issues := selectInstallAddress(o, c, false); selected.Present() && len(issues) == 0 {
			network = network.Default("installAddressRef", selected.Get("name"))
		}
	}
	if network.Present() {
		s = s.With("network", network)
	}
	access := s.Get("access")
	if installed(o) {
		if envs := c.OfKind(api.Environment); len(envs) == 1 && envs[0].Spec().Has("remoteMachinesAccessKey", "keyRef") {
			access = api.MapValue().WithPath(api.StringValue("bootwright"), "ssh", "user").WithPath(envs[0].Spec().Get("remoteMachinesAccessKey", "keyRef"), "ssh", "auth", "privateKeyRef")
		}
	} else if s.Get("os", "provided").Bool() && !access.Has("local") && !access.Has("ssh") {
		access = access.WithPath(api.MapValue(), "ssh", "auth", "operatorIdentity")
	}
	if access.Has("ssh") {
		ssh := access.Get("ssh").Default("port", api.IntegerValue("22"))
		if !ssh.Has("addressRef") {
			if _, ok := namedValue(network.Get("addresses"), "ssh"); ok {
				ssh = ssh.With("addressRef", api.StringValue("ssh"))
			} else {
				ssh = ssh.With("addressRef", api.StringValue("fqdn"))
			}
		}
		if !ssh.Has("user") && !ssh.Has("auth", "operatorIdentity") && !ssh.Has("auth", "passwordRef") {
			ssh = ssh.With("user", api.StringValue("root"))
		}
		access = access.With("ssh", ssh)
	}
	if access.Present() {
		s = s.With("access", access.Default("rootLogin", api.StringValue("keep")))
	}
	if installed(o) {
		install := s.Get("os", "install")
		if profile, ok := c.Find(api.MachineInstallProfile, s.Get("os", "installProfileRef").Text()); ok {
			if value := profile.Spec().Get("proxy"); value.Present() {
				s = s.Default("proxy", value)
			}
			if value := profile.Spec().Get("ntp"); value.Present() {
				install = install.Default("ntp", value)
			}
		}
		if install.Has("ntp") {
			install = install.With("ntp", infrastructureservices.NormalizeServerSelections(install.Get("ntp"), c, api.NTPServer))
		}
		if install.Present() {
			s = s.WithPath(install, "os", "install")
		}
	}
	if s.Get("os", "provided").Bool() || installed(o) {
		s = s.With("proxy", infrastructureservices.NormalizeProxy(s.Get("proxy"), c))
	}
	return o.WithSpec(s), nil
}

// NormalizationOrigins identifies profile choices inherited by an installed
// Machine without assigning a profile origin to intrinsic direct access.
func NormalizationOrigins(before, after api.Object, c api.Catalog) []api.FieldOrigin {
	if before.Kind() != api.Machine || !installed(before) {
		return nil
	}
	profile, ok := c.Find(api.MachineInstallProfile, before.Spec().Get("os", "installProfileRef").Text())
	if !ok {
		return nil
	}
	origins := []api.FieldOrigin{}
	if !before.Spec().Has("proxy") && profile.Spec().Has("proxy") && after.Spec().Has("proxy") {
		origins = append(origins, api.FieldOrigin{Field: "$.spec.proxy", SourceKind: api.MachineInstallProfile, SourceName: profile.Name(), SourceField: "$.spec.proxy"})
	}
	if !before.Spec().Has("os", "install", "ntp") && profile.Spec().Has("ntp") && after.Spec().Has("os", "install", "ntp") {
		origins = append(origins, api.FieldOrigin{Field: "$.spec.os.install.ntp", SourceKind: api.MachineInstallProfile, SourceName: profile.Name(), SourceField: "$.spec.ntp"})
	}
	return origins
}

func ValidateAuthored(o api.Object, c api.Catalog) []api.Issue {
	if o.Kind() == api.NetworkConfig {
		return validateNative(o.Spec().Get("nmstate"), "$.spec.nmstate", false)
	}
	if o.Kind() != api.Machine {
		return nil
	}
	s := o.Spec()
	issues := []api.Issue{}
	issues = appendIssues(issues, validateServiceIntent(o)...)
	if installed(o) && s.Has("access") {
		issues = appendIssues(issues, invariant("$.spec.access", "Bootwright-installed Machines must not author access"))
	}
	if s.Has("access", "ssh", "auth", "passwordRef") && !s.Has("access", "ssh", "user") {
		issues = appendIssues(issues, invariant("$.spec.access.ssh.user", "password authentication requires an authored user"))
	}
	if s.Get("access", "rootLogin").Text() == "revoke" && !s.Has("access", "ssh") {
		issues = appendIssues(issues, invariant("$.spec.access.rootLogin", "root-login revocation requires authored SSH access"))
	}
	issues = appendIssues(issues, substrate.ValidateBMCDefaults(s.Get("hardware", "management", "bmc"), "$.spec.hardware.management.bmc", true)...)
	issues = appendIssues(issues, validateNative(s.Get("network", "inline", "nmstate"), "$.spec.network.inline.nmstate", false)...)
	issues = appendIssues(issues, validateNative(s.Get("network", "overrides"), "$.spec.network.overrides", true)...)
	return issues
}

func Validate(o api.Object, c api.Catalog) []api.Issue {
	if o.Kind() == api.NetworkConfig {
		return validateConfiguration(o.Spec(), c, "$.spec")
	}
	if o.Kind() != api.Machine {
		return nil
	}
	s := o.Spec()
	issues := []api.Issue{}
	issues = appendIssues(issues, validateServices(o, c)...)
	if slices.Contains(s.Get("capabilities").Strings(), "ceph-arbiter") && !slices.Contains(s.Get("capabilities").Strings(), "ceph-node") {
		issues = appendIssues(issues, invariant("$.spec.capabilities", "ceph-arbiter requires ceph-node capability"))
	}
	if envs := c.OfKind(api.Environment); len(envs) == 1 && s.Has("placement", "site") {
		if _, ok := namedValue(envs[0].Spec().Get("sites"), s.Get("placement", "site").Text()); !ok {
			issues = appendIssues(issues, reference("$.spec.placement.site", "site must name an Environment site"))
		}
	}
	provided := s.Get("os", "provided")
	network := s.Get("network")
	if provided.Bool() {
		for _, key := range []string{"installProfileRef", "install"} {
			if s.Has("os", key) {
				issues = appendIssues(issues, invariant("$.spec.os."+key, "OS-ready Machines forbid installation declarations"))
			}
		}
		for _, key := range []string{"configRef", "inline", "attachmentRef", "interfaceAttachments", "interfaceBinding", "installAddressRef", "overrides"} {
			if network.Has(key) {
				issues = appendIssues(issues, invariant("$.spec.network."+key, "OS-ready Machines declare contacts only"))
			}
		}
	} else if provided.Present() && !s.Has("substrate", "providerRef") {
		issues = appendIssues(issues, invariant("$.spec.substrate.providerRef", "non-provided Machines require a substrate provider"))
	}
	provider, found := Provider(o, c)
	variant := substrate.Variant(provider)
	if found && variant != "" {
		profileRef := s.Get("substrate", "profileRef")
		if variant == "baremetal" && profileRef.Present() {
			issues = appendIssues(issues, invariant("$.spec.substrate.profileRef", "bare-metal Machines forbid a virtual machine profile"))
		}
		if variant != "baremetal" {
			if provided.Present() && !provided.Bool() && !profileRef.Present() {
				issues = appendIssues(issues, invariant("$.spec.substrate.profileRef", "virtual installation requires a provider-local machine profile"))
			}
			if profileRef.Present() {
				if _, ok := namedValue(provider.Spec().Get(variant, "machineProfiles"), profileRef.Text()); !ok {
					issues = appendIssues(issues, reference("$.spec.substrate.profileRef", "profile must name a machine profile on the selected provider"))
				}
			}
		}
		if variant == "baremetal" && provided.Present() && !provided.Bool() {
			issues = appendIssues(issues, validateBaremetal(o)...)
		}
	}
	issues = appendIssues(issues, validateHardware(o, c, variant)...)
	configured := network.Has("configRef") || network.Has("inline")
	if network.Has("overrides") && !network.Has("configRef") {
		issues = appendIssues(issues, invariant("$.spec.network.overrides", "network overrides require configRef"))
	}
	for _, key := range []string{"attachmentRef", "interfaceAttachments", "interfaceBinding", "installAddressRef"} {
		if network.Has(key) && !configured {
			issues = appendIssues(issues, invariant("$.spec.network."+key, "network selections and bindings require a configured network"))
		}
	}
	if network.Has("inline") {
		issues = appendIssues(issues, validateConfiguration(network.Get("inline"), c, "$.spec.network.inline")...)
	}
	for i, address := range network.Get("addresses").Items() {
		path := fmt.Sprintf("$.spec.network.addresses[%d]", i)
		if address.Has("interface") && (!configured || provided.Bool()) {
			issues = appendIssues(issues, invariant(path+".interface", "interface assignment requires a non-provided Machine with configured network"))
		}
		if address.Get("name").Text() == "fqdn" {
			if address.Has("interface") || !validDNS(address.Get("address").Text()) {
				issues = appendIssues(issues, invariant(path, "fqdn must be an unassigned DNS contact"))
			}
			for _, other := range c.OfKind(api.Machine) {
				if other.Identity() == o.Identity() {
					continue
				}
				for _, peer := range other.Spec().Get("network", "addresses").Items() {
					if peer.Get("name").Text() == "fqdn" && peer.Get("address").Equal(address.Get("address")) {
						issues = appendIssues(issues, invariant(path+".address", "Machine fqdn contacts must be unique"))
					}
				}
			}
		}
	}
	native, compositionIssues := ComposeNetwork(o, c)
	issues = appendIssues(issues, compositionIssues...)
	if found && configured {
		issues = appendIssues(issues, validateAttachments(o, provider, native)...)
	}
	if len(compositionIssues) == 0 {
		address, selectionIssues := selectInstallAddress(o, c, false)
		issues = appendIssues(issues, selectionIssues...)
		if address.Present() {
			prefix, _ := netip.ParsePrefix(address.Get("address").Text())
			for _, other := range c.OfKind(api.Machine) {
				if other.Identity() == o.Identity() {
					continue
				}
				peer, pi := selectInstallAddress(other, c, false)
				pp, e := netip.ParsePrefix(peer.Get("address").Text())
				if len(pi) == 0 && e == nil && prefix.Addr() == pp.Addr() {
					issues = appendIssues(issues, invariant("$.spec.network.installAddressRef", "selected installation IPs must be unique across Machines"))
					break
				}
			}
			if found && variant == "libvirt" {
				issues = appendIssues(issues, validateManagedAttachmentContainment(prefix, network, provider)...)
			}
		}
		if installed(o) {
			if profile, ok := c.Find(api.MachineInstallProfile, s.Get("os", "installProfileRef").Text()); ok && profile.Spec().Has("installer", "anaconda") {
				issues = appendIssues(issues, validateAnacondaNetwork(address, native)...)
			}
		}
	}
	if local := s.Get("access", "local"); local.Present() && (!local.Bool() || !provided.Bool()) {
		issues = appendIssues(issues, invariant("$.spec.access.local", "local access must be true and requires an OS-ready Machine"))
	}
	if ssh := s.Get("access", "ssh"); ssh.Present() {
		if _, ok := namedValue(network.Get("addresses"), ssh.Get("addressRef").Text()); ssh.Has("addressRef") && !ok {
			issues = appendIssues(issues, reference("$.spec.access.ssh.addressRef", "SSH address must name a Machine contact or assignment"))
		}
	}
	if s.Get("access", "rootLogin").Text() == "revoke" && (installed(o) || !successorLogin(o, c)) {
		issues = appendIssues(issues, invariant("$.spec.access.rootLogin", "revoking root login requires a managed storage-cluster non-root successor login"))
	}
	issues = appendIssues(issues, validateHostKey(o, c)...)
	issues = appendIssues(issues, substrate.ValidateBMCDefaults(s.Get("hardware", "management", "bmc"), "$.spec.hardware.management.bmc", false)...)
	if bmc := s.Get("hardware", "management", "bmc"); bmc.Present() {
		if !bmc.Has("credentialsRef") {
			issues = appendIssues(issues, invariant("$.spec.hardware.management.bmc.credentialsRef", "BMC credentials are required after provider inheritance"))
		}
		if bmc.Has("address") && !validBMC(bmc.Get("address").Text(), false) {
			issues = appendIssues(issues, invariant("$.spec.hardware.management.bmc.address", "BMC address must be an absolute Redfish endpoint without embedded credentials"))
		}
	}
	return issues
}

// validateManagedAttachmentContainment requires the install address to lie
// inside the network Bootwright defines for a managed libvirt attachment.
func validateManagedAttachmentContainment(address netip.Prefix, network api.Value, provider api.Object) []api.Issue {
	attachment, ok := namedValue(provider.Spec().Get("networkAttachments"), network.Get("attachmentRef").Text())
	if !ok || attachment.Get("libvirt", "management").Text() != "managed" {
		return nil
	}
	managed, err := netip.ParsePrefix(attachment.Get("libvirt", "address").Text())
	if err != nil || managed.Masked().Contains(address.Addr()) {
		return nil
	}
	return []api.Issue{invariant("$.spec.network.installAddressRef", "install address must lie inside the managed libvirt attachment's network")}
}

// validateHostKey keeps a delivered host key to the one lifecycle that
// delivers it, and to one Machine. A host key identifies exactly one machine,
// so two Machines sharing one would each satisfy the other's completion proof.
func validateHostKey(o api.Object, c api.Catalog) []api.Issue {
	reference := o.Spec().Get("os", "install", "hostKeyRef")
	if !reference.Present() {
		return nil
	}
	provider, found := Provider(o, c)
	if !installed(o) || !found || substrate.Variant(provider) != "baremetal" {
		return []api.Issue{invariant("$.spec.os.install.hostKeyRef",
			"only a bare-metal Bootwright-installed Machine delivers its own SSH host key")}
	}
	for _, other := range c.OfKind(api.Machine) {
		if other.Identity() != o.Identity() && other.Spec().Get("os", "install", "hostKeyRef").Equal(reference) {
			return []api.Issue{invariant("$.spec.os.install.hostKeyRef",
				"an SSH host key identifies one Machine and cannot be shared")}
		}
	}
	return nil
}

func validateBaremetal(o api.Object) []api.Issue {
	s := o.Spec()
	issues := []api.Issue{}
	if s.Get("hardware", "nics").Len() == 0 {
		issues = appendIssues(issues, invariant("$.spec.hardware.nics", "bare-metal installation requires hardware NICs"))
	}
	for i, nic := range s.Get("hardware", "nics").Items() {
		if !nic.Has("macAddress") {
			issues = appendIssues(issues, invariant(fmt.Sprintf("$.spec.hardware.nics[%d].macAddress", i), "every bare-metal install NIC requires a MAC"))
		}
	}
	if _, ok := namedValue(s.Get("hardware", "nics"), s.Get("hardware", "boot", "nicRef").Text()); !ok {
		issues = appendIssues(issues, reference("$.spec.hardware.boot.nicRef", "bare-metal boot requires a declared NIC"))
	}
	bmc := s.Get("hardware", "management", "bmc")
	if !bmc.Present() {
		issues = appendIssues(issues, invariant("$.spec.hardware.management.bmc", "bare-metal installation requires a BMC"))
	} else if !validBMC(bmc.Get("address").Text(), true) {
		issues = appendIssues(issues, invariant("$.spec.hardware.management.bmc.address", "bare-metal BMC address must select one exact Redfish ComputerSystem"))
	}
	// A physical machine offers no channel to read back what it holds, so the
	// key it will answer with is declared here and delivered by the
	// installation rather than discovered afterwards.
	if installed(o) && !s.Has("os", "install", "hostKeyRef") {
		issues = appendIssues(issues, invariant("$.spec.os.install.hostKeyRef", "bare-metal installation requires the SSH host key it delivers"))
	}
	hints := s.Get("os", "install", "rootDeviceHints")
	if !hints.Has("deviceName") && !hints.Has("wwn") {
		issues = appendIssues(issues, invariant("$.spec.os.install.rootDeviceHints", "bare-metal installation requires deviceName or wwn as its target selector"))
	}
	return issues
}

func validateHardware(o api.Object, c api.Catalog, variant string) []api.Issue {
	issues := []api.Issue{}
	seen := map[string]bool{}
	for i, nic := range o.Spec().Get("hardware", "nics").Items() {
		mac, ok := canonicalMAC(nic.Get("macAddress").Text())
		if !ok {
			continue
		}
		path := fmt.Sprintf("$.spec.hardware.nics[%d].macAddress", i)
		if seen[mac] {
			issues = appendIssues(issues, invariant(path, "canonical hardware MACs must be unique"))
		}
		seen[mac] = true
		if variant == "vsphere" && (mac < "00:50:56:00:00:00" || mac > "00:50:56:3f:ff:ff") {
			issues = appendIssues(issues, invariant(path, "vSphere MAC must be in the manual assignment range"))
		}
		for _, other := range c.OfKind(api.Machine) {
			if other.Identity() == o.Identity() {
				continue
			}
			for _, peer := range other.Spec().Get("hardware", "nics").Items() {
				pm, ok := canonicalMAC(peer.Get("macAddress").Text())
				if ok && pm == mac {
					issues = appendIssues(issues, invariant(path, "authored hardware MACs must be unique across Machines"))
				}
			}
		}
	}
	if ref := o.Spec().Get("hardware", "boot", "nicRef"); ref.Present() {
		if _, ok := namedValue(o.Spec().Get("hardware", "nics"), ref.Text()); !ok {
			issues = appendIssues(issues, reference("$.spec.hardware.boot.nicRef", "boot NIC must name a declared hardware NIC"))
		}
	}
	return issues
}

func validateAttachments(o, provider api.Object, native api.Value) []api.Issue {
	network := o.Spec().Get("network")
	variant := substrate.Variant(provider)
	issues := []api.Issue{}
	if network.Has("attachmentRef") && network.Has("interfaceAttachments") {
		issues = appendIssues(issues, invariant("$.spec.network.interfaceAttachments", "attachmentRef and interfaceAttachments are mutually exclusive"))
	}
	if !network.Has("attachmentRef") && !network.Has("interfaceAttachments") {
		issues = appendIssues(issues, invariant("$.spec.network.attachmentRef", "provider-backed configured networks require an explicit attachment unless the unique matching default applies"))
	}
	check := func(ref api.Value, path string) {
		if ref.Present() {
			a, ok := namedValue(provider.Spec().Get("networkAttachments"), ref.Text())
			if !ok || !a.Has(variant) {
				issues = appendIssues(issues, reference(path, "attachment must name a matching arm on the selected provider"))
			}
		}
	}
	check(network.Get("attachmentRef"), "$.spec.network.attachmentRef")
	if network.Has("interfaceAttachments") {
		if variant != "kubevirt" {
			issues = appendIssues(issues, invariant("$.spec.network.interfaceAttachments", "per-interface attachments require KubeVirt"))
		}
		seen := map[string]bool{}
		for i, entry := range network.Get("interfaceAttachments").Items() {
			path := fmt.Sprintf("$.spec.network.interfaceAttachments[%d]", i)
			check(entry.Get("attachmentRef"), path+".attachmentRef")
			name := entry.Get("interface").Text()
			if seen[name] {
				issues = appendIssues(issues, invariant(path+".interface", "physical interfaces must be attached once"))
			}
			seen[name] = true
			if native.Present() {
				iface, ok := namedValue(native.Get("interfaces"), name)
				if !ok || iface.Get("type").Text() != "ethernet" || unavailableInterface(iface) {
					issues = appendIssues(issues, reference(path+".interface", "attachment must name an available physical ethernet interface"))
				}
			}
		}
		for _, iface := range native.Get("interfaces").Items() {
			if iface.Get("type").Text() == "ethernet" && !unavailableInterface(iface) && !seen[iface.Get("name").Text()] {
				issues = appendIssues(issues, invariant("$.spec.network.interfaceAttachments", "per-interface attachments must cover every physical interface"))
			}
		}
	}
	return issues
}

func validateAnacondaNetwork(address, native api.Value) []api.Issue {
	if !native.Present() {
		return nil
	}
	if address.Present() {
		prefix, err := netip.ParsePrefix(address.Get("address").Text())
		if err == nil && !prefix.Addr().Is4() {
			return []api.Issue{invariant("$.spec.network.installAddressRef", "Anaconda installation requires static IPv4 or DHCP")}
		}
		iface, ok := namedValue(native.Get("interfaces"), address.Get("interface").Text())
		if ok && !slices.Contains([]string{"ethernet", "vlan", "bond"}, iface.Get("type").Text()) {
			return []api.Issue{invariant("$.spec.network.installAddressRef", "Anaconda static install interface must be ethernet, vlan, or bond")}
		}
		return nil
	}
	for _, iface := range native.Get("interfaces").Items() {
		if !unavailableInterface(iface) && iface.Get("ipv4", "dhcp").Bool() {
			return nil
		}
	}
	return []api.Issue{invariant("$.spec.network", "Anaconda install networking requires DHCP or a static IPv4 installation address")}
}

func normalizeConfiguration(config api.Value, c api.Catalog) api.Value {
	if config.Has("dns") {
		config = config.With("dns", infrastructureservices.NormalizeServerSelections(config.Get("dns"), c, api.DNSServer))
	}
	networks := config.Get("machineNetwork").Items()
	for i, network := range networks {
		if prefix, err := netip.ParsePrefix(network.Get("cidr").Text()); err == nil {
			networks[i] = network.With("cidr", api.StringValue(prefix.Masked().String()))
		}
	}
	if config.Has("machineNetwork") {
		config = config.With("machineNetwork", api.ListValue(networks...))
	}
	if config.Has("nmstate") {
		config = config.With("nmstate", normalizeNativeMACs(config.Get("nmstate")))
	}
	return config
}

func normalizeNativeMACs(native api.Value) api.Value {
	interfaces := native.Get("interfaces").Items()
	for i, iface := range interfaces {
		if mac, ok := canonicalMAC(iface.Get("mac-address").Text()); ok {
			interfaces[i] = iface.With("mac-address", api.StringValue(mac))
		}
	}
	if native.Has("interfaces") {
		native = native.With("interfaces", api.ListValue(interfaces...))
	}
	return native
}

func validateConfiguration(config api.Value, c api.Catalog, path string) []api.Issue {
	issues := validateNative(config.Get("nmstate"), path+".nmstate", false)
	seen := map[netip.Prefix]bool{}
	for i, network := range config.Get("machineNetwork").Items() {
		prefix, err := netip.ParsePrefix(network.Get("cidr").Text())
		if err == nil {
			prefix = prefix.Masked()
			if seen[prefix] {
				issues = appendIssues(issues, invariant(fmt.Sprintf("%s.machineNetwork[%d].cidr", path, i), "machine networks must have unique canonical CIDRs"))
			}
			seen[prefix] = true
		}
	}
	issues = appendIssues(issues, infrastructureservices.ValidateServerSelections(config.Get("dns"), c, api.DNSServer, path+".dns")...)
	managed := map[string]bool{}
	for _, selection := range config.Get("dns").Items() {
		if server, ok := c.Find(api.DNSServer, selection.Get("serverRef").Text()); ok && server.Spec().Get("management").Text() == "managed" {
			managed[server.Name()] = true
		}
	}
	if len(managed) > 1 {
		issues = appendIssues(issues, invariant(path+".dns", "a network may consume at most one distinct managed DNS server"))
	}
	return issues
}

func validateServiceIntent(o api.Object) []api.Issue {
	s := o.Spec()
	issues := infrastructureservices.ValidateProxyChoice(s.Get("proxy"), "$.spec.proxy")
	if provided := s.Get("os", "provided"); s.Has("proxy") && provided.Present() && !provided.Bool() && !installed(o) {
		issues = appendIssues(issues, invariant("$.spec.proxy", "installer-provisioned Machines use their cluster's installation proxy choice"))
	}
	if s.Has("os", "install", "ntp") && !installed(o) {
		issues = appendIssues(issues, invariant("$.spec.os.install.ntp", "installation NTP choices require a Bootwright-installed Machine with installProfileRef"))
	}
	return issues
}

func validateServices(o api.Object, c api.Catalog) []api.Issue {
	s := o.Spec()
	issues := validateServiceIntent(o)
	issues = appendIssues(issues, infrastructureservices.ValidateProxy(s.Get("proxy"), c, "$.spec.proxy", installed(o))...)
	issues = appendIssues(issues, infrastructureservices.ValidateServerSelections(s.Get("os", "install", "ntp"), c, api.NTPServer, "$.spec.os.install.ntp")...)
	return issues
}

func successorLogin(o api.Object, c api.Catalog) bool {
	for _, cluster := range c.OfKind(api.StorageCluster) {
		if cluster.Spec().Get("management").Text() != "managed" {
			continue
		}
		ssh := cluster.Spec().Get("ceph", "cephadm", "clusterSSH")
		user := ssh.Get("user").Text()
		if user == "" || user == "root" || !ssh.Has("keyRef") {
			continue
		}
		for _, node := range cluster.Spec().Get("ceph", "topology", "nodes").Items() {
			if node.Get("machineRef").Text() == o.Name() {
				return true
			}
		}
	}
	return false
}
func installed(o api.Object) bool {
	return o.Spec().Get("os", "provided").Present() && !o.Spec().Get("os", "provided").Bool() && o.Spec().Has("os", "installProfileRef")
}
func inherit(local, defaults api.Value) api.Value {
	for _, f := range defaults.Fields() {
		if !local.Has(f.Name) {
			local = local.With(f.Name, f.Value)
		} else if f.Value.Type() == api.Mapping && local.Get(f.Name).Type() == api.Mapping {
			local = local.With(f.Name, inherit(local.Get(f.Name), f.Value))
		}
	}
	return local
}
func validBMC(raw string, system bool) bool {
	invalidCharacter := func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }
	if strings.IndexFunc(raw, invalidCharacter) >= 0 {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || !slices.Contains([]string{"http", "https", "redfish+http", "redfish+https", "redfish-virtualmedia+http", "redfish-virtualmedia+https"}, u.Scheme) {
		return false
	}
	if !system {
		return true
	}
	const prefix = "/redfish/v1/Systems/"
	if !strings.HasPrefix(u.Path, prefix) {
		return false
	}
	id := strings.TrimPrefix(u.Path, prefix)
	return id != "" && id != "." && id != ".." && !strings.Contains(id, "/") && strings.IndexFunc(id, invalidCharacter) < 0 && u.RawPath == ""
}
func validDNS(s string) bool {
	if len(s) == 0 || len(s) > 253 || strings.HasSuffix(s, ".") {
		return false
	}
	for _, label := range strings.Split(s, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}
