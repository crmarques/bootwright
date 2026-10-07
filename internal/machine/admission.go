package machine

import (
	"fmt"
	"net/netip"
	"slices"

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
	network := normalizedContacts(o, c, s.Get("network"))
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
		if found && substrate.RealizesPhysicalNICs(provider) {
			bmc = inherit(bmc, providerBMCDefaults(bmc, provider))
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
		if envs := c.OfKind(api.Environment); !exists && len(envs) == 1 && api.ValidLexical("name", o.Name()) {
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
	return network
}

func normalizedAccess(o api.Object, c api.Catalog, s, network api.Value) api.Value {
	access := s.Get("access")
	if installed(o) {
		if key, ok := fleetKey(c); ok {
			access = api.MapValue().WithPath(api.StringValue("bootwright"), "ssh", "user").WithPath(api.StringValue(key), "ssh", "auth", "privateKeyRef")
		}
	} else if s.Get("os", "provided").Bool() && !access.Has("local") && !access.Has("ssh") {
		access = access.WithPath(api.MapValue(), "ssh", "auth", "operatorIdentity")
	}
	if access.Has("ssh") {
		ssh := access.Get("ssh").Default("port", api.IntegerValue("22"))
		// A name that is not a DNS label derives no fqdn contact, so its session
		// falls back to none rather than repeat the name's refusal.
		reference := sshAddressRef(o, network)
		if _, declared := namedValue(network.Get("addresses"), reference); !ssh.Has("addressRef") && (declared || api.ValidLexical("name", o.Name())) {
			ssh = ssh.With("addressRef", api.StringValue(reference))
		}
		if !ssh.Has("user") && !ssh.Has("auth", "operatorIdentity") && !ssh.Has("auth", "passwordRef") {
			ssh = ssh.With("user", api.StringValue("root"))
		}
		access = access.With("ssh", ssh)
	}
	return access
}

// fleetKey is the Environment's fleet key when it names an sshKeyPair Secret,
// the only key an installed Machine's derived access can use. Any other key is
// the Environment's own refusal, which a derived reference would only repeat
// with a remedy admission forbids on an installed Machine.
func fleetKey(c api.Catalog) (string, bool) {
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
	return s
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
	if !derivesHardware(o, c) {
		issues = appendIssues(issues, substrate.ValidateBMCDefaults(s.Get("hardware", "management", "bmc"), "$.spec.hardware.management.bmc", true)...)
	}
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
	issues := validateName(o)
	issues = appendIssues(issues, validateServices(o, c)...)
	if slices.Contains(s.Get("capabilities").Strings(), "ceph-arbiter") && !slices.Contains(s.Get("capabilities").Strings(), "ceph-node") {
		issues = appendIssues(issues, invariant("$.spec.capabilities", "ceph-arbiter requires ceph-node capability"))
	}
	if envs := c.OfKind(api.Environment); len(envs) == 1 && s.Has("placement", "site") {
		if _, ok := namedValue(envs[0].Spec().Get("sites"), s.Get("placement", "site").Text()); !ok {
			issues = appendIssues(issues, reference("$.spec.placement.site", "site must name an Environment site"))
		}
	}
	issues = appendIssues(issues, validateProvision(s)...)
	provider, found := Provider(o, c)
	variant := substrate.Variant(provider)
	if found && variant != "" {
		issues = appendIssues(issues, validateSubstrate(o, provider, variant)...)
	}
	issues = appendIssues(issues, validateHardware(o, c, variant)...)
	network := s.Get("network")
	configured := network.Has("configRef") || network.Has("inline")
	issues = appendIssues(issues, validateNetworkContacts(o, c, configured)...)
	issues = appendIssues(issues, validateInstallNetwork(o, c, provider, found, variant, configured)...)
	issues = appendIssues(issues, validateAccess(o, c)...)
	issues = appendIssues(issues, validatePlacementHost(o, c)...)
	issues = appendIssues(issues, validateHostKey(o, c)...)
	if variant == substrate.ArmLibvirt {
		return issues
	}
	return appendIssues(issues, validateBMC(s.Get("hardware", "management", "bmc"))...)
}

// validateName holds every Machine name to the longest block a Machine
// contributes, os-install-<name>, because a Machine may change lifecycle. A
// name outside the label grammar is the compiler's own refusal.
func validateName(o api.Object) []api.Issue {
	if !api.ValidLexical("name", o.Name()) || len(o.Name()) <= substrate.MachineNameLimit {
		return []api.Issue{}
	}
	return []api.Issue{valueIssue("$.metadata.name",
		fmt.Sprintf("a Machine name is at most %d bytes, because the block os-install-<name> that installs it is a 63-byte identity", substrate.MachineNameLimit),
		fmt.Sprintf("rename %s to at most %d bytes, with every reference to it", o.Identity(), substrate.MachineNameLimit))}
}

func validateProvision(s api.Value) []api.Issue {
	var issues []api.Issue
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
	return issues
}

func validateSubstrate(o, provider api.Object, variant string) []api.Issue {
	var issues []api.Issue
	s := o.Spec()
	provided := s.Get("os", "provided")
	profileRef := s.Get("substrate", "profileRef")
	if variant == substrate.ArmBaremetal && profileRef.Present() {
		issues = appendIssues(issues, invariant("$.spec.substrate.profileRef", "bare-metal Machines forbid a virtual machine profile"))
	}
	if variant != substrate.ArmBaremetal {
		if provided.Present() && !provided.Bool() && !profileRef.Present() {
			issues = appendIssues(issues, invariant("$.spec.substrate.profileRef", "virtual installation requires a provider-local machine profile"))
		}
		if profileRef.Present() {
			if _, ok := namedValue(provider.Spec().Get(variant, "machineProfiles"), profileRef.Text()); !ok {
				issues = appendIssues(issues, reference("$.spec.substrate.profileRef", "profile must name a machine profile on the selected provider"))
			}
		}
	}
	if variant == substrate.ArmBaremetal && provided.Present() && !provided.Bool() {
		issues = appendIssues(issues, validateBaremetal(o)...)
	}
	for _, hint := range substrate.UnmatchableRootDeviceHints(provider) {
		if s.Has("os", "install", "rootDeviceHints", hint) {
			issues = appendIssues(issues, invariant("$.spec.os.install.rootDeviceHints."+hint,
				"the disks this substrate creates carry no WWN, SCSI address or serial number, so this hint can match none of them"))
		}
	}
	return issues
}

func validateNetworkContacts(o api.Object, c api.Catalog, configured bool) []api.Issue {
	var issues []api.Issue
	provided := o.Spec().Get("os", "provided")
	network := o.Spec().Get("network")
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
			if address.Has("interface") || !api.ValidLexical("dns", address.Get("address").Text()) {
				issues = appendIssues(issues, api.Issue{Code: "api.invariant", Field: path, Message: "fqdn must be an unassigned DNS contact",
					Remediation: "correct " + path[2:] + " on " + o.Identity() + " to a lowercase DNS name with no interface"})
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
	return issues
}

func validateInstallNetwork(o api.Object, c api.Catalog, provider api.Object, found bool, variant string, configured bool) []api.Issue {
	var issues []api.Issue
	native, compositionIssues := ComposeNetwork(o, c)
	issues = appendIssues(issues, compositionIssues...)
	if found && configured {
		issues = appendIssues(issues, validateAttachments(o, provider, native)...)
	}
	if found && variant == substrate.ArmLibvirt && len(compositionIssues) == 0 {
		issues = appendIssues(issues, validateDomainInterfaces(o, c, native, configured)...)
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
			if found && variant == substrate.ArmLibvirt {
				issues = appendIssues(issues, validateManagedAttachmentContainment(o, prefix, provider)...)
			}
		}
		if anacondaInstalled(o, c) && len(selectionIssues) == 0 {
			issues = appendIssues(issues, validateAnacondaNetwork(address, native, configured)...)
		}
	}
	return issues
}

func anacondaInstalled(o api.Object, c api.Catalog) bool {
	profile, ok := c.Find(api.MachineInstallProfile, o.Spec().Get("os", "installProfileRef").Text())
	return installed(o) && ok && profile.Spec().Has("installer", "anaconda")
}

// validateDomainInterfaces refuses a non-provided Machine a libvirt domain
// could not attach: the domain has one interface per available ethernet
// interface of the composed network configuration, so a Machine with none
// would be realized unreachable, or refused at plan. A Bootwright-installed
// Anaconda Machine that selects no configuration already refuses at its install
// address with the same remedy, so it is refused once.
func validateDomainInterfaces(o api.Object, c api.Catalog, native api.Value, configured bool) []api.Issue {
	if provided := o.Spec().Get("os", "provided"); !provided.Present() || provided.Bool() {
		return nil
	}
	if !configured {
		if anacondaInstalled(o, c) {
			return nil
		}
		return []api.Issue{{Code: "api.invariant", Field: "$.spec.network",
			Message:     "a Machine a libvirt provider realizes attaches one interface per available ethernet interface of its network configuration, and this Machine selects none",
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
		Message:     "the network configuration presents no available ethernet interface, so the domain would have no interface",
		Remediation: "declare an ethernet interface that is not absent or ignored in the NMState " + o.Identity() + " selects, or select another configuration"}}
}

func validateAccess(o api.Object, c api.Catalog) []api.Issue {
	var issues []api.Issue
	s := o.Spec()
	provided := s.Get("os", "provided")
	if local := s.Get("access", "local"); local.Present() && (!local.Bool() || !provided.Bool()) {
		issues = appendIssues(issues, invariant("$.spec.access.local", "local access must be true and requires an OS-ready Machine"))
	}
	if ssh := s.Get("access", "ssh"); ssh.Present() {
		if _, ok := namedValue(s.Get("network", "addresses"), ssh.Get("addressRef").Text()); ssh.Has("addressRef") && !ok {
			issues = appendIssues(issues, reference("$.spec.access.ssh.addressRef", "SSH address must name a Machine contact or assignment"))
		}
	}
	if s.Get("access", "rootLogin").Text() == "revoke" && (installed(o) || !successorLogin(o, c)) {
		issues = appendIssues(issues, invariant("$.spec.access.rootLogin", "revoking root login requires a managed storage-cluster non-root successor login"))
	}
	return issues
}

func validateBMC(bmc api.Value) []api.Issue {
	var issues []api.Issue
	issues = appendIssues(issues, substrate.ValidateBMCDefaults(bmc, "$.spec.hardware.management.bmc", false)...)
	if bmc.Present() {
		if !bmc.Has("credentialsRef") {
			issues = appendIssues(issues, invariant("$.spec.hardware.management.bmc.credentialsRef", "BMC credentials are required after provider inheritance"))
		}
		if _, ok := substrate.NormalizeControllerEndpoint(bmc.Get("address").Text()); bmc.Has("address") && !ok {
			issues = appendIssues(issues, invariant("$.spec.hardware.management.bmc.address", "the BMC address must be a canonical absolute http or https URL naming exactly one /redfish/v1/Systems/<id> ComputerSystem"))
		}
	}
	return issues
}

// validateManagedAttachmentContainment requires the install address to be a
// guest's host address on the network Bootwright defines for a managed libvirt
// attachment: inside its prefix, never the bridge's own host address and, for
// an IPv4 prefix shorter than /31, never its network or broadcast address.
func validateManagedAttachmentContainment(o api.Object, address netip.Prefix, provider api.Object) []api.Issue {
	attachment, ok := namedValue(provider.Spec().Get("networkAttachments"), o.Spec().Get("network", "attachmentRef").Text())
	if !ok || attachment.Get("libvirt", "management").Text() != "managed" {
		return nil
	}
	managed, err := netip.ParsePrefix(attachment.Get("libvirt", "address").Text())
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
		return refuse("install address must lie inside the managed libvirt attachment's network")
	case address.Addr() == managed.Addr():
		return refuse("the install address is the managed bridge's own host address on " + provider.Identity())
	case end != "":
		return refuse("the install address is the " + end + " address of the managed bridge's prefix on " + provider.Identity())
	}
	return nil
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
	if !installed(o) || !found || !substrate.RealizesPhysicalNICs(provider) {
		return []api.Issue{invariant("$.spec.os.install.hostKeyRef",
			"only a bare-metal Bootwright-installed Machine delivers its own SSH host key")}
	}
	for _, other := range c.OfKind(api.Machine) {
		if other.Identity() != o.Identity() && other.Spec().Get("os", "install", "hostKeyRef").Equal(reference) {
			return []api.Issue{invariant("$.spec.os.install.hostKeyRef",
				"an SSH host key identifies one Machine and cannot be shared")}
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
		issues = appendIssues(issues, api.Issue{Code: "api.invariant", Field: "$.spec.os.install.hostKeyRef",
			Message:     "the SSH host key Secret " + key + " is also " + holder.Identity() + "'s " + field + "; a host key is this Machine's own and no other credential",
			Remediation: "name a dedicated sshKeyPair Secret in spec.os.install.hostKeyRef on " + o.Identity()})
	}
	for _, environment := range c.OfKind(api.Environment) {
		if environment.Spec().Get("remoteMachinesAccessKey", "keyRef").Text() == key {
			collide(environment, "spec.remoteMachinesAccessKey.keyRef")
		}
	}
	for _, machine := range c.OfKind(api.Machine) {
		if machine.Identity() != o.Identity() && !machine.Spec().Has("os", "installProfileRef") && machine.Spec().Get("access", "ssh", "auth", "privateKeyRef").Text() == key {
			collide(machine, "spec.access.ssh.auth.privateKeyRef")
		}
	}
	for _, cluster := range c.OfKind(api.StorageCluster) {
		if cluster.Spec().Get("ceph", "cephadm", "clusterSSH", "keyRef").Text() == key {
			collide(cluster, "spec.ceph.cephadm.clusterSSH.keyRef")
		}
	}
	for _, cluster := range c.OfKind(api.ContainerCluster) {
		for _, field := range []string{"keyPairRef", "publicKeyRef", "privateKeyRef"} {
			if cluster.Spec().Get("install", "nodeSSH", field).Text() == key {
				collide(cluster, "spec.install.nodeSSH."+field)
				break
			}
		}
	}
	return issues
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
	if !s.Get("hardware", "management", "bmc").Present() {
		issues = appendIssues(issues, invariant("$.spec.hardware.management.bmc", "bare-metal installation requires a BMC"))
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

// derivesHardware is a Machine of a libvirt provider, which derives every
// interface's MAC and realizes the management controller as its emulated BMC,
// so it reads neither from the Machine.
func derivesHardware(o api.Object, c api.Catalog) bool {
	provider, found := Provider(o, c)
	return found && substrate.Variant(provider) == substrate.ArmLibvirt
}

// validateDerivedHardware refuses each MAC and the management controller a
// libvirt provider's Machine authors, since realization would ignore them.
func validateDerivedHardware(o api.Object) []api.Issue {
	issues := []api.Issue{}
	for i, nic := range o.Spec().Get("hardware", "nics").Items() {
		if nic.Has("macAddress") {
			path := fmt.Sprintf("spec.hardware.nics[%d].macAddress", i)
			issues = appendIssues(issues, api.Issue{Code: "api.invariant", Field: "$." + path,
				Message:     "a libvirt provider derives every interface's MAC from the context, Machine and interface names, so an authored MAC would be ignored",
				Remediation: "remove " + path + " from " + o.Identity()})
		}
	}
	if o.Spec().Has("hardware", "management", "bmc") {
		issues = appendIssues(issues, api.Issue{Code: "api.invariant", Field: "$.spec.hardware.management.bmc",
			Message:     "a libvirt provider realizes this Machine's management controller as its emulated BMC, so an authored one would be ignored",
			Remediation: "remove spec.hardware.management.bmc from " + o.Identity()})
	}
	return issues
}

func validateHardware(o api.Object, c api.Catalog, variant string) []api.Issue {
	if variant == substrate.ArmLibvirt {
		return validateDerivedHardware(o)
	}
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
		if variant == substrate.ArmVSphere && (mac < "00:50:56:00:00:00" || mac > "00:50:56:3f:ff:ff") {
			issues = appendIssues(issues, invariant(path, "vSphere MAC must be in the manual assignment range"))
		}
		for _, other := range c.OfKind(api.Machine) {
			if other.Identity() == o.Identity() || derivesHardware(other, c) {
				continue
			}
			for _, peer := range other.Spec().Get("hardware", "nics").Items() {
				pm, ok := canonicalMAC(peer.Get("macAddress").Text())
				if ok && pm == mac {
					issues = appendIssues(issues, api.Issue{Code: "api.invariant", Field: path, Message: "authored hardware MACs must be unique across Machines",
						Remediation: "correct " + path[2:] + " on " + o.Identity() + ", a MAC " + other.Identity() + " also declares"})
				}
			}
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
	// A bare-metal attachment configures nothing, so bare metal selects none;
	// one it authors still resolves below.
	if variant != substrate.ArmBaremetal && !network.Has("attachmentRef") && !network.Has("interfaceAttachments") {
		issues = appendIssues(issues, api.Issue{Code: "api.invariant", Field: "$.spec.network.attachmentRef",
			Message:     "provider-backed configured networks require an explicit attachment unless the unique matching default applies",
			Remediation: "set spec.network.attachmentRef on " + o.Identity() + " to an attachment of " + provider.Identity()})
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
		if variant != substrate.ArmKubeVirt {
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
	remediation := "assign an IPv4 address with its prefix to the install interface in spec.network.addresses and select it with spec.network.installAddressRef"
	if !configured {
		remediation = "select a network configuration with spec.network.configRef or declare one in spec.network.inline, then " + remediation
	}
	static := api.Issue{Code: "api.invariant", Field: "$.spec.network.installAddressRef",
		Message:     "a Bootwright-installed Anaconda Machine installs with one static IPv4 address, and DHCP installation is not supported",
		Remediation: remediation}
	if !address.Present() {
		return []api.Issue{static}
	}
	if prefix, err := netip.ParsePrefix(address.Get("address").Text()); err == nil && !prefix.Addr().Is4() {
		return []api.Issue{static}
	}
	iface, ok := namedValue(native.Get("interfaces"), address.Get("interface").Text())
	if ok && !slices.Contains([]string{"ethernet", "vlan", "bond"}, iface.Get("type").Text()) {
		return []api.Issue{invariant("$.spec.network.installAddressRef", "Anaconda static install interface must be ethernet, vlan, or bond")}
	}
	return nil
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

// providerBMCDefaults is what a bare-metal provider offers one Machine's
// controller. A Machine's own virtual-media block replaces the provider's
// whole, and a Machine that turns verification off inherits no bundle, since a
// bundle is an anchor only for a verified leg.
func providerBMCDefaults(bmc api.Value, provider api.Object) api.Value {
	defaults := provider.Spec().Get("baremetal", "defaults", "bmc")
	if bmc.Has("virtualMedia") {
		defaults = defaults.Without("virtualMedia")
	}
	if bmc.Get("tls").Has("verify") && !bmc.Get("tls", "verify").Bool() && defaults.Get("tls").Has("trustBundleRef") {
		defaults = defaults.With("tls", defaults.Get("tls").Without("trustBundleRef"))
	}
	return defaults
}
