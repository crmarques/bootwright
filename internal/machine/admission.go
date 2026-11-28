package machine

import (
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/machine/diskselection"
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
	if s.Has("hardware", "nics") {
		nics := s.Get("hardware", "nics").Items()
		for i, nic := range nics {
			if mac, ok := canonicalMAC(nic.Get("macAddress").Text()); ok {
				nics[i] = nic.With("macAddress", api.StringValue(mac))
			}
		}
		s = s.WithPath(api.ListValue(nics...), "hardware", "nics")
	}
	network := normalizedContacts(o, c, s.Get("network"))
	provider, found := Provider(o, c)
	if found && substrate.VirtualArm(substrate.Variant(provider)) && s.Get("hardware", "nics").Len() > 0 && !api.MachineProvided(o) {
		if entries, ok := defaultedNICSegments(c, o, provider); ok {
			s = s.WithPath(entries, "hardware", "nics")
		}
	}
	if network.Present() {
		s = s.With("network", network)
	}
	if bmc := s.Get("hardware", "bmc"); bmc.Present() {
		if found && substrate.RealizesPhysicalNICs(provider) {
			bmc = inherit(bmc, providerBMCDefaults(bmc, provider))
		}
		s = s.WithPath(substrate.NormalizeBMCDefaults(bmc), "hardware", "bmc")
	}
	if found && substrate.RealizesPhysicalNICs(provider) && !api.MachineProvided(o) && len(substrate.MachineRootDeviceHints(o.WithSpec(s)).Names()) == 0 {
		s = s.WithPath(api.MapValue(), "hardware", "rootDisk", "bootVolume")
	}
	o = o.WithSpec(s)
	if native, issues := ComposeNetwork(o, c); native.Present() && len(issues) == 0 {
		if matches, issues := resolveMatches(o, c, native); matches.Len() > 0 && len(issues) == 0 {
			network = network.With("interfaceMatches", matches)
		}
		if selected, issues := selectInstallAddress(o, c, false); selected.Present() && len(issues) == 0 {
			network = network.Default("installAddressRef", selected.Get("name"))
		}
	}
	if network.Present() {
		s = s.With("network", network)
	}
	if access := normalizedAccess(o, c, s, network); access.Present() {
		s = s.With("access", access.Default("rootLogin", api.StringValue("keep")))
	}
	return o.WithSpec(normalizedInstallation(o, c, s)), nil
}

func normalizedContacts(o api.Object, c api.Catalog, network api.Value) api.Value {
	if network.Has("inline") {
		network = network.With("inline", normalizeConfiguration(network.Get("inline"), c))
	}
	if network.Has("overrides") {
		network = network.With("overrides", normalizeNativeMACs(network.Get("overrides")))
	}
	addresses := network.Get("addresses").Items()
	for i, address := range addresses {
		if ip, err := netip.ParseAddr(address.Get("address").Text()); err == nil {
			addresses[i] = address.With("address", api.StringValue(ip.String()))
		}
	}
	if contact, ok := derivedContact(o, c, addresses); ok && !contact.overlong {
		addresses = append(addresses, api.MapValue(api.FieldValue{Name: "name", Value: api.StringValue("fqdn")}, api.FieldValue{Name: "address", Value: api.StringValue(contact.name)}))
	}
	if len(addresses) != 0 || network.Has("addresses") {
		network = network.With("addresses", api.ListValue(addresses...))
	}
	return network
}

type contact struct {
	name     string
	overlong bool
}

// derivedContact is the fqdn contact normalization derives for a Machine that
// declares none. An over-long one, composed of a valid name and domain, is
// refused at the name and never appended.
func derivedContact(o api.Object, c api.Catalog, addresses []api.Value) (contact, bool) {
	for _, a := range addresses {
		if a.Get("name").Text() == "fqdn" {
			return contact{}, false
		}
	}
	envs := c.OfKind(api.Environment)
	if len(envs) != 1 || !api.ValidLexical(api.LexicalName, o.Name()) {
		return contact{}, false
	}
	domain := envs[0].Spec().Get("domains", "machines").Text()
	if domain == "" {
		domain = envs[0].Spec().Get("domains", "base").Text()
	}
	if domain == "" {
		return contact{}, false
	}
	name := o.Name() + "." + domain
	return contact{name: name, overlong: api.ValidLexical(api.LexicalDNS, domain) && !api.ValidLexical(api.LexicalDNS, name)}, true
}

func validateDerivedContact(o api.Object, c api.Catalog) []api.Issue {
	derived, ok := derivedContact(o, c, o.Spec().Get("network", "addresses").Items())
	if !ok || !derived.overlong {
		return nil
	}
	return []api.Issue{invariant("$.metadata.name", "the derived fqdn contact "+derived.name+" exceeds a DNS name's 253 bytes or a label's 63",
		"shorten the name of "+o.Identity()+" or the Environment's spec.domains.machines (or spec.domains.base, its default), or author an fqdn contact in spec.network.addresses")}
}

func normalizedAccess(o api.Object, c api.Catalog, s, network api.Value) api.Value {
	access := s.Get("access")
	if installed(o) {
		if key, ok := FleetKey(c); ok {
			access = api.MapValue().WithPath(api.StringValue("bootwright"), "ssh", "user").WithPath(api.StringValue(key), "ssh", "auth", "privateKeyRef")
		}
	} else if api.MachineProvided(o) && !access.Has("local") && !access.Has("ssh") {
		access = access.WithPath(api.MapValue(), "ssh", "auth", "operatorIdentity")
	}
	if access.Has("ssh") {
		ssh := access.Get("ssh").Default("port", api.IntegerValue("22"))
		// A name that is not a DNS label derives no fqdn contact, so its session
		// falls back to none rather than repeat the name's refusal.
		reference := sshAddressRef(o, network)
		derived, _ := derivedContact(o, c, network.Get("addresses").Items())
		if _, declared := namedValue(network.Get("addresses"), reference); !ssh.Has("addressRef") && (declared || api.ValidLexical(api.LexicalName, o.Name()) && !derived.overlong) {
			ssh = ssh.With("addressRef", api.StringValue(reference))
		}
		if derived.overlong && ssh.Get("addressRef").Text() == "fqdn" {
			ssh = ssh.Without("addressRef")
		}
		if !ssh.Has("user") && !ssh.Has("auth", "operatorIdentity") && !ssh.Has("auth", "passwordRef") {
			ssh = ssh.With("user", api.StringValue("root"))
		}
		access = access.With("ssh", ssh)
	}
	return access
}

// FleetKey is the Environment's fleet key when it names an sshKeyPair Secret,
// the only key an installed Machine's derived access can use. Any other key is
// the Environment's own refusal, which a derived reference would only repeat
// with a remedy admission forbids on an installed Machine.
func FleetKey(c api.Catalog) (string, bool) {
	envs := c.OfKind(api.Environment)
	if len(envs) != 1 {
		return "", false
	}
	key := envs[0].Spec().Get("remoteMachinesAccessKey", "keyRef").Text()
	secret, found := c.Find(api.Secret, key)
	return key, found && secret.Spec().Get("type").Text() == "sshKeyPair"
}

