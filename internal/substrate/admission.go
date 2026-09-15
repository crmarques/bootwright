package substrate

import (
	"fmt"
	"net/netip"
	"net/url"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func Variant(o api.Object) string {
	selected := ""
	for _, name := range []string{"baremetal", "libvirt", "vsphere", "kubevirt"} {
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
	tls := media.Get("tls").Default("trust", api.StringValue("disable-verification"))
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
	trust := tls.Get("trust").Text()
	if trust == "" {
		trust = "disable-verification"
	}
	if tls.Has("restoreVerificationAfterBoot") && trust != "disable-verification" {
		issues = add(issues, issue(path+".virtualMedia.tls.restoreVerificationAfterBoot", "restoring verification requires disable-verification trust"))
	}
	if tls.Has("removeCertificateAfterBoot") && trust != "import-certificate" {
		issues = add(issues, issue(path+".virtualMedia.tls.removeCertificateAfterBoot", "removing a certificate requires import-certificate trust"))
	}
	return issues
}

func ValidateAuthored(o api.Object, _ api.Catalog) []api.Issue {
	if o.Kind() != api.InfraProvider {
		return nil
	}
	issues := ValidateBMCDefaults(o.Spec().Get("baremetal", "defaults", "bmc"), "$.spec.baremetal.defaults.bmc", true)
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
	variant := Variant(o)
	if variant == "" {
		return nil
	}
	arm := o.Spec().Get(variant)
	issues := []api.Issue{}
	if variant == "baremetal" {
		issues = add(issues, ValidateBMCDefaults(arm.Get("defaults", "bmc"), "$.spec.baremetal.defaults.bmc", false)...)
	}
	if variant == "libvirt" {
		host, ok := c.Find(api.Machine, arm.Get("machineRef").Text())
		if ok && !slices.Contains(host.Spec().Get("capabilities").Strings(), "libvirt") {
			issues = add(issues, issue("$.spec.libvirt.machineRef", "libvirt host requires libvirt capability"))
		}
		if raw := arm.Get("uri").Text(); raw != "" {
			uri, err := url.Parse(raw)
			password := false
			if uri != nil && uri.User != nil {
				_, password = uri.User.Password()
			}
			if err != nil || uri.Scheme == "" || password {
				issues = add(issues, issue("$.spec.libvirt.uri", "libvirt URI requires a scheme and forbids inline passwords"))
			}
		}
		bmc := arm.Get("bmcEmulationDefaults")
		if bmc.Has("enabled") && !bmc.Get("enabled").Bool() {
			issues = add(issues, issue("$.spec.libvirt.bmcEmulationDefaults.enabled", "libvirt BMC emulation must be enabled"))
		}
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
		for index, profile := range arm.Get("machineProfiles").Items() {
			path := fmt.Sprintf("$.spec.vsphere.machineProfiles[%d]", index)
			for _, size := range []string{"cpu", "memoryMiB", "diskGiB"} {
				value := profile.Get(size)
				if value.Type() != api.Integer || value.Text() == "0" || strings.HasPrefix(value.Text(), "-") {
					issues = add(issues, issue(path+"."+size, "vSphere machine profiles require a positive capacity"))
				}
			}
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

// bmcPortRange is the contiguous range a provider's emulated BMCs claim: one
// port per hosted Machine in canonical name order, or the base port alone while
// no Machine selects the provider.
func bmcPortRange(provider api.Object, c api.Catalog) (int64, int64, bool) {
	first, ok := provider.Spec().Get("libvirt", "bmcEmulationDefaults", "port").Int64()
	if !ok {
		return 0, 0, false
	}
	machines := int64(0)
	for _, machine := range c.OfKind(api.Machine) {
		if machine.Spec().Get("substrate", "providerRef").Text() == provider.Name() {
			machines++
		}
	}
	return first, first + max(machines, 1) - 1, true
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
