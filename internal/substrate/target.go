package substrate

import (
	"net/netip"
	"net/url"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// The substrate arms an InfraProvider may declare. The arm is authored intent
// rather than an implementation identity, so deriving from it here is the same
// reading admission performs.
const (
	ArmLibvirt   = "libvirt"
	ArmBaremetal = "baremetal"
	ArmVSphere   = "vsphere"
	ArmKubeVirt  = "kubevirt"
)

// Arms lists every arm an InfraProvider may declare, in the order admission
// reads them. Declaring more than one selects none.
func Arms() []string { return []string{ArmBaremetal, ArmLibvirt, ArmVSphere, ArmKubeVirt} }

// Realized lists the arms this executable can realize, in canonical order.
// Every other arm refuses before an operation registers.
func Realized() []string { return []string{ArmBaremetal, ArmLibvirt} }

// RealizesPhysicalNICs reports whether a provider's machines are operator-owned
// hardware whose interfaces exist before Bootwright realizes anything. A
// consumer asks this rather than naming an arm, so adding one changes this
// answer alone.
func RealizesPhysicalNICs(provider api.Object) bool { return Variant(provider) == ArmBaremetal }

// The identity channels a substrate offers to prove what a machine holds.
// Which one a Machine uses is fixed by its substrate and frozen with the
// request, so a consumer asks for a marker and a host key and never learns
// which mechanism answered.
const (
	// ChannelGuestAgent reads a bounded guest file out of band, through the
	// hypervisor's own channel into the guest.
	ChannelGuestAgent = "guest-agent"
	// ChannelDeliveredKey is for a machine with no such channel. The
	// installation delivers the host key the machine will present, so the key
	// is known before the machine is ever contacted and the read is an SSH
	// connection pinned to exactly it.
	ChannelDeliveredKey = "delivered-key"
)

// The virtual-media trust a controller's fetch of published media uses. It is
// the BMC-to-artifact-server leg and is independent of the controller's own.
const (
	TrustDisableVerification = "disable-verification"
	TrustImportCertificate   = "import-certificate"
	TrustEstablished         = "established"
)

// Controller is the management controller one Machine is reached through, and
// the trust each of its two legs carries.
type Controller struct {
	// Endpoint is the exact ComputerSystem resource, absolute and normalized.
	Endpoint       string
	CredentialsRef string
	// TLSVerify covers the controller-to-BMC leg. An opt-out is declared per
	// endpoint and never becomes a default.
	TLSVerify bool
	// TrustBundleRef names the caBundle Secret that is the one anchor of the
	// controller-to-BMC leg when set; the system trust store anchors it
	// otherwise. Admission refuses it beside TLSVerify false.
	TrustBundleRef string
	VirtualMedia   VirtualMedia
}

// VirtualMedia is the BMC-to-artifact-server leg: how the controller is made
// to trust the server it fetches published media from.
type VirtualMedia struct {
	Trust               string
	RestoreVerification bool
	RemoveCertificate   bool
}

// Identity is the channel that proves what a machine holds.
type Identity struct {
	Channel string
	// URI and Domain address the hypervisor a guest-agent channel reaches
	// through; both are empty on every other channel.
	URI    string
	Domain string
	// HostKeyRef names the sshKeyPair a delivered-key installation installs as
	// the machine's own host key, and is empty on every other channel.
	HostKeyRef string
}

// Interface is one declared physical NIC, by the name and address the
// hardware must report for this Machine to be the one the operator meant.
type Interface struct {
	Name       string
	MACAddress string
}

// Target is everything a consumer of a realized Machine needs, derived once
// from the substrate its provider declares. It answers which controller boots
// the machine, which channel proves what it holds, whether it is operator-owned
// hardware, and which Machine's host reaches that controller.
//
// Consumers read this and name no substrate, so an arm added later reaches
// every one of them without changing any. Placement is named rather than
// resolved because deriving it is application work over a Machine's access
// declaration, which this domain does not own.
type Target struct {
	Provider  string
	Substrate string
	// Physical is an operator-owned machine that exists before Bootwright and
	// outlives this context: its installation erases what is already there and
	// its removal retains it.
	Physical   bool
	Controller Controller
	Identity   Identity
	// Interfaces are the NICs this machine presents, in declared order, by the
	// name and hardware address it reports. A physical machine declares them
	// and must prove them; a machine its substrate creates presents the
	// addresses that realization derived. A consumer that has to name this
	// machine's hardware to an installer reads them here rather than learning
	// how each substrate assigns one.
	Interfaces []Interface
	// RootDeviceHints select the whole disk an installation is permitted to
	// erase, exactly as the Machine declares them. Which of them an
	// installation can carry is that installation's own rule.
	RootDeviceHints RootDeviceHints
	// PlacementMachine is the Machine whose host reaches the controller.
	PlacementMachine api.Object
}

// TargetFor derives the realized target of one Machine. It reads no host,
// endpoint or Secret material and performs no effect.
//
// A Machine on no provider is realized by nothing, and is a target at all only
// because it authors its own management controller. It is physical for the
// same reason a bare-metal Machine is: the hardware exists without Bootwright,
// so reaching it needs no realization to have happened first.
func TargetFor(catalog api.Catalog, machine api.Object, contextName, controllerMachine string) (Target, error) {
	reference := machine.Spec().Get("substrate", "providerRef").Text()
	if reference == "" {
		return authoredTarget(catalog, machine, controllerMachine)
	}
	provider, found := catalog.Find(api.InfraProvider, reference)
	if !found {
		return Target{}, refusal("api.reference", "the Machine's provider is not in the selected graph",
			"declare "+reference+" or correct spec.substrate.providerRef on "+machine.Identity())
	}
	arm := Variant(provider)
	if !slices.Contains(Realized(), arm) {
		return Target{}, refusal("lifecycle.state", "this executable does not realize the substrate of "+provider.Identity(),
			"place "+machine.Identity()+" on a libvirt or bare-metal provider")
	}
	target := Target{Provider: provider.Name(), Substrate: arm}
	if arm == ArmBaremetal {
		return physicalTarget(catalog, provider, machine, controllerMachine, target)
	}
	return virtualTarget(catalog, provider, machine, contextName, target)
}

// authoredTarget is a Machine reached only through the controller it declares.
// It carries no identity channel and no hardware proof, because nothing here
// installs it: the one thing this answers is where to reach it.
func authoredTarget(catalog api.Catalog, machine api.Object, controllerMachine string) (Target, error) {
	controller, err := authoredController(machine)
	if err != nil {
		return Target{}, err
	}
	host, ok := catalog.Find(api.Machine, controllerMachine)
	if !ok {
		return Target{}, refusal("api.reference", "the Environment's controller Machine is not in the selected graph",
			"declare it or correct spec.controller.machineRef")
	}
	return Target{Physical: true, Controller: controller, PlacementMachine: host}, nil
}

// authoredController reads the management controller a Machine declares. It is
// the one reader of that block, so an authored controller means the same thing
// to every consumer whether or not a provider also offers one.
func authoredController(machine api.Object) (Controller, error) {
	bmc := machine.Spec().Get("hardware", "management", "bmc")
	if !bmc.Present() {
		return Controller{}, refusal("api.required", "the Machine declares no management controller",
			"set spec.hardware.management.bmc on "+machine.Identity())
	}
	endpoint, ok := NormalizeControllerEndpoint(bmc.Get("address").Text())
	if !ok {
		return Controller{}, refusal("api.value", "the Machine's controller address does not select one exact ComputerSystem",
			"set spec.hardware.management.bmc.address to an absolute /redfish/v1/Systems/<id> URL on "+machine.Identity())
	}
	credentials := bmc.Get("credentialsRef").Text()
	if credentials == "" {
		return Controller{}, refusal("api.required", "the Machine's management controller declares no credential",
			"set spec.hardware.management.bmc.credentialsRef on "+machine.Identity())
	}
	return Controller{
		Endpoint: endpoint, CredentialsRef: credentials,
		TLSVerify:      !bmc.Get("tls").Has("verify") || bmc.Get("tls", "verify").Bool(),
		TrustBundleRef: bmc.Get("tls", "trustBundleRef").Text(),
		VirtualMedia:   virtualMediaTrust(bmc.Get("virtualMedia", "tls")),
	}, nil
}

// virtualTarget derives a Machine the substrate creates. Its controller is
// emulated at the port its provider's own allocation rule assigns, and its
// identity channel reaches into the guest through the hypervisor.
func virtualTarget(catalog api.Catalog, provider, machine api.Object, contextName string, target Target) (Target, error) {
	port, ok := ControllerPort(catalog, provider, machine.Name())
	if !ok {
		return Target{}, refusal("api.value", "the Machine's emulated controller port does not allocate",
			"correct spec.libvirt.bmcEmulationDefaults.port on "+provider.Identity())
	}
	credentials := provider.Spec().Get("libvirt", "bmcEmulationDefaults", "auth", "credentialsRef").Text()
	if credentials == "" {
		return Target{}, refusal("api.required", "the provider declares no emulated controller credential",
			"set spec.libvirt.bmcEmulationDefaults.auth.credentialsRef on "+provider.Identity())
	}
	host, ok := catalog.Find(api.Machine, provider.Spec().Get("libvirt", "machineRef").Text())
	if !ok {
		return Target{}, refusal("api.reference", "the provider's host Machine is not in the selected graph",
			"declare it or correct spec.libvirt.machineRef on "+provider.Identity())
	}
	address := provider.Spec().Get("libvirt", "bmcEmulationDefaults", "bindAddress").Text()
	target.Controller = Controller{
		Endpoint:       ControllerEndpoint(address, port, DomainUUID(contextName, machine.Name())),
		CredentialsRef: credentials,
		// The emulator serves plain HTTP, so there is no leg to verify, and it
		// is configured to fetch media without verifying the artifact server.
		// Neither is a per-boot action the installation performs.
		TLSVerify:    true,
		VirtualMedia: VirtualMedia{Trust: TrustEstablished},
	}
	target.Identity = Identity{
		Channel: ChannelGuestAgent,
		URI:     provider.Spec().Get("libvirt", "uri").Text(),
		Domain:  DomainName(contextName, machine.Name()),
	}
	names, err := EthernetInterfaces(catalog, machine)
	if err != nil {
		return Target{}, err
	}
	for _, name := range names {
		target.Interfaces = append(target.Interfaces, Interface{
			Name: name, MACAddress: InterfaceMAC(contextName, machine.Name(), name),
		})
	}
	target.RootDeviceHints = rootDeviceHints(machine)
	target.PlacementMachine = host
	return target, nil
}

// physicalTarget derives an operator-owned machine on a bare-metal provider.
// It adds to the authored controller the two things an installation needs: the
// hardware it must prove before erasing anything, and the key it will deliver
// so the machine can be recognized afterwards.
func physicalTarget(catalog api.Catalog, provider, machine api.Object, controllerMachine string, target Target) (Target, error) {
	controller, err := authoredController(machine)
	if err != nil {
		return Target{}, err
	}
	interfaces, err := declaredInterfaces(machine)
	if err != nil {
		return Target{}, err
	}
	host, ok := catalog.Find(api.Machine, controllerMachine)
	if !ok {
		return Target{}, refusal("api.reference", "the Environment's controller Machine is not in the selected graph",
			"declare it or correct spec.controller.machineRef")
	}
	target.Physical = true
	target.Controller = controller
	target.Identity = Identity{
		Channel: ChannelDeliveredKey, HostKeyRef: machine.Spec().Get("os", "install", "hostKeyRef").Text(),
	}
	target.Interfaces = interfaces
	target.RootDeviceHints = rootDeviceHints(machine)
	target.PlacementMachine = host
	return target, nil
}

// virtualMediaTrust freezes the BMC-to-artifact-server leg. Importing the
// server's certificate is the default, and each settle option is frozen only
// under the one trust that acts on it.
func virtualMediaTrust(tls api.Value) VirtualMedia {
	media := VirtualMedia{Trust: tls.Get("trust").Text()}
	if media.Trust == "" {
		media.Trust = TrustImportCertificate
	}
	switch media.Trust {
	case TrustDisableVerification:
		media.RestoreVerification = !tls.Has("restoreVerificationAfterBoot") || tls.Get("restoreVerificationAfterBoot").Bool()
	case TrustImportCertificate:
		media.RemoveCertificate = tls.Get("removeCertificateAfterBoot").Bool()
	}
	return media
}

// declaredInterfaces reads the NICs a physical Machine must prove, in declared
// order, with the canonical spelling admission normalized them to.
func declaredInterfaces(machine api.Object) ([]Interface, error) {
	var interfaces []Interface
	for _, nic := range machine.Spec().Get("hardware", "nics").Items() {
		address := nic.Get("macAddress").Text()
		if address == "" {
			return nil, refusal("api.required", "a declared NIC carries no hardware address",
				"set every hardware.nics[].macAddress on "+machine.Identity())
		}
		interfaces = append(interfaces, Interface{Name: nic.Get("name").Text(), MACAddress: strings.ToLower(address)})
	}
	if len(interfaces) == 0 {
		return nil, refusal("api.required", "the Machine declares no hardware NIC to prove it by",
			"declare hardware.nics on "+machine.Identity())
	}
	return interfaces, nil
}

// NormalizeControllerEndpoint is the one grammar of a controller address. It
// rewrites nothing: it returns the address byte for byte, or refuses it, so
// admission, the claim key and every request the target sends read one text.
// The address is an absolute http or https URL with a lowercase scheme, a
// canonical host and port, no userinfo, query, fragment or percent-encoding,
// and the path /redfish/v1/Systems/<id> for one id of RFC 3986 unreserved
// characters.
// A collection, a child resource or any second spelling is refused: the
// address is what a destructive operation is aimed at, so it may not be
// ambiguous.
func NormalizeControllerEndpoint(address string) (string, bool) {
	if !api.ValidLexical("http-url", address) || !strings.HasPrefix(address, "http://") && !strings.HasPrefix(address, "https://") {
		return "", false
	}
	for index := range len(address) {
		if b := address[index]; b < 0x21 || b > 0x7e || b == '?' || b == '#' || b == '%' {
			return "", false
		}
	}
	parsed, err := url.Parse(address)
	if err != nil || !canonicalControllerHost(parsed.Hostname()) || strings.HasPrefix(parsed.Port(), "0") {
		return "", false
	}
	identity, found := strings.CutPrefix(parsed.Path, "/redfish/v1/Systems/")
	if !found || !unreservedSystemIdentity(identity) {
		return "", false
	}
	return address, true
}

// canonicalControllerHost admits an IP literal only in the spelling netip
// prints, and a DNS name only when its last label is not all digits, because
// glibc reads a name such as 192.000.002.001 or 3232235777 as an IPv4 address
// in another spelling, and two spellings would be two claims on one server.
func canonicalControllerHost(host string) bool {
	if address, err := netip.ParseAddr(host); err == nil {
		return address.String() == host
	}
	return strings.Trim(host[strings.LastIndex(host, ".")+1:], "0123456789") != ""
}

func unreservedSystemIdentity(identity string) bool {
	if identity == "" || identity == "." || identity == ".." {
		return false
	}
	for index := range len(identity) {
		b := identity[index]
		if !('a' <= b && b <= 'z' || 'A' <= b && b <= 'Z' || '0' <= b && b <= '9' || b == '-' || b == '.' || b == '_' || b == '~') {
			return false
		}
	}
	return true
}

// ControllerReservationKey claims one physical machine by its controller, so
// two contexts never drive one server. The host and port come from the
// endpoint itself, which is already normalized.
func ControllerReservationKey(endpoint string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "bmc:" + endpoint
	}
	host := parsed.Host
	if parsed.Port() == "" {
		port := "80"
		if parsed.Scheme == "https" {
			port = "443"
		}
		host = bracketed(parsed.Hostname()) + ":" + port
	}
	return "bmc:" + host + strings.TrimPrefix(parsed.Path, "/redfish/v1/Systems")
}

