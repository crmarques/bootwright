package substrate

import (
	"fmt"
	"net/netip"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func Variant(o api.Object) string {
	selected := ""
	for _, name := range Arms() {
		if o.Spec().Has(name) {
			if selected != "" {
				return ""
			}
			selected = name
		}
	}
	return selected
}

func Normalize(o api.Object, _ api.Catalog) (api.Object, []api.Issue) {
	if o.Kind() != api.InfraProvider {
		return o, nil
	}
	spec := o.Spec()
	variant := Variant(o)
	arm := spec.Get(variant)
	switch variant {
	case "baremetal":
		bmc := arm.Get("defaults", "bmc")
		if bmc.Present() {
			bmc = NormalizeBMCDefaults(bmc)
			arm = arm.WithPath(bmc, "defaults", "bmc")
		}
		// The boot method materializes whether or not the block was authored,
		// so effective state always names the path a physical machine is
		// booted by rather than leaving a consumer to assume one.
		arm = arm.WithPath(arm.Get("boot").Default("method", api.StringValue(BootRedfishVirtualMedia)), "boot")
	case "libvirt":
		// One listener has one spelling, so its endpoint, its socket key and
		// every comparison with another listener read the text netip prints.
		if address, err := netip.ParseAddr(arm.Get("bmcEmulationDefaults", "bindAddress").Text()); err == nil {
			arm = arm.WithPath(api.StringValue(address.String()), "bmcEmulationDefaults", "bindAddress")
		}
	case "vsphere":
		for _, side := range []string{"external", "internal"} {
			values := arm.Get("nodeNetworking", side, "networkSubnetCidr")
			if values.Present() {
				items := values.Items()
				for i, v := range items {
					if p, e := netip.ParsePrefix(v.Text()); e == nil {
						items[i] = api.StringValue(p.Masked().String())
					}
				}
				arm = arm.WithPath(api.ListValue(items...), "nodeNetworking", side, "networkSubnetCidr")
			}
		}
		profiles := arm.Get("machineProfiles").Items()
		domains := arm.Get("failureDomains").Items()
		if len(domains) == 1 {
			for index, profile := range profiles {
				profiles[index] = profile.Default("failureDomainRef", domains[0].Get("name"))
			}
			if arm.Has("machineProfiles") {
				arm = arm.With("machineProfiles", api.ListValue(profiles...))
			}
			if arm.Has("isoStaging") {
				arm = arm.With("isoStaging", arm.Get("isoStaging").Default("datastore", domains[0].Get("topology", "datastore")))
			}
		}
		if arm.Has("isoStaging") {
			arm = arm.With("isoStaging", arm.Get("isoStaging").Default("folder", api.StringValue("bootwright-vmedia")))
		}
	}
	if variant == "" {
		return o, nil
	}
	spec = spec.With(variant, arm)
	attachments := spec.Get("networkAttachments").Items()
	if variant == "libvirt" {
		for index, attachment := range attachments {
			libvirt := attachment.Get("libvirt")
			if libvirt.Get("management").Text() == "managed" && !libvirt.Has("forward") {
				attachments[index] = attachment.WithPath(api.StringValue("nat"), "libvirt", "forward")
			}
		}
		if spec.Has("networkAttachments") {
			spec = spec.With("networkAttachments", api.ListValue(attachments...))
		}
	}
	if variant == "kubevirt" {
		for index, attachment := range attachments {
			ref := attachment.Get("kubevirt", "networkRef")
			if !ref.Present() {
				continue
			}
			ref = ref.Default("kind", api.StringValue("ClusterUserDefinedNetwork"))
			switch ref.Get("kind").Text() {
			case "ClusterUserDefinedNetwork", "UserDefinedNetwork":
				ref = ref.Default("apiGroup", api.StringValue("k8s.ovn.org"))
			case "NetworkAttachmentDefinition":
				ref = ref.Default("apiGroup", api.StringValue("k8s.cni.cncf.io"))
			}
			if ref.Get("kind").Text() == "UserDefinedNetwork" || ref.Get("kind").Text() == "NetworkAttachmentDefinition" {
				ref = ref.Default("namespace", arm.Get("namespace"))
			}
			attachments[index] = attachment.WithPath(ref, "kubevirt", "networkRef")
		}
		if spec.Has("networkAttachments") {
			spec = spec.With("networkAttachments", api.ListValue(attachments...))
		}
	}
	return o.WithSpec(spec), nil
}

func NormalizeBMCDefaults(bmc api.Value) api.Value {
	bmc = bmc.With("tls", bmc.Get("tls").Default("verify", api.BoolValue(true)))
	media := bmc.Get("virtualMedia")
	tls := media.Get("tls").Default("trust", api.StringValue(TrustImportCertificate))
	switch tls.Get("trust").Text() {
	case "disable-verification":
		tls = tls.Default("restoreVerificationAfterBoot", api.BoolValue(true))
	case "import-certificate":
		tls = tls.Default("removeCertificateAfterBoot", api.BoolValue(false))
	}
	return bmc.With("virtualMedia", media.With("tls", tls))
}

func ValidateBMCDefaults(bmc api.Value, path string, authored bool) []api.Issue {
	tls := bmc.Get("virtualMedia", "tls")
	issues := []api.Issue{}
	if authored && tls.Present() && tls.Len() == 0 {
		issues = add(issues, issue(path+".virtualMedia.tls", "an authored virtual-media TLS block must set an option"))
	}
	issues = add(issues, validateControllerTrust(bmc, path)...)
	trust := tls.Get("trust").Text()
	if trust == "" {
		trust = TrustImportCertificate
	}
	if tls.Has("restoreVerificationAfterBoot") && trust != "disable-verification" {
		issues = add(issues, issue(path+".virtualMedia.tls.restoreVerificationAfterBoot", "restoring verification requires disable-verification trust"))
	}
	if tls.Has("removeCertificateAfterBoot") && trust != "import-certificate" {
		issues = add(issues, issue(path+".virtualMedia.tls.removeCertificateAfterBoot", "removing a certificate requires import-certificate trust"))
	}
	return issues
}

// validateControllerTrust refuses a bundle beside verification turned off. It
// reads the value it is given, so an authored block and an effective one that
// inherited either half are held to the same rule.
func validateControllerTrust(bmc api.Value, path string) []api.Issue {
	tls := bmc.Get("tls")
	if !tls.Has("trustBundleRef") || !tls.Has("verify") || tls.Get("verify").Bool() {
		return nil
	}
	return []api.Issue{{Code: "api.invariant", Field: path + ".tls.trustBundleRef",
		Message:     "a controller trust bundle requires verification, and tls.verify is false here, whether authored or inherited",
		Remediation: "set tls.verify: true where the bundle is declared, or remove tls.trustBundleRef"}}
}

// validateProviderMediaTrust refuses the one virtual-media trust that is an
// exception: a provider default would hand it to every Machine it hosts.
func validateProviderMediaTrust(bmc api.Value, path string) []api.Issue {
	if bmc.Get("virtualMedia", "tls", "trust").Text() != TrustDisableVerification {
		return nil
	}
	return []api.Issue{{Code: "api.invariant", Field: path + ".virtualMedia.tls.trust",
		Message:     "disable-verification is a per-Machine exception and never a provider default",
		Remediation: "declare hardware.management.bmc.virtualMedia.tls.trust: disable-verification on each Machine that needs it"}}
}

func ValidateAuthored(o api.Object, _ api.Catalog) []api.Issue {
	if o.Kind() != api.InfraProvider {
		return nil
	}
	bmc := o.Spec().Get("baremetal", "defaults", "bmc")
	issues := ValidateBMCDefaults(bmc, "$.spec.baremetal.defaults.bmc", true)
	issues = add(issues, validateProviderMediaTrust(bmc, "$.spec.baremetal.defaults.bmc")...)
	staging := o.Spec().Get("vsphere", "isoStaging")
	if staging.Present() && !staging.Has("datastore") && !staging.Has("folder") {
		issues = add(issues, issue("$.spec.vsphere.isoStaging", "an authored staging block must set datastore or folder"))
	}
	return issues
}

func Validate(o api.Object, c api.Catalog) []api.Issue {
	if o.Kind() != api.InfraProvider {
		return nil
	}
	issues := []api.Issue{}
	if api.ValidLexical("name", o.Name()) && len(o.Name()) > ProviderNameLimit {
		issues = add(issues, api.Issue{Code: "api.value", Field: "$.metadata.name",
			Message:     fmt.Sprintf("an InfraProvider name is at most %d bytes, because its host block substrate-host-<name> is a 63-byte identity", ProviderNameLimit),
			Remediation: fmt.Sprintf("rename %s to at most %d bytes, with every providerRef that names it", o.Identity(), ProviderNameLimit)})
	}
	variant := Variant(o)
	if variant == "" {
		return issues
	}
	arm := o.Spec().Get(variant)
	if variant == "baremetal" {
		issues = add(issues, ValidateBMCDefaults(arm.Get("defaults", "bmc"), "$.spec.baremetal.defaults.bmc", false)...)
	}
	if variant == "libvirt" {
		host, ok := c.Find(api.Machine, arm.Get("machineRef").Text())
		if ok && !slices.Contains(host.Spec().Get("capabilities").Strings(), "libvirt") {
			issues = add(issues, issue("$.spec.libvirt.machineRef", "libvirt host requires libvirt capability"))
		}
		bmc := arm.Get("bmcEmulationDefaults")
		if bmc.Has("enabled") && !bmc.Get("enabled").Bool() {
			issues = add(issues, issue("$.spec.libvirt.bmcEmulationDefaults.enabled", "libvirt BMC emulation must be enabled"))
		}
		issues = add(issues, validateEmulatedListener(bmc)...)
		issues = add(issues, requirePositiveCapacity(arm, variant, "libvirt")...)
		issues = add(issues, validateLibvirtDataDisks(o, arm)...)
		first, last, ranged := bmcPortRange(o, c)
		if ranged && last > 65535 {
			issues = add(issues, issue("$.spec.libvirt.bmcEmulationDefaults.port", "the emulated BMC port range must end at or below 65535"))
		}
		for _, other := range c.OfKind(api.InfraProvider) {
			peer := other.Spec().Get("libvirt")
			if other.Identity() == o.Identity() || !peer.Present() || !peer.Get("machineRef").Equal(arm.Get("machineRef")) {
				continue
			}
			peerFirst, peerLast, peerRanged := bmcPortRange(other, c)
			if ranged && peerRanged && first <= peerLast && peerFirst <= last {
				issues = add(issues, issue("$.spec.libvirt.bmcEmulationDefaults.port", "emulated BMC port ranges of providers on one host must not overlap"))
				break
			}
		}
		issues = add(issues, validateSharedHostNetworks(o, c)...)
	}
	if variant == "vsphere" {
		domains := arm.Get("failureDomains").Items()
		for index, domain := range domains {
			path := fmt.Sprintf("$.spec.vsphere.failureDomains[%d]", index)
			server, ok := findNamed(arm.Get("vcenters"), "server", domain.Get("server").Text())
			if domain.Has("server") && !ok {
				issues = add(issues, reference(path+".server", "failure-domain server must name a declared vCenter"))
			} else if ok && domain.Get("topology", "datacenter").Present() && !slices.Contains(server.Get("datacenters").Strings(), domain.Get("topology", "datacenter").Text()) {
				issues = add(issues, reference(path+".topology.datacenter", "failure-domain datacenter must be declared on its vCenter"))
			}
			if domain.Get("topology", "networks").Len() > 1 && !arm.Has("nodeNetworking") {
				issues = add(issues, issue("$.spec.vsphere.nodeNetworking", "multiple topology networks require nodeNetworking"))
			}
		}
		issues = add(issues, requirePositiveCapacity(arm, variant, "vSphere")...)
		for index, profile := range arm.Get("machineProfiles").Items() {
			path := fmt.Sprintf("$.spec.vsphere.machineProfiles[%d]", index)
			if !profile.Has("failureDomainRef") && len(domains) > 1 {
				issues = add(issues, issue(path+".failureDomainRef", "multiple failure domains require an explicit profile selection"))
			} else if profile.Has("failureDomainRef") {
				if _, ok := findNamed(arm.Get("failureDomains"), "name", profile.Get("failureDomainRef").Text()); !ok {
					issues = add(issues, reference(path+".failureDomainRef", "profile failure domain must name a domain on this provider"))
				}
			}
		}
	}
	for index, attachment := range o.Spec().Get("networkAttachments").Items() {
		path := fmt.Sprintf("$.spec.networkAttachments[%d]", index)
		if !attachment.Has(variant) {
			issues = add(issues, issue(path, "attachment arm must match its provider substrate"))
		}
		if variant == "libvirt" {
			issues = add(issues, validateLibvirtAttachment(attachment.Get("libvirt"), path+".libvirt")...)
		}
		if variant == "vsphere" && arm.Get("failureDomains").Len() > 1 && !attachment.Get("vsphere").Has("distributedSwitch") {
			issues = add(issues, issue(path+".vsphere.distributedSwitch", "multiple failure domains require an explicit distributed switch"))
		}
		if variant == "kubevirt" {
			ref := attachment.Get("kubevirt", "networkRef")
			kind := ref.Get("kind").Text()
			if kind == "ClusterUserDefinedNetwork" && ref.Has("namespace") {
				issues = add(issues, issue(path+".kubevirt.networkRef.namespace", "cluster-scoped networks forbid a namespace"))
			}
			if ref.Present() && !ref.Has("apiGroup") {
				issues = add(issues, issue(path+".kubevirt.networkRef.apiGroup", "unknown external network kinds require an explicit API group"))
			}
		}
	}
	return issues[:min(len(issues), 999)]
}

// NameableListener reports whether an emulated BMC may listen on address: one
// unicast address, in the spelling netip prints, that every hosted Machine's
// controller endpoint, and the socket key its listener claims, can name. A
// wildcard, multicast or the IPv4 broadcast address names no one listener. An
// IPv6 address is bracketed in the endpoint, but a link-local one means nothing
// without a zone, a zone has no spelling a controller endpoint admits, and an
// IPv4-mapped one is a second spelling of an IPv4 socket, so each refuses.
func NameableListener(address string) bool {
	parsed, err := netip.ParseAddr(address)
	if err != nil || parsed.String() != address || parsed.Zone() != "" || parsed.IsUnspecified() || parsed.IsMulticast() {
		return false
	}
	if parsed.Is6() {
		return !parsed.Is4In6() && !parsed.IsLinkLocalUnicast()
	}
	return parsed != netip.AddrFrom4([4]byte{255, 255, 255, 255})
}

// validateEmulatedListener judges only an address that is present: the schema
// already refuses an absent one, and a second diagnostic would say it twice.
func validateEmulatedListener(bmc api.Value) []api.Issue {
	address := bmc.Get("bindAddress")
	if !address.Present() || NameableListener(address.Text()) {
		return nil
	}
	return []api.Issue{issue("$.spec.libvirt.bmcEmulationDefaults.bindAddress",
		"the emulated BMC listens on one unicast address its controller endpoints can name: not unspecified, multicast or the IPv4 broadcast address, and not an IPv6 link-local, zoned or IPv4-mapped address")}
}

// requirePositiveCapacity refuses a profile size its arm cannot create a
// machine with. The schema materializes 0 for an omitted size, so an omitted
// size refuses here too.
func requirePositiveCapacity(arm api.Value, variant, label string) []api.Issue {
	issues := []api.Issue{}
	for index, profile := range arm.Get("machineProfiles").Items() {
		for _, size := range []string{"cpu", "memoryMiB", "diskGiB"} {
			value := profile.Get(size)
			if value.Type() != api.Integer || value.Text() == "0" || strings.HasPrefix(value.Text(), "-") {
				issues = add(issues, issue(fmt.Sprintf("$.spec.%s.machineProfiles[%d].%s", variant, index, size), label+" machine profiles require a positive capacity"))
			}
		}
	}
	return issues
}

// bmcPortRange is the contiguous range a provider's emulated BMCs claim: one
// port per hosted Machine in canonical name order, or the base port alone while
// no Machine selects the provider.
func bmcPortRange(provider api.Object, c api.Catalog) (int64, int64, bool) {
	first, ok := provider.Spec().Get("libvirt", "bmcEmulationDefaults", "port").Int64()
	if !ok {
		return 0, 0, false
	}
	machines := int64(len(HostedMachines(c, provider.Name())))
	return first, first + max(machines, 1) - 1, true
}

// validateLibvirtDataDisks refuses a profile whose data disks the domain cannot
// present: more than its targets after the root disk, or one named root, whose
// image the root disk already is.
func validateLibvirtDataDisks(o api.Object, arm api.Value) []api.Issue {
	issues := []api.Issue{}
	for index, profile := range arm.Get("machineProfiles").Items() {
		path := fmt.Sprintf("$.spec.libvirt.machineProfiles[%d].dataDisks", index)
		disks := profile.Get("dataDisks")
		if disks.Len() > MaxDataDisks {
			issues = add(issues, api.Issue{Code: "api.invariant", Field: path,
				Message:     fmt.Sprintf("a libvirt domain presents at most %d data disks, vdb through vdh, after its root disk", MaxDataDisks),
				Remediation: fmt.Sprintf("declare at most %d dataDisks on spec.libvirt.machineProfiles[%d] of %s", MaxDataDisks, index, o.Identity())})
		}
		for disk, data := range disks.Items() {
			if data.Get("name").Text() == "root" {
				issues = add(issues, api.Issue{Code: "api.invariant", Field: fmt.Sprintf("%s[%d].name", path, disk),
					Message:     "the data disk name root is reserved for the root disk, which the domain presents as vda from root.qcow2",
					Remediation: fmt.Sprintf("rename spec.libvirt.machineProfiles[%d].dataDisks[%d] on %s", index, disk, o.Identity())})
			}
		}
	}
	return issues
}

// hostAttachment is one libvirt network attachment of a provider on a host.
type hostAttachment struct {
	provider api.Object
	index    int
	name     string
	bridge   string
	managed  bool
	prefix   netip.Prefix
}

// libvirtAttachments lists a provider's libvirt attachments, keeping each name
// and bridge only when it passes its grammar, and a managed attachment's masked
// prefix only when its address is a host address with its prefix, so an
// invalid value yields its grammar refusal and nothing else.
func libvirtAttachments(provider api.Object) []hostAttachment {
	var found []hostAttachment
	for index, attachment := range provider.Spec().Get("networkAttachments").Items() {
		libvirt := attachment.Get("libvirt")
		if !libvirt.Present() {
			continue
		}
		entry := hostAttachment{provider: provider, index: index, managed: libvirt.Get("management").Text() == "managed"}
		if name := attachment.Get("name").Text(); api.ValidLexical("name", name) {
			entry.name = name
		}
		if bridge := libvirt.Get("bridge").Text(); api.ValidLexical("bridge", bridge) {
			entry.bridge = bridge
		}
		if prefix, err := netip.ParsePrefix(libvirt.Get("address").Text()); entry.managed && err == nil && prefix.Bits() > 0 && prefix.Addr() != prefix.Masked().Addr() {
			entry.prefix = prefix.Masked()
		}
		found = append(found, entry)
	}
	return found
}

// validateSharedHostNetworks refuses what two attachments on one host would
// both claim: a managed attachment name another provider also manages, since
// a context names the one network it defines after it, and a bridge a managed
// attachment defines that another attachment also names, since a bridge name
// is host-global and goes with the host block that defines it; and two managed
// prefixes that overlap, of one provider or two, since the host routes each
// managed prefix to its own bridge.
func validateSharedHostNetworks(o api.Object, c api.Catalog) []api.Issue {
	host := o.Spec().Get("libvirt", "machineRef").Text()
	if !api.ValidLexical("name", host) {
		return nil
	}
	attachments := libvirtAttachments(o)
	onHost := slices.Clone(attachments)
	for _, other := range c.OfKind(api.InfraProvider) {
		if other.Identity() != o.Identity() && other.Spec().Get("libvirt").Present() && other.Spec().Get("libvirt", "machineRef").Text() == host {
			onHost = append(onHost, libvirtAttachments(other)...)
		}
	}
	issues := []api.Issue{}
	for _, own := range attachments {
		path := fmt.Sprintf("$.spec.networkAttachments[%d]", own.index)
		for _, peer := range onHost {
			sameProvider := peer.provider.Identity() == o.Identity()
			if sameProvider && peer.index == own.index {
				continue
			}
			if !sameProvider && own.managed && peer.managed && own.name != "" && own.name == peer.name {
				issues = add(issues, api.Issue{Code: "api.invariant", Field: path + ".name",
					Message: fmt.Sprintf("managed attachment %s is also a managed attachment of %s on host Machine/%s, and both would define the one libvirt network this context names after it",
						own.name, peer.provider.Identity(), host),
					Remediation: fmt.Sprintf("rename the attachment on %s or %s, with the attachmentRef of every Machine that selects it", o.Identity(), peer.provider.Identity())})
			}
			if (own.managed || peer.managed) && own.bridge != "" && own.bridge == peer.bridge {
				issues = add(issues, api.Issue{Code: "api.invariant", Field: path + ".libvirt.bridge",
					Message: fmt.Sprintf("bridge %s of networkAttachments[%d] is also named by networkAttachments[%d] of %s on host Machine/%s; a bridge a managed attachment defines belongs to that attachment alone",
						own.bridge, own.index, peer.index, peer.provider.Identity(), host),
					Remediation: fmt.Sprintf("give spec.networkAttachments[%d] of %s or spec.networkAttachments[%d] of %s its own bridge, or make both external",
						own.index, o.Identity(), peer.index, peer.provider.Identity())})
			}
			if own.managed && peer.managed && own.prefix.IsValid() && peer.prefix.IsValid() && own.prefix.Overlaps(peer.prefix) {
				issues = add(issues, api.Issue{Code: "api.invariant", Field: path + ".libvirt.address",
					Message: fmt.Sprintf("managed prefix %s of networkAttachments[%d] overlaps managed prefix %s of networkAttachments[%d] of %s on host Machine/%s, and the host routes each managed prefix to its own bridge",
						own.prefix, own.index, peer.prefix, peer.index, peer.provider.Identity(), host),
					Remediation: fmt.Sprintf("give spec.networkAttachments[%d].libvirt.address of %s or spec.networkAttachments[%d].libvirt.address of %s a prefix no other managed attachment on host Machine/%s overlaps",
						own.index, o.Identity(), peer.index, peer.provider.Identity(), host)})
			}
		}
	}
	return issues
}

func validateLibvirtAttachment(arm api.Value, path string) []api.Issue {
	if !arm.Present() {
		return nil
	}
	issues := []api.Issue{}
	managed := arm.Get("management").Text() == "managed"
	if managed && !arm.Has("address") {
		issues = add(issues, issue(path+".address", "a managed libvirt network requires the host address and prefix"))
	}
	if !managed {
		for _, key := range []string{"address", "forward"} {
			if arm.Has(key) {
				issues = add(issues, issue(path+"."+key, "an external libvirt bridge carries no managed network fields"))
			}
		}
	}
	if address := arm.Get("address"); address.Present() {
		prefix, err := netip.ParsePrefix(address.Text())
		if err != nil || prefix.Bits() == 0 || prefix.Addr() == prefix.Masked().Addr() {
			issues = add(issues, issue(path+".address", "the managed network address must be a host IP with its prefix"))
		}
	}
	return issues
}

func findNamed(values api.Value, key, name string) (api.Value, bool) {
	var found api.Value
	count := 0
	for _, value := range values.Items() {
		if value.Get(key).Text() == name {
			found = value
			count++
		}
	}
	return found, count == 1
}

func issue(field, message string) api.Issue {
	return api.Issue{Code: "api.invariant", Field: field, Message: message, Remediation: "make the provider and its local selections consistent"}
}

func reference(field, message string) api.Issue {
	i := issue(field, message)
	i.Code = "api.reference"
	return i
}

func add(issues []api.Issue, more ...api.Issue) []api.Issue {
	return append(issues, more[:min(len(more), 999-len(issues))]...)
}