// sshAddressRef names the address a session dials when access authors none.
// An installed Machine's installation proves its host key at the install
// address, which the controller routes to, so that address is the one a
// session can prove; a name only a resolver knows would dial an endpoint the
// proof does not cover. An authored ssh address still wins, and an OS-ready
// Machine has no installation to prove anything. An install address the
// Machine does not declare is refused where it is authored, so the session
// falls back to the FQDN rather than repeat that refusal.
func sshAddressRef(o api.Object, network api.Value) string {
	if _, ok := namedValue(network.Get("addresses"), "ssh"); ok {
		return "ssh"
	}
	if reference := network.Get("installAddressRef").Text(); installed(o) && reference != "" {
		if _, declared := namedValue(network.Get("addresses"), reference); declared {
			return reference
		}
	}
	return "fqdn"
}

func normalizedInstallation(o api.Object, c api.Catalog, s api.Value) api.Value {
	if installed(o) {
		install := s.Get("os", "install")
		if profile, ok := c.Find(api.MachineInstallProfile, api.MachineInstallProfileRef(o)); ok {
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
	if api.MachineProvided(o) && s.Has("os", "provided", "ntp") {
		s = s.WithPath(infrastructureservices.NormalizeServerSelections(s.Get("os", "provided", "ntp"), c, api.NTPServer), "os", "provided", "ntp")
	}
	if api.MachineProvided(o) || installed(o) {
		s = s.With("proxy", infrastructureservices.NormalizeProxy(s.Get("proxy"), c))
	}
	return s
}

// NormalizationOrigins identifies profile choices inherited by an installed
// Machine without assigning a profile origin to intrinsic direct access.
func NormalizationOrigins(before, after api.Object, c api.Catalog) []api.FieldOrigin {
	if before.Kind() != api.Machine || !installed(before) {
		return nil
	}
	profile, ok := c.Find(api.MachineInstallProfile, api.MachineInstallProfileRef(before))
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
	issues = api.AppendIssues(issues, validateServiceIntent(o)...)
	if installed(o) && s.Has("access") {
		issues = api.AppendIssues(issues, invariant("$.spec.access", "Bootwright-installed Machines must not author access", "remove spec.access; Bootwright derives an installed Machine's access from the fleet key"))
	}
	if s.Has("access", "ssh", "auth", "passwordRef") && !s.Has("access", "ssh", "user") {
		issues = api.AppendIssues(issues, invariant("$.spec.access.ssh.user", "password authentication requires an authored user", "set spec.access.ssh.user, or authenticate with spec.access.ssh.auth.privateKeyRef instead of a password"))
	}
	issues = api.AppendIssues(issues, substrate.ValidateBMCDefaults(substrate.MachineBMC(o), "$."+substrate.BMCPath, true)...)
	issues = api.AppendIssues(issues, validateNative(s.Get("network", "inline", "nmstate"), "$.spec.network.inline.nmstate", false)...)
	issues = api.AppendIssues(issues, validateNative(s.Get("network", "overrides"), "$.spec.network.overrides", true)...)
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
	issues = api.AppendIssues(issues, validateServices(o, c)...)
	if envs := c.OfKind(api.Environment); len(envs) == 1 && s.Has("site") {
		if _, ok := namedValue(envs[0].Spec().Get("sites"), s.Get("site").Text()); !ok {
			issues = api.AppendIssues(issues, api.ReferenceIssue("$.spec.site", "site must name an Environment site", "set spec.site to a site declared in the Environment's spec.sites"))
		}
	}
	provider, found := Provider(o, c)
	variant := substrate.Variant(provider)
	issues = api.AppendIssues(issues, validateProvision(o, variant)...)
	if substrate.MachineLinuxPreflight(o).Present() && (variant != substrate.ArmBaremetal || !api.MachineClusterInstalled(o)) {
		issues = api.AppendIssues(issues, invariant("$."+substrate.RootDiskPath+".linuxPreflight",
			"Linux root preflight requires a physical Machine installed by the cluster installer", "remove linuxPreflight or use a bare-metal cluster-installed Machine"))
	}
	armIssues := validateArm(o, found, variant)
	issues = api.AppendIssues(issues, armIssues...)
	if found && variant != "" && len(armIssues) == 0 {
		issues = api.AppendIssues(issues, validateSubstrate(c, o, provider, variant)...)
	}
	issues = api.AppendIssues(issues, validateSegmentRefs(c, o, provider, found && len(armIssues) == 0)...)
	issues = api.AppendIssues(issues, validateHardware(o, c, variant)...)
	if !substrate.VirtualArm(variant) {
		issues = api.AppendIssues(issues, validateDisks(o, variant == substrate.ArmBaremetal)...)
		issues = api.AppendIssues(issues, validateVolumeRoot(o, variant)...)
		issues = api.AppendIssues(issues, validateRootSize(o)...)
	}
	issues = api.AppendIssues(issues, validateSelectingSerials(o, c)...)
	network := s.Get("network")
	configured := network.Has("configRef") || network.Has("inline")
	issues = api.AppendIssues(issues, validateNetworkContacts(o, c, configured)...)
	issues = api.AppendIssues(issues, validateDerivedContact(o, c)...)
	issues = api.AppendIssues(issues, validateInstallNetwork(o, c, provider, found, variant, configured)...)
	issues = api.AppendIssues(issues, validateNetworkMembership(o, c)...)
	issues = api.AppendIssues(issues, validateAccess(o, c)...)
	issues = api.AppendIssues(issues, validateFirewall(o, c)...)
	issues = api.AppendIssues(issues, validateProvidedUpdate(o, c)...)
	issues = api.AppendIssues(issues, validateProvidedRegistration(o, c)...)
	issues = api.AppendIssues(issues, validateProvidedTime(o, c)...)
	issues = api.AppendIssues(issues, validatePlacementHost(o, c)...)
	issues = api.AppendIssues(issues, validateHostKey(o, c)...)
	return api.AppendIssues(issues, validateBMC(substrate.MachineBMC(o), s.Has("hardware", "providerRef") && !found)...)
}

func validateProvision(o api.Object, variant substrate.Arm) []api.Issue {
	var issues []api.Issue
	s := o.Spec()
	network := s.Get("network")
	if api.MachineProvided(o) {
		for _, key := range []string{"configRef", "inline", "interfaceMatches", "installAddressRef", "overrides"} {
			if network.Has(key) {
				issues = api.AppendIssues(issues, invariant("$.spec.network."+key, "OS-ready Machines declare contacts only", "remove spec.network."+key+"; an OS-ready Machine declares contacts only"))
			}
		}
		if substrate.VirtualArm(variant) {
			for i, nic := range s.Get("hardware", "nics").Items() {
				if nic.Has("segmentRef") {
					path := fmt.Sprintf("spec.hardware.nics[%d].segmentRef", i)
					issues = api.AppendIssues(issues, invariant("$."+path, "OS-ready Machines declare contacts only", "remove "+path+"; an OS-ready Machine declares contacts only"))
				}
			}
		}
		if !substrate.VirtualArm(variant) {
			for _, name := range substrate.MachineRootDeviceHints(o).Names() {
				if path := substrate.RootDeviceHintPath(name); !slices.Contains([]string{"deviceName", "wwn", "sizeBytes", "volumeRef", "bootVolume", "discover"}, name) {
					issues = api.AppendIssues(issues, api.Issue{Code: "api.deferred", Field: "$." + path,
						Message:     "nothing installs an OS-ready Machine, so " + path + " selects no disk; only hints.deviceName and wwn name its root disk, to keep it out of Ceph OSD selections",
						Remediation: "remove " + path + ", or keep it knowing nothing reads it on an OS-ready Machine"})
				}
			}
		}
	} else if (api.MachineInstalled(o) || api.MachineClusterInstalled(o)) && !s.Has("hardware", "providerRef") {
		issues = api.AppendIssues(issues, invariant("$."+substrate.ProviderPath, "an installed Machine requires the hardware provider that realizes it", "set "+substrate.ProviderPath+" to the InfraProvider that realizes this Machine, or declare os.provided: {} for a Machine whose OS is already installed"))
	}
	return issues
}

func validateArm(o api.Object, found bool, variant substrate.Arm) []api.Issue {
	s := o.Spec()
	issues := []api.Issue{}
	if s.Has("hardware", "profileRef") {
		path := "$." + substrate.ProfilePath
		switch {
		case !s.Has("hardware", "providerRef"):
			issues = append(issues, invariant(path, "a machine profile belongs to the Machine's provider, and "+o.Identity()+" names none", "set "+substrate.ProviderPath+" to the provider whose machineProfiles declares it, or remove "+substrate.ProfilePath))
		case found && variant == substrate.ArmBaremetal:
			issues = append(issues, invariant(path, "a bare-metal provider realizes no machine profile; the hardware itself is the machine", "remove "+substrate.ProfilePath+" from "+o.Identity()))
		}
	}
	if !found || !substrate.VirtualArm(variant) {
		return issues
	}
	for _, key := range []struct{ field, reason string }{
		{"bmc", "the provider controls the machine it creates, so it declares no BMC"},
		{"firmware", "the provider creates the machine's firmware, so it declares no firmware settings"},
		{"rootDisk", "the provider creates the root disk, and every installer on it writes " + substrate.VirtualRootDevice(variant)},
		{"dataDisks", "the provider creates the machine's data disks from its machine profile"},
	} {
		if s.Has("hardware", key.field) {
			path := substrate.HardwarePath + "." + key.field
			issues = append(issues, invariant("$."+path, "a "+string(variant)+" Machine declares no "+path+": "+key.reason, "remove "+path+" from "+o.Identity()))
		}
	}
	if variant == substrate.ArmVSphere {
		return issues
	}
	for i, nic := range s.Get("hardware", "nics").Items() {
		if nic.Has("macAddress") {
			path := fmt.Sprintf("%s[%d].macAddress", substrate.NICsPath, i)
			issues = append(issues, invariant("$."+path, "a "+string(variant)+" provider derives each vNIC's MAC from the context, Machine and NIC names", "remove "+path+" from "+o.Identity()))
		}
	}
	return issues
}

func validateSubstrate(c api.Catalog, o, provider api.Object, variant substrate.Arm) []api.Issue {
	var issues []api.Issue
	s := o.Spec()
	installs := api.MachineInstalled(o) || api.MachineClusterInstalled(o)
	if variant != substrate.ArmBaremetal && installs && !s.Has("hardware", "profileRef") {
		issues = api.AppendIssues(issues, invariant("$."+substrate.ProfilePath, "virtual installation requires a provider-local machine profile", "set "+substrate.ProfilePath+" to a machine profile of the selected provider"))
	}
	if api.MachineClusterInstalled(o) && !substrate.SupportsClusterMachines(variant) {
		issues = append(issues, invariant("$.spec.os.clusterInstalled", "the provider supports standalone Machine installation only", "select spec.os.install with a standalone MachineInstallProfile"))
	}
	if substrate.VirtualArm(variant) && installs {
		issues = api.AppendIssues(issues, validateVirtualNICs(c, o, provider, variant)...)
	}
	if variant == substrate.ArmBaremetal && installs {
		issues = api.AppendIssues(issues, validateBaremetal(c, o)...)
	}
	return issues
}

func validateNetworkContacts(o api.Object, c api.Catalog, configured bool) []api.Issue {
	var issues []api.Issue
	provided := api.MachineProvided(o)
	network := o.Spec().Get("network")
	if network.Has("overrides") && !network.Has("configRef") {
		issues = api.AppendIssues(issues, invariant("$.spec.network.overrides", "network overrides require configRef", "select a reusable configuration with spec.network.configRef, or remove spec.network.overrides"))
	}
	for _, key := range []string{"interfaceMatches", "installAddressRef"} {
		if key == "installAddressRef" && anacondaInstalled(o, c) {
			continue
		}
		path := "spec.network." + key
		if network.Has(key) && !configured && !provided {
			issues = api.AppendIssues(issues, api.Issue{Code: "api.invariant", Field: "$." + path, Message: "network selections and bindings require a configured network",
				Remediation: "select a network configuration on " + o.Identity() + " with spec.network.configRef or spec.network.inline, or remove " + path})
		}
	}
	if network.Has("inline") {
		issues = api.AppendIssues(issues, validateConfiguration(network.Get("inline"), c, "$.spec.network.inline")...)
	}
	for i, address := range network.Get("addresses").Items() {
		path := fmt.Sprintf("$.spec.network.addresses[%d]", i)
		if address.Has("interface") && (!configured || provided) {
			remediation := "select a network configuration on " + o.Identity() + " with spec.network.configRef or spec.network.inline, or remove " + path[2:] + ".interface"
			if provided {
				remediation = "remove " + path[2:] + ".interface from " + o.Identity() + "; an OS-ready Machine declares contacts only"
			}
			issues = api.AppendIssues(issues, api.Issue{Code: "api.invariant", Field: path + ".interface", Message: "interface assignment requires a non-provided Machine with configured network", Remediation: remediation})
		}
		if address.Get("name").Text() == "fqdn" {
			if address.Has("interface") || !api.ValidLexical(api.LexicalDNS, address.Get("address").Text()) {
				issues = api.AppendIssues(issues, api.Issue{Code: "api.invariant", Field: path, Message: "fqdn must be an unassigned DNS contact",
					Remediation: "correct " + path[2:] + " on " + o.Identity() + " to a lowercase DNS name with no interface"})
			}
			for _, other := range c.OfKind(api.Machine) {
				if other.Identity() == o.Identity() {
					continue
				}
				for _, peer := range other.Spec().Get("network", "addresses").Items() {
					if peer.Get("name").Text() == "fqdn" && peer.Get("address").Equal(address.Get("address")) {
						issues = api.AppendIssues(issues, invariant(path+".address", "Machine fqdn contacts must be unique", "give this Machine an fqdn contact no other Machine declares"))
					}
				}
			}
		}
	}
	return issues
}

func validateInstallNetwork(o api.Object, c api.Catalog, provider api.Object, found bool, variant substrate.Arm, configured bool) []api.Issue {
	var issues []api.Issue
	native, compositionIssues := ComposeNetwork(o, c)
	issues = api.AppendIssues(issues, compositionIssues...)
	if found && configured {
		issues = api.AppendIssues(issues, validateSegments(c, o, provider, native)...)
	}
	if found && variant == substrate.ArmLibvirt && len(compositionIssues) == 0 {
		issues = api.AppendIssues(issues, validateDomainInterfaces(o, c, native, configured)...)
	}
	if _, declared := namedValue(o.Spec().Get("network", "addresses"), o.Spec().Get("network", "installAddressRef").Text()); len(compositionIssues) == 0 && (declared || !o.Spec().Has("network", "installAddressRef")) {
		address, selectionIssues := selectInstallAddress(o, c, false)
		issues = api.AppendIssues(issues, selectionIssues...)
		if address.Present() {
			prefix, _ := AssignedPrefix(o, c, address)
			for _, other := range c.OfKind(api.Machine) {
				if other.Identity() == o.Identity() {
					continue
				}
				peer, pi := selectInstallAddress(other, c, false)
				pp, e := netip.ParseAddr(peer.Get("address").Text())
				if len(pi) == 0 && e == nil && prefix.Addr() == pp {
					issues = api.AppendIssues(issues, invariant("$.spec.network.installAddressRef", "selected installation IPs must be unique across Machines", "select an install address whose IP no other Machine selects"))
					break
				}
			}
			if found && variant == substrate.ArmLibvirt {
				issues = api.AppendIssues(issues, validateManagedSegmentContainment(c, o, prefix, provider, address.Get("interface").Text(), native)...)
			}
			issues = api.AppendIssues(issues, validateInstallGateway(o, c, address, prefix, native)...)
		}
		if anacondaInstalled(o, c) && len(selectionIssues) == 0 && (address.Present() || !c.UndecodableOfKind(api.Network)) {
			issues = api.AppendIssues(issues, validateAnacondaNetwork(address, native, configured)...)
		}
	}
	return issues
}

func anacondaInstalled(o api.Object, c api.Catalog) bool {
	profile, ok := c.Find(api.MachineInstallProfile, api.MachineInstallProfileRef(o))
	return installed(o) && ok && profile.Spec().Has("installer", "anaconda")
}

func validateDomainInterfaces(o api.Object, c api.Catalog, native api.Value, configured bool) []api.Issue {
	if !api.MachineInstalled(o) && !api.MachineClusterInstalled(o) {
		return nil
	}
	if !configured {
		if anacondaInstalled(o, c) {
			return nil
		}
		return []api.Issue{{Code: "api.invariant", Field: "$.spec.network",
			Message:     "a Machine a libvirt provider realizes is reached through the network configuration its operating system applies, and this Machine selects none",
			Remediation: "select a network configuration on " + o.Identity() + " with spec.network.configRef or spec.network.inline, with an ethernet interface that is not absent or ignored"}}
	}
	if !native.Present() {
		return nil
	}
	for _, iface := range native.Get("interfaces").Items() {
		if iface.Get("type").Text() == "ethernet" && !unavailableInterface(iface) {
			return nil
		}
	}
	return []api.Issue{{Code: "api.invariant", Field: configurationPath(o),
		Message:     "the network configuration presents no available ethernet interface, so no NIC of the Machine would be configured",
		Remediation: "declare an ethernet interface that is not absent or ignored in the NMState " + o.Identity() + " selects, or select another configuration"}}
}

func validateAccess(o api.Object, c api.Catalog) []api.Issue {
	var issues []api.Issue
	s := o.Spec()
	if s.Has("access", "local") && !api.MachineProvided(o) {
		issues = api.AppendIssues(issues, invariant("$.spec.access.local", "local access requires an OS-ready Machine", "remove spec.access.local, or declare it on an OS-ready Machine"))
	}
	return issues
}

func validateBMC(bmc api.Value, unresolvedProvider bool) []api.Issue {
	var issues []api.Issue
	const path = substrate.BMCPath
	issues = api.AppendIssues(issues, substrate.ValidateBMCDefaults(bmc, "$."+path, false)...)
	if bmc.Present() {
		if !bmc.Has("credentialsRef") && !unresolvedProvider {
			issues = api.AppendIssues(issues, invariant("$."+path+".credentialsRef", "BMC credentials are required after provider inheritance", "set "+path+".credentialsRef, or inherit it from the provider's spec.baremetal.defaults.bmc.credentialsRef"))
		}
		if _, ok := substrate.NormalizeControllerEndpoint(bmc.Get("address").Text()); bmc.Has("address") && !ok {
			issues = api.AppendIssues(issues, invariant("$."+path+".address", "the BMC address must be a canonical absolute http or https URL naming exactly one /redfish/v1/Systems/<id> ComputerSystem", "set "+path+".address to an http or https URL ending in /redfish/v1/Systems/<id>"))
		}
	}
	return issues
}

// validateManagedSegmentContainment requires the install address to be a
// guest's host address on a managed Network: inside its prefix, never the
// bridge's own host address and, for an IPv4 prefix shorter than /31, never its
// network or broadcast address.
func validateManagedSegmentContainment(c api.Catalog, o api.Object, address netip.Prefix, provider api.Object, installInterface string, native api.Value) []api.Issue {
	var issues []api.Issue
	for _, reference := range installSegmentReferences(c, o, provider, native, installInterface) {
		issues = api.AppendIssues(issues, validateManagedSegmentAddress(c, o, address, provider, reference)...)
	}
	return issues
}

func validateManagedSegmentAddress(c api.Catalog, o api.Object, address netip.Prefix, provider api.Object, reference string) []api.Issue {
	segment, ok := namedValue(substrate.Segments(c, provider), reference)
	network, isManaged := substrate.ManagedSegmentNetwork(segment)
	if !ok || !isManaged {
		return nil
	}
	managed, err := netip.ParsePrefix(network.Prefix)
	if err != nil {
		return nil
	}
	reserved := managed.Addr().String()
	if reservedEnd(managed, managed.Masked().Addr()) != "" {
		reserved += ", " + managed.Masked().Addr().String() + " and " + broadcastAddress(managed).String()
	}
	refuse := func(message string) []api.Issue {
		return []api.Issue{{Code: "api.invariant", Field: "$.spec.network.installAddressRef", Message: message,
			Remediation: "assign " + o.Identity() + " an install address inside " + managed.Masked().String() + " other than " + reserved}}
	}
	switch end := reservedEnd(managed, address.Addr()); {
	case !managed.Masked().Contains(address.Addr()):
		return refuse("install address must lie inside the realized prefix " + managed.Masked().String() + " of managed Network/" + reference)
	case address.Addr() == managed.Addr():
		return refuse("the install address is the host address of managed Network/" + reference + " on " + provider.Identity())
	case end != "":
		return refuse("the install address is the " + end + " address of managed Network/" + reference + " on " + provider.Identity())
	}
	return nil
}

func nmstateSource(o api.Object) string {
	reference := o.Spec().Get("network", "configRef")
	if !reference.Present() {
		return "spec.network.inline.nmstate"
	}
	if o.Spec().Has("network", "overrides") {
		return "NetworkConfig/" + reference.Text() + " with spec.network.overrides"
	}
	return "NetworkConfig/" + reference.Text()
}

func validateInstallGateway(o api.Object, c api.Catalog, address api.Value, prefix netip.Prefix, native api.Value) []api.Issue {
	gateway, ok := defaultGateway(native, prefix.Addr().Is4(), address.Get("interface").Text())
	if !ok {
		return nil
	}
	source := nmstateSource(o)
	if prefix.Contains(gateway) || gateway.IsLinkLocalUnicast() {
		return validateNetworkGateway(o, c, prefix, gateway, source)
	}
	name := address.Get("name").Text()
	return []api.Issue{invariant("$.spec.network.installAddressRef",
		"install gateway "+gateway.String()+", the next hop of the default route in "+source+", lies outside install address "+name+" "+prefix.String()+" of "+o.Identity(),
		"set that default route's next-hop-address in "+source+" to a gateway inside "+prefix.Masked().String()+", or move install address "+name+" to a Network whose prefix contains "+gateway.String())}
}

func defaultGateway(native api.Value, ipv4 bool, iface string) (netip.Addr, bool) {
	for _, route := range native.Get("routes", "config").Items() {
		destination, err := netip.ParsePrefix(route.Get("destination").Text())
		hop := route.Get("next-hop-interface")
		if err != nil || destination.Bits() != 0 || destination.Addr().Is4() != ipv4 || route.Get("state").Text() == "absent" || hop.Present() && hop.Text() != iface {
			continue
		}
		if gateway, err := netip.ParseAddr(route.Get("next-hop-address").Text()); err == nil && gateway.Is4() == ipv4 && gateway.Zone() == "" {
			return gateway, true
		}
	}
	return netip.Addr{}, false
}

// validateHostKey keeps a delivered host key to the one lifecycle that
// delivers it, and to one Machine. A host key identifies exactly one machine,
// so two Machines sharing one would each satisfy the other's completion proof.
func validateHostKey(o api.Object, c api.Catalog) []api.Issue {
	reference := o.Spec().Get("os", "install", "hostKeyRef")
	provider, found := Provider(o, c)
	clone := substrate.UsesTemplateSnapshot(c, o)
	if !reference.Present() {
		if clone {
			return []api.Issue{invariant("$.spec.os.install.hostKeyRef", "a Machine cloned from a template snapshot needs its own SSH host key",
				"set spec.os.install.hostKeyRef to a dedicated sshKeyPair Secret")}
		}
		if installed(o) && found && substrate.RequiresInstallHostKey(provider) {
			return []api.Issue{invariant("$.spec.os.install.hostKeyRef", "this installation requires the SSH host key it delivers",
				"set spec.os.install.hostKeyRef to a dedicated sshKeyPair Secret")}
		}
		return nil
	}
	if !installed(o) || found && !clone && !substrate.SupportsDeliveredHostKey(provider) {
		return []api.Issue{invariant("$.spec.os.install.hostKeyRef",
			"this Machine's installation does not deliver an SSH host key", "remove spec.os.install.hostKeyRef or select an installation method that delivers the key")}
	}
	for _, other := range c.OfKind(api.Machine) {
		if other.Identity() != o.Identity() && other.Spec().Get("os", "install", "hostKeyRef").Equal(reference) {
			return []api.Issue{invariant("$.spec.os.install.hostKeyRef",
				"an SSH host key identifies one Machine and cannot be shared", "name a dedicated sshKeyPair Secret in spec.os.install.hostKeyRef, one no other Machine names")}
		}
	}
	return hostKeyCredentialCollisions(o, c, reference.Text())
}

// hostKeyCredentialCollisions refuses a host key that is also another
// credential: the machine would present to every client a key some other
// party already holds. Each colliding object is named once, here, because the
// Machine sees the whole catalog. An installed Machine's access key is the
// derived fleet key, so the Environment comparison already names it.
func hostKeyCredentialCollisions(o api.Object, c api.Catalog, key string) []api.Issue {
	if key == "" {
		return nil
	}
	issues := []api.Issue{}
	collide := func(holder api.Object, field string) {
		issues = api.AppendIssues(issues, api.Issue{Code: "api.invariant", Field: "$.spec.os.install.hostKeyRef",
			Message:     "the SSH host key Secret " + key + " is also " + holder.Identity() + "'s " + field + "; a host key is this Machine's own and no other credential",
			Remediation: "name a dedicated sshKeyPair Secret in spec.os.install.hostKeyRef on " + o.Identity()})
	}
	for _, environment := range c.OfKind(api.Environment) {
		if environment.Spec().Get("remoteMachinesAccessKey", "keyRef").Text() == key {
			collide(environment, "spec.remoteMachinesAccessKey.keyRef")
		}
	}
	for _, machine := range c.OfKind(api.Machine) {
		if machine.Identity() != o.Identity() && !api.MachineInstalled(machine) && machine.Spec().Get("access", "ssh", "auth", "privateKeyRef").Text() == key {
			collide(machine, "spec.access.ssh.auth.privateKeyRef")
		}
		if machine.Spec().Get("access", "lifecycle", "privateKeyRef").Text() == key {
			collide(machine, "spec.access.lifecycle.privateKeyRef")
		}
	}
	for _, cluster := range c.Clusters() {
		if field, ok := api.ClusterNodeLoginField(cluster, key); ok {
			collide(cluster, field)
		}
	}
	return issues
}

func validateBaremetal(c api.Catalog, o api.Object) []api.Issue {
	s := o.Spec()
	issues := []api.Issue{}
	if s.Get("hardware", "nics").Len() == 0 {
		issues = api.AppendIssues(issues, invariant("$.spec.hardware.nics", "bare-metal installation requires hardware NICs", "declare the install NICs in spec.hardware.nics"))
	}
	for i, nic := range s.Get("hardware", "nics").Items() {
		if !nic.Has("macAddress") {
			path := fmt.Sprintf("%s[%d].macAddress", substrate.NICsPath, i)
			issues = api.AppendIssues(issues, invariant("$."+path, "every bare-metal install NIC requires a MAC",
				"set "+path+" to the hardware address of NIC "+nic.Get("name").Text()))
		}
	}
	if !substrate.MachineBMC(o).Present() {
		issues = api.AppendIssues(issues, invariant("$."+substrate.BMCPath, "bare-metal installation requires a BMC", "declare "+substrate.BMCPath+", or inherit it from the provider's spec.baremetal.defaults.bmc"))
	}
	if reason, remedy := PhysicalRootDeviceNameRefusal(c, o); reason != "" {
		return api.AppendIssues(issues, invariant("$."+substrate.RootDiskPath+".hints.deviceName", reason, remedy))
	}
	if wwn := substrate.MachineRootDisk(o).Get("wwn"); wwn.Present() && !substrate.ValidRootWWN(wwn.Text()) {
		return api.AppendIssues(issues, invariant("$."+substrate.RootDiskPath+".wwn", "a root disk's wwn is 0x followed by 16 or 32 lowercase hexadecimal digits",
			"set "+substrate.RootDiskPath+".wwn on "+o.Identity()+" to the disk's WWN in that form, as its /dev/disk/by-id/wwn-0x link names it"))
	}
	if !installed(o) {
		if reason, remedy := AgentRootDeviceRefusal(c, o); reason != "" {
			issues = api.AppendIssues(issues, invariant("$."+substrate.RootDiskPath, reason, remedy))
		}
		return issues
	}
	physical := substrate.MachineRootDisk(o)
	bySerial := physical.Has("serialNumber") && physical.Has("sizeBytes")
	if !physical.Has("wwn") && !bySerial && !physical.Has("volumeRef") && !physical.Has("bootVolume") && !physical.Has("discover") {
		issues = api.AppendIssues(issues, invariant("$."+substrate.RootDiskPath, "bare-metal installation requires a root selector: a wwn, serialNumber with sizeBytes, or a volumeRef or bootVolume logical volume",
			RootFormsRemedy(" on "+o.Identity())))
	}
	return issues
}

// RootFormsRemedy names every root form of a bare-metal Machine an installer writes.
func RootFormsRemedy(on string) string {
	return "set " + substrate.RootDiskPath + ".wwn" + on + " to the disk's WWN, 0x and 16 or 32 lowercase hexadecimal digits, or its serialNumber and sizeBytes to the disk's serial number and exact capacity, or its volumeRef to an existing logical volume, or remove every root-disk field to default to bootVolume"
}

// PhysicalRootDeviceNameRefusal refuses a hints.deviceName on a bare-metal
// Machine an installer writes, whose WWN root form is rootDisk.wwn.
func PhysicalRootDeviceNameRefusal(c api.Catalog, o api.Object) (reason, remedy string) {
	declared := substrate.MachineRootDisk(o).Get("hints", "deviceName").Text()
	if substrate.MachineArm(c, o) != substrate.ArmBaremetal || api.MachineProvided(o) || declared == "" {
		return "", ""
	}
	path := substrate.RootDiskPath + ".hints.deviceName"
	reason = "a bare-metal Machine an installer writes names its root WWN as " + substrate.RootDiskPath + ".wwn, never by " + path
	if match := clearDevicePath.FindStringSubmatch(declared); match != nil {
		return reason, "replace " + path + " on " + o.Identity() + " with " + substrate.RootDiskPath + ".wwn: 0x" + match[1] + ", the WWN " + declared + " names"
	}
	return reason, "remove " + path + " from " + o.Identity() + " and " + RootFormsRemedy("")
}

// AgentRootDeviceRefusal requires the durable selector the agent can carry.
func AgentRootDeviceRefusal(c api.Catalog, o api.Object) (string, string) {
	if substrate.MachineArm(c, o) != substrate.ArmBaremetal {
		return "", ""
	}
	if reason, remedy := PhysicalRootDeviceNameRefusal(c, o); reason != "" {
		return reason, remedy
	}
	hints := substrate.MachineRootDeviceHints(o)
	if substrate.MachineLinuxPreflight(o).Present() {
		size, err := strconv.ParseInt(hints.SizeBytes, 10, 64)
		if !api.MachineClusterInstalled(o) || !slices.Equal(hints.Names(), []string{"serialNumber", "sizeBytes", "wwn"}) ||
			!diskselection.ValidWWN(hints.WWN) || !diskselection.ValidSerial(hints.SerialNumber) || err != nil || !diskselection.ValidSize(size) ||
			len(o.Spec().Get("hardware", "dataDisks").Items()) != 0 {
			return "Linux root preflight requires a physical Agent node with exact WWN, serial and byte capacity and no additional drive declarations or root hints",
				"set rootDisk.wwn, serialNumber and sizeBytes to the recorded Linux identity and remove any other selectors and data disks"
		}
		bmc := substrate.MachineBMC(o)
		if !substrate.BMCVerified(bmc) || bmc.Get("virtualMedia", "tls", "trust").Text() == substrate.TrustDisableVerification {
			return "Linux root preflight requires verified HTTPS for BMC access and virtual media", "use a verified BMC connection with established or imported artifact-server trust"
		}
		if firmware := substrate.MachineFirmware(o); firmware != nil && firmware.Mode == substrate.FirmwareModeEnforce {
			return "Linux root preflight requires stable firmware between inventory and installation", "apply and qualify the firmware outside this Agent installation, then remove firmware enforcement or use observe mode"
		}
		if action := substrate.MachineRootDisk(o).Get("onRemoval").Text(); action != "" && action != "retain" {
			return "Linux root preflight is an Agent installation proof", "set rootDisk.onRemoval to retain or remove it"
		}
		return "", ""
	}
	if hints.Discovery {
		return "automatic root discovery is supported only for a Bootwright RHEL installation", "declare an exact root selector for this agent installation"
	}
	if hints.VolumeRef != nil {
		if hints.VolumeRef.Valid() && len(hints.Names()) == 1 {
			return "", ""
		}
		return "a volumeRef or bootVolume selects the root alone, and a volumeRef requires safe storageId and volumeId segments",
			"declare only " + substrate.RootDiskPath + ".volumeRef or bootVolume on " + o.Identity() + ", without other root hints"
	}
	wwn := substrate.ValidRootWWN(hints.WWN)
	serial := diskselection.ValidSerial(hints.SerialNumber)
	size, err := strconv.ParseInt(hints.SizeBytes, 10, 64)
	sized := err == nil && diskselection.ValidSize(size)
	if !wwn && !(serial && sized) || hints.WWN != "" && !wwn || hints.SerialNumber != "" && !serial || hints.SizeBytes != "" && !(serial && sized) {
		return "a physical agent installation requires a stable root selector: a wwn, serialNumber with exact sizeBytes, or a volumeRef or bootVolume logical volume",
			RootFormsRemedy(" on " + o.Identity())
	}
	return "", ""
}

func validateHardware(o api.Object, c api.Catalog, variant substrate.Arm) []api.Issue {
	issues := []api.Issue{}
	seen := map[string]bool{}
	for i, entry := range o.Spec().Get("hardware", "nics").Items() {
		mac, ok := canonicalMAC(entry.Get("macAddress").Text())
		if !ok {
			continue
		}
		path := fmt.Sprintf("$.%s[%d].macAddress", substrate.NICsPath, i)
		if seen[mac] {
			issues = api.AppendIssues(issues, invariant(path, "canonical hardware MACs must be unique", "correct "+path[2:]+"; another NIC of this Machine declares the same MAC"))
		}
		seen[mac] = true
		if variant == substrate.ArmVSphere && (mac < "00:50:56:00:00:00" || mac > "00:50:56:3f:ff:ff") {
			issues = api.AppendIssues(issues, invariant(path, "vSphere MAC must be in the manual assignment range", "set "+path[2:]+" to a MAC from 00:50:56:00:00:00 to 00:50:56:3f:ff:ff"))
		}
		for _, other := range c.OfKind(api.Machine) {
			if other.Identity() == o.Identity() {
				continue
			}
			for _, peer := range other.Spec().Get("hardware", "nics").Items() {
				pm, ok := canonicalMAC(peer.Get("macAddress").Text())
				if ok && pm == mac {
					issues = api.AppendIssues(issues, api.Issue{Code: "api.invariant", Field: path, Message: "authored hardware MACs must be unique across Machines",
						Remediation: "correct " + path[2:] + " on " + o.Identity() + ", a MAC " + other.Identity() + " also declares"})
				}
			}
		}
	}
	return issues
}

func defaultedNICSegments(c api.Catalog, o, provider api.Object) (api.Value, bool) {
	segments := substrate.Segments(c, provider)
	nics := o.Spec().Get("hardware", "nics")
	if segments.Len() != 1 {
		return nics, false
	}
	sole := segments.Items()[0].Get("name")
	if sole.Type() != api.String || sole.Text() == "" {
		return nics, false
	}
	values := nics.Items()
	for i, nic := range values {
		values[i] = nic.Default("segmentRef", sole)
	}
	return api.ListValue(values...), true
}

func validateSegmentRefs(c api.Catalog, o, provider api.Object, found bool) []api.Issue {
	arm := substrate.Variant(provider)
	issues := []api.Issue{}
	for i, entry := range substrate.MachineNICSegments(o).Items() {
		if !entry.Has("segmentRef") {
			continue
		}
		field := fmt.Sprintf("$.%s[%d].segmentRef", substrate.NICsPath, i)
		if !o.Spec().Has("hardware", "providerRef") {
			issues = api.AppendIssues(issues, invariant(field, "NIC "+entry.Get("name").Text()+" of "+o.Identity()+" names segment "+entry.Get("segmentRef").Text()+", and the Machine names no provider whose segments it could select",
				"set "+substrate.ProviderPath+" on "+o.Identity()+" to the InfraProvider that declares the segment, or remove "+field[2:]))
			continue
		}
		if segment, ok := namedValue(substrate.Segments(c, provider), entry.Get("segmentRef").Text()); found && ok && !segment.Has(string(arm)) {
			issues = api.AppendIssues(issues, api.ReferenceIssue(field, "segment must name a matching arm on the selected provider", "set "+field[2:]+" to a spec.segments entry of "+provider.Identity()+" that has its "+string(arm)+" arm"))
		}
	}
	return issues
}

func validateVirtualNICs(c api.Catalog, o, provider api.Object, variant substrate.Arm) []api.Issue {
	nics := substrate.MachineNICs(o)
	if len(nics) == 0 {
		return []api.Issue{invariant("$.spec.hardware.nics", "a "+string(variant)+" provider creates one vNIC per spec.hardware.nics entry, in order, and "+o.Identity()+" declares none",
			"declare each vNIC of "+o.Identity()+" in spec.hardware.nics, in device order, such as one entry named enp1s0")}
	}
	segments := substrate.Segments(c, provider)
	path := "$." + substrate.NICsPath
	issues := []api.Issue{}
	for i, nic := range nics {
		if !api.ValidLexical(api.LexicalIfname, nic) {
			field := fmt.Sprintf("$.spec.hardware.nics[%d].name", i)
			issues = api.AppendIssues(issues, invariant(field, "NIC "+nic+" of "+o.Identity()+" is a "+string(variant)+" vNIC the installer pins under its NIC name unless an interface matches it, and "+interfaceNameRule,
				"rename NIC "+nic+" in spec.hardware.nics on "+o.Identity()+" to an interface name the guest may use, such as enp"+strconv.Itoa(i+1)+"s0, and update every nicRef that names it"))
		}
	}
	if segments.Len() == 0 && !c.UndecodableOfKind(api.Network) {
		remedy := "declare a segment with a " + string(variant) + " arm in spec.segments of " + provider.Identity()
		if variant == substrate.ArmLibvirt {
			remedy += ", or a Network with spec.provided false whose spec." + string(variant) + ".providerRef names " + provider.Name()
		}
		return append(issues, invariant(path, o.Identity()+" cannot attach its vNICs: "+provider.Identity()+" declares no segment", remedy))
	}
	offered, named := "in spec.segments", "a spec.segments entry of "+provider.Identity()
	if variant == substrate.ArmLibvirt {
		offered = "across spec.segments and the managed Networks whose spec.libvirt.providerRef names it"
		named += " or a managed Network whose spec.libvirt.providerRef names " + provider.Name()
	}
	for i, nic := range nics {
		if substrate.NICSegment(c, provider, o, nic) == "" && !c.UndecodableOfKind(api.Network) {
			field := fmt.Sprintf("%s[%d].segmentRef", path, i)
			issues = api.AppendIssues(issues, invariant(field, fmt.Sprintf("NIC %s of %s selects no segment, and %s offers %d segments %s, not exactly one that every NIC defaults to", nic, o.Identity(), provider.Identity(), segments.Len(), offered),
				"set "+field[2:]+" to "+named+" on "+o.Identity()))
		}
	}
	return issues
}

func validateVirtualMACs(o api.Object, native api.Value, variant substrate.Arm) []api.Issue {
	issues := []api.Issue{}
	for _, iface := range native.Get("interfaces").Items() {
		if iface.Get("type").Text() == "ethernet" && iface.Has("mac-address") {
			name := iface.Get("name").Text()
			field, where := configurationPath(o), "in the network configuration of "+o.Identity()
			if override, found := namedValue(o.Spec().Get("network", "overrides", "interfaces"), name); found && override.Has("mac-address") {
				field, where = "$.spec.network.overrides", "in spec.network.overrides of "+o.Identity()
			}
			issues = api.AppendIssues(issues, invariant(field, "ethernet interface "+name+" of "+o.Identity()+" authors a mac-address, but a "+string(variant)+" provider derives each vNIC's MAC from the context, Machine and NIC names",
				"remove mac-address from interface "+name+" "+where+", and match the interface to its NIC with spec.network.interfaceMatches if their names differ"))
		}
	}
	return issues
}

// validateAnacondaNetwork admits the one install network the Kickstart
// carries: a static IPv4 address on the install interface. DHCP installation
// is not supported, so a network that would leave the installer to lease one,
// including no network configuration at all, is refused here rather than at
// plan, and without a configuration the remedy selects one first, since no
// install interface exists before it. A configuration that does not resolve is
// another rule's refusal.
func validateAnacondaNetwork(address, native api.Value, configured bool) []api.Issue {
	if configured && !native.Present() {
		return nil
	}
	remediation := "assign an IPv4 address to the install interface in spec.network.addresses and select it with spec.network.installAddressRef"
	if !configured {
		remediation = "select a network configuration with spec.network.configRef or declare one in spec.network.inline, then " + remediation
	}
	static := api.Issue{Code: "api.invariant", Field: "$.spec.network.installAddressRef",
		Message:     "a Bootwright-installed Anaconda Machine installs with one static IPv4 address, and DHCP installation is not supported",
		Remediation: remediation}
	if !address.Present() {
		return []api.Issue{static}
	}
	if ip, err := netip.ParseAddr(address.Get("address").Text()); err == nil && !ip.Is4() {
		return []api.Issue{static}
	}
	iface, ok := namedValue(native.Get("interfaces"), address.Get("interface").Text())
	if ok && !slices.Contains([]string{"ethernet", "vlan", "bond"}, iface.Get("type").Text()) {
		return []api.Issue{invariant("$.spec.network.installAddressRef", "Anaconda static install interface must be ethernet, vlan, or bond", "assign the install address to an ethernet, vlan or bond interface")}
	}
	return nil
}

func normalizeConfiguration(config api.Value, c api.Catalog) api.Value {
	if config.Has("dns") {
		config = config.With("dns", infrastructureservices.NormalizeServerSelections(config.Get("dns"), c, api.DNSServer))
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
	issues = api.AppendIssues(issues, infrastructureservices.ValidateServerSelections(config.Get("dns"), c, api.DNSServer, path+".dns")...)
	managed := map[string]bool{}
	for _, selection := range config.Get("dns").Items() {
		if server, ok := c.Find(api.DNSServer, selection.Get("serverRef").Text()); ok && server.Spec().Get("management").Text() == "managed" {
			managed[server.Name()] = true
		}
	}
	if len(managed) > 1 {
		issues = api.AppendIssues(issues, invariant(path+".dns", "a network may consume at most one distinct managed DNS server", "select at most one managed DNSServer in dns"))
	}
	return issues
}

func validateServiceIntent(o api.Object) []api.Issue {
	s := o.Spec()
	issues := infrastructureservices.ValidateProxyChoice(s.Get("proxy"), "$.spec.proxy")
	if s.Has("proxy") && api.MachineClusterInstalled(o) {
		issues = api.AppendIssues(issues, invariant("$.spec.proxy", "installer-provisioned Machines use their cluster's installation proxy choice", "remove spec.proxy; the installation proxy choice of this Machine's cluster applies"))
	}
	return issues
}

func validateServices(o api.Object, c api.Catalog) []api.Issue {
	s := o.Spec()
	issues := validateServiceIntent(o)
	issues = api.AppendIssues(issues, infrastructureservices.ValidateProxy(s.Get("proxy"), c, "$.spec.proxy", installed(o))...)
	if installed(o) && s.Has("proxy", "proxyRef") {
		if profile, found := c.Find(api.MachineInstallProfile, api.MachineInstallProfileRef(o)); found &&
			profile.Spec().Get("customizations", "repositories", "configure").Len() == 0 {
			issues = api.AppendIssues(issues, api.Issue{Code: "api.deferred", Field: "$.spec.proxy",
				Message:     "the install profile declares no configured repository to receive this Machine's proxy",
				Remediation: "declare customizations.repositories.configure on " + profile.Identity() + " if its installed repositories need this proxy"})
		}
	}
	issues = api.AppendIssues(issues, infrastructureservices.ValidateServerSelections(s.Get("os", "install", "ntp"), c, api.NTPServer, "$.spec.os.install.ntp")...)
	issues = api.AppendIssues(issues, infrastructureservices.ValidateServerSelections(s.Get("os", "provided", "ntp"), c, api.NTPServer, "$.spec.os.provided.ntp")...)
	return issues
}

func installed(o api.Object) bool { return api.MachineInstalled(o) }
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

// providerBMCDefaults is what a bare-metal provider offers one Machine's
// controller. A Machine's own virtual-media block replaces the provider's
// whole, and a Machine that turns verification off inherits no bundle, since a
// bundle is an anchor only for a verified leg.
func providerBMCDefaults(bmc api.Value, provider api.Object) api.Value {
	defaults := provider.Spec().Get("baremetal", "defaults", "bmc")
	if bmc.Has("virtualMedia") {
		defaults = defaults.Without("virtualMedia")
	}
	if !substrate.BMCVerified(bmc) && defaults.Get("tls").Has("trustBundleRef") {
		defaults = defaults.With("tls", defaults.Get("tls").Without("trustBundleRef"))
	}
	return defaults
}

func installSegmentReferences(c api.Catalog, o, provider api.Object, native api.Value, name string) []string {
	var references []string
	for _, leaf := range physicalLeaves(native, name, map[string]bool{}) {
		references = append(references, substrate.NICSegment(c, provider, o, substrate.InterfaceNIC(o, leaf)))
	}
	return references
}

func physicalLeaves(native api.Value, name string, visiting map[string]bool) []string {
	if visiting[name] {
		return nil
	}
	visiting[name] = true
	defer delete(visiting, name)
	iface, _ := namedValue(native.Get("interfaces"), name)
	if parent := iface.Get("vlan", "base-iface").Text(); parent != "" {
		return physicalLeaves(native, parent, visiting)
	}
	if ports := bondPorts(native, name); len(ports) > 0 {
		var leaves []string
		for _, port := range ports {
			leaves = append(leaves, physicalLeaves(native, port, visiting)...)
		}
		return leaves
	}
	return []string{name}
}

func bondPorts(native api.Value, bond string) []string {
	iface, _ := namedValue(native.Get("interfaces"), bond)
	ports := slices.Clone(iface.Get("link-aggregation", "port").Strings())
	for _, member := range native.Get("interfaces").Items() {
		if name := member.Get("name").Text(); member.Get("controller").Text() == bond && name != bond && !slices.Contains(ports, name) {
			ports = append(ports, name)
		}
	}
	return ports
}

func nestsSplitBond(native api.Value, bond string, split map[string]bool, visiting map[string]bool) bool {
	if visiting[bond] {
		return false
	}
	visiting[bond] = true
	for _, port := range bondPorts(native, bond) {
		if split[port] || nestsSplitBond(native, port, split, visiting) {
			return true
		}
	}
	return false
}

func validateBondSegments(o api.Object, native api.Value, nicsPath string) []api.Issue {
	entries := substrate.MachineNICSegments(o).Items()
	type splitBond struct {
		name      string
		placed    []string
		differing int
	}
	var splits []splitBond
	split := map[string]bool{}
	for _, iface := range native.Get("interfaces").Items() {
		bond := iface.Get("name").Text()
		if len(bondPorts(native, bond)) == 0 || unavailableInterface(iface) {
			continue
		}
		var placed []string
		first, differing := "", -1
		for _, leaf := range physicalLeaves(native, bond, map[string]bool{}) {
			nic := substrate.InterfaceNIC(o, leaf)
			for index, entry := range entries {
				reference := entry.Get("segmentRef").Text()
				if entry.Get("name").Text() != nic || reference == "" {
					continue
				}
				placed = append(placed, leaf+" on "+reference)
				if first == "" {
					first = reference
				} else if reference != first && differing < 0 {
					differing = index
				}
			}
		}
		if differing >= 0 {
			splits = append(splits, splitBond{bond, placed, differing})
			split[bond] = true
		}
	}
	var issues []api.Issue
	for _, s := range splits {
		if nestsSplitBond(native, s.name, split, map[string]bool{}) {
			continue
		}
		issues = api.AppendIssues(issues, invariant(fmt.Sprintf("%s[%d].segmentRef", nicsPath, s.differing),
			"bond "+s.name+" of "+o.Identity()+" has ports on different segments ("+strings.Join(s.placed, ", ")+"), so a failover would move the host onto another network",
			"put the NIC of every port of bond "+s.name+" on one segment in "+nicsPath[2:]+" on "+o.Identity()+", or remove the port on another segment from the bond"))
	}
	return issues
}

func validateVolumeRoot(o api.Object, variant substrate.Arm) []api.Issue {
	root := substrate.MachineRootDisk(o)
	if root.Has("discover") {
		return validateDiscoveryRoot(o, variant)
	}
	hints := substrate.MachineRootDeviceHints(o)
	if root.Has("volumeRef") && root.Has("bootVolume") {
		return []api.Issue{invariant("$."+substrate.RootDiskPath, "bootVolume and volumeRef are mutually exclusive", "choose one root-volume selector")}
	}
	if hints.VolumeRef == nil {
		return nil
	}
	path := "$." + substrate.RootDiskPath + ".volumeRef"
	if hints.VolumeRef.Firmware {
		path = "$." + substrate.RootDiskPath + ".bootVolume"
	}
	if variant != substrate.ArmBaremetal || api.MachineProvided(o) || !hints.VolumeRef.Valid() || len(hints.Names()) != 1 {
		return []api.Issue{invariant(path, "volumeRef and bootVolume select an installed physical root alone, and volumeRef uses 1 to 128 ASCII letters, digits, underscores or hyphens for each ID", "declare volumeRef with storageId and volumeId, or bootVolume, and remove other root hints")}
	}
	if !substrate.BMCVerified(substrate.MachineBMC(o)) {
		return []api.Issue{invariant(path, "logical-volume discovery requires verified BMC TLS", "set "+substrate.BMCPath+".tls.verification to full or certificate on an https BMC address, or name the root by "+substrate.RootDiskPath+".wwn or by serialNumber and sizeBytes")}
	}
	return nil
}