// bracketed wraps a literal IPv6 address so the colon separating host from
// port never becomes ambiguous.
func bracketed(host string) string {
	if address, err := netip.ParseAddr(host); err == nil && address.Is6() {
		return "[" + host + "]"
	}
	return host
}

// ProvidersOn lists the providers of one substrate arm in canonical name order.
func ProvidersOn(catalog api.Catalog, arm string) []api.Object {
	var found []api.Object
	for _, provider := range catalog.OfKind(api.InfraProvider) {
		if Variant(provider) == arm {
			found = append(found, provider)
		}
	}
	slices.SortFunc(found, func(x, y api.Object) int { return strings.Compare(x.Name(), y.Name()) })
	return found
}

// Unrealized is one selected object this executable cannot realize, why, and
// what the operator changes.
type Unrealized struct {
	Object      api.Object
	Reason      string
	Remediation string
}

// Unrealizable lists every selected object this executable cannot realize, in
// canonical order: a provider on an arm no capability implements, and every
// non-provided Machine hosted on one. It has one owner so that adding an arm
// narrows the refusal in exactly one place.
func Unrealizable(catalog api.Catalog) []Unrealized {
	realized := "a " + strings.Join(Realized(), " or ") + " provider"
	var found []Unrealized
	for _, provider := range catalog.OfKind(api.InfraProvider) {
		if slices.Contains(Realized(), Variant(provider)) {
			continue
		}
		reason := "this executable realizes no provider on the substrate it declares"
		found = append(found, Unrealized{Object: provider, Reason: reason,
			Remediation: "remove " + provider.Identity() + " and the Machines it hosts from the selected Environment, or declare them on " + realized})
		for _, machine := range HostedMachines(catalog, provider.Name()) {
			found = append(found, Unrealized{Object: machine,
				Reason:      "its provider " + provider.Identity() + " is on a substrate this executable does not realize",
				Remediation: "host " + machine.Identity() + " on " + realized + ", declare its operating system provided, or remove it from the selected Environment"})
		}
	}
	slices.SortFunc(found, func(x, y Unrealized) int { return strings.Compare(x.Object.Identity(), y.Object.Identity()) })
	return found
}
