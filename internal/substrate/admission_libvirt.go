package substrate

import (
	"fmt"
	"net/netip"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/secrets"
)

func normalizeLibvirt(arm api.Value) api.Value {
	// One listener has one spelling, so its endpoint, its socket key and
	// every comparison with another listener read the text netip prints.
	if address, err := netip.ParseAddr(arm.Get("bmcEmulation", "bindAddress").Text()); err == nil {
		return arm.WithPath(api.StringValue(address.String()), "bmcEmulation", "bindAddress")
	}
	return arm
}

func validateLibvirt(o api.Object, c api.Catalog) []api.Issue {
	arm := o.Spec().Get(ArmLibvirt)
	bmc := arm.Get("bmcEmulation")
	issues := []api.Issue{}
	issues = api.AppendIssues(issues, validateEmulatedListener(bmc)...)
	issues = api.AppendIssues(issues, validateEmulatedCertificate(bmc, c)...)
	issues = api.AppendIssues(issues, requirePositiveCapacity(arm, ArmLibvirt, "libvirt")...)
	issues = api.AppendIssues(issues, validateLibvirtDataDisks(o, arm)...)
	first, last, ranged := bmcPortRange(o, c)
	if ranged && last > 65535 {
		issues = api.AppendIssues(issues, issue("$.spec.libvirt.bmcEmulation.port", "the emulated BMC port range must end at or below 65535"))
	}
	for _, other := range c.OfKind(api.InfraProvider) {
		peer := other.Spec().Get(ArmLibvirt)
		if other.Identity() == o.Identity() || !peer.Present() || !peer.Get("machineRef").Equal(arm.Get("machineRef")) {
			continue
		}
		peerFirst, peerLast, peerRanged := bmcPortRange(other, c)
		if ranged && peerRanged && first <= peerLast && peerFirst <= last {
			issues = api.AppendIssues(issues, issue("$.spec.libvirt.bmcEmulation.port", "emulated BMC port ranges of providers on one host must not overlap"))
			break
		}
	}
	return issues
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

// CertificateNames is the address each libvirt provider's emulated BMC serves
// HTTPS at with the Secret its bmcEmulation.certificateRef names.
func CertificateNames(c api.Catalog, secret string) []string {
	var names []string
	for _, provider := range ProvidersOn(c, ArmLibvirt) {
		bmc := provider.Spec().Get(ArmLibvirt, "bmcEmulation")
		if bmc.Get("certificateRef").Text() == secret && NameableListener(bmc.Get("bindAddress").Text()) {
			names = append(names, bmc.Get("bindAddress").Text())
		}
	}
	return names
}

// validateEmulatedListener judges only an address that is present: the schema
// already refuses an absent one, and a second diagnostic would say it twice.
func validateEmulatedListener(bmc api.Value) []api.Issue {
	address := bmc.Get("bindAddress")
	if !address.Present() || NameableListener(address.Text()) {
		return nil
	}
	return []api.Issue{issue("$.spec.libvirt.bmcEmulation.bindAddress",
		"the emulated BMC listens on one unicast address its Machines' BMC endpoints can name: not unspecified, multicast or the IPv4 broadcast address, and not an IPv6 link-local, zoned or IPv4-mapped address")}
}

// validateEmulatedCertificate holds a generated serving certificate to the
// nameable bindAddress; reference validation refuses a missing or mistyped
// Secret, and a stored one is proved only when a fresh apply binds it.
func validateEmulatedCertificate(bmc api.Value, c api.Catalog) []api.Issue {
	address, name := bmc.Get("bindAddress").Text(), bmc.Get("certificateRef").Text()
	secret, found := c.Find(api.Secret, name)
	if !NameableListener(address) || !found || secret.Spec().Get("type").Text() != "tlsCertificate" {
		return nil
	}
	generated := secret.Spec().Get("source", "generated")
	if !generated.Present() || secrets.GeneratedCertificateNames(generated, address) {
		return nil
	}
	return []api.Issue{{Code: "api.invariant", Field: "$.spec.libvirt.bmcEmulation.certificateRef",
		Message: "the emulated BMC serves HTTPS at " + address + ", which the generated certificate of Secret/" + name + " does not name",
		Remediation: "add " + address + " to spec.source.generated.ipAddresses of Secret/" + name + ", import the change with bootwright context update --name <context> --input-dir <dir>, " +
			"then run bootwright secret generate --name " + name + " --context <context>"}}
}

// bmcPortRange is the contiguous range a provider's emulated BMCs claim: one
// port per hosted Machine in canonical name order, or the base port alone while
// no Machine selects the provider.
func bmcPortRange(provider api.Object, c api.Catalog) (int64, int64, bool) {
	first, ok := provider.Spec().Get(ArmLibvirt, "bmcEmulation", "port").Int64()
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
			issues = api.AppendIssues(issues, api.Issue{Code: "api.invariant", Field: path,
				Message:     fmt.Sprintf("a libvirt domain presents at most %d data disks beside its root disk", MaxDataDisks),
				Remediation: fmt.Sprintf("declare at most %d dataDisks on spec.libvirt.machineProfiles[%d] of %s", MaxDataDisks, index, o.Identity())})
		}
		for disk, data := range disks.Items() {
			if data.Get("name").Text() == "root" {
				issues = api.AppendIssues(issues, api.Issue{Code: "api.invariant", Field: fmt.Sprintf("%s[%d].name", path, disk),
					Message:     "the data disk name root is reserved for the root disk, which the domain presents as vda from root.qcow2",
					Remediation: fmt.Sprintf("rename spec.libvirt.machineProfiles[%d].dataDisks[%d] on %s", index, disk, o.Identity())})
			}
		}
	}
	return issues
}

func ExternalBridges(provider api.Object) []string {
	var bridges []string
	for _, segment := range provider.Spec().Get("segments").Items() {
		if bridge := segment.Get(ArmLibvirt, "bridge").Text(); bridge != "" {
			bridges = append(bridges, bridge)
		}
	}
	slices.Sort(bridges)
	return slices.Compact(bridges)
}
