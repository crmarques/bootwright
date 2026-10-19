package installation

import (
	"net"
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/substrate"
)

var installInterfaceTypes = []string{"ethernet", "bond", "vlan"}

var bondModes = []string{"balance-rr", "active-backup", "balance-xor", "broadcast", "802.3ad", "balance-tlb", "balance-alb"}

var (
	kernelInterfaceName = regexp.MustCompile(`^eth[0-9]+$`)
	bondOptionName      = regexp.MustCompile(`^[a-z0-9_]+$`)
	installerArgument   = regexp.MustCompile(`^ifname=[A-Za-z0-9_.+-]{1,15}:[0-9a-f]{2}(:[0-9a-f]{2}){5}$`)
)

// installHost is what the install-interface rules read about the target; a
// target that did not derive is not Known, and the builder refuses it first.
type installHost struct {
	Known, Libvirt, Physical bool
	NICs                     []Interface
}

func hostOfTarget(derived substrate.Target, ok bool) installHost {
	if !ok {
		return installHost{}
	}
	host := installHost{Known: true, Libvirt: derived.Substrate == substrate.ArmLibvirt, Physical: derived.Physical}
	for _, nic := range derived.Interfaces {
		host.NICs = append(host.NICs, Interface{MACAddress: nic.MACAddress, Name: nic.Name})
	}
	return host
}

func hostOfRequest(target Target) installHost {
	host := installHost{Known: true, Libvirt: target.Substrate == substrate.ArmLibvirt, Physical: target.Physical}
	if target.Hardware != nil {
		host.NICs = target.Hardware.Interfaces
	}
	return host
}

type installTopology struct {
	Interface, Type, VLANBase, Bond string
	VLANID                          int
	Ports, Options                  []string
}

func availableOfType(template api.Value, kind string) []api.Value {
	var found []api.Value
	for _, declared := range template.Get("interfaces").Items() {
		if available(declared) && declared.Get("type").Text() == kind {
			found = append(found, declared)
		}
	}
	return found
}

func availableNamed(template api.Value, name string, kinds ...string) (api.Value, bool) {
	declared, found := findNamed(template.Get("interfaces"), "name", name)
	return declared, found && available(declared) && slices.Contains(kinds, declared.Get("type").Text())
}

func installBond(template, declared api.Value) (api.Value, bool) {
	switch declared.Get("type").Text() {
	case "bond":
		return declared, true
	case "vlan":
		base := declared.Get("vlan", "base-iface")
		if base.Type() != api.String {
			return api.Value{}, false
		}
		return availableNamed(template, base.Text(), "bond")
	}
	return api.Value{}, false
}

func installInterfaceRefusal(catalog api.Catalog, machine api.Object, template api.Value, iface string, host installHost) (reason, remediation string) {
	identity := machine.Identity()
	ethernet := availableOfType(template, "ethernet")
	for _, declared := range ethernet {
		if name := declared.Get("name").Text(); kernelInterfaceName.MatchString(name) {
			return "a managed-OS installation names each ethernet interface after its hardware address, and a kernel-style name such as " + name + " can collide with the names the kernel assigns",
				"rename interface " + name + " in the network of " + identity + " to a name outside the kernel's ethN scheme, such as its predictable name"
		}
	}
	if host.Known && host.Physical {
		for _, declared := range ethernet {
			if name := declared.Get("name").Text(); !hardwareMatches(machine, host.NICs, declared) {
				return unboundReason(name, identity), unboundRemedy(name, identity)
			}
		}
	}
	declared, found := findNamed(template.Get("interfaces"), "name", iface)
	if kind := declared.Get("type").Text(); found && !slices.Contains(installInterfaceTypes, kind) {
		return "the installer configures only an ethernet, bond or VLAN install interface, and the install interface " + iface + " of " + identity + " is of type " + kind,
			"assign the install address of " + identity + " to an ethernet, bond or VLAN interface"
	}
	if bond, ok := installBond(template, declared); ok {
		if reason, remediation := bondRefusal(template, bond, identity, host); reason != "" {
			return reason, remediation
		}
	}
	if declared.Get("type").Text() == "vlan" {
		if reason, remediation := vlanRefusal(catalog, machine, template, declared, host); reason != "" {
			return reason, remediation
		}
	}
	spec, err := substrate.NetworkSpec(catalog, machine)
	if err == nil && len(spec.Get("dns").Items()) != 0 && template.Has("dns-resolver", "config", "server") {
		return "the installed name servers of " + identity + " are its network's DNS server selections, and its native dns-resolver.config.server declares them a second time",
			"remove dns-resolver.config.server from the network of " + identity + ", or remove its DNS server selections"
	}
	return "", ""
}

func unboundReason(name, identity string) string {
	return "a managed-OS installation names each ethernet interface after its hardware address, and interface " + name + " of " + identity +
		" resolves to no declared hardware NIC, or to one whose address differs from its mac-address"
}

func unboundRemedy(name, identity string) string {
	return "bind interface " + name + " to its hardware NIC in spec.network.interfaceBinding of " + identity + ", or mark it absent in its network"
}

func hardwareMatches(machine api.Object, nics []Interface, declared api.Value) bool {
	resolved, found := interfaceMAC(machine, nics, declared.Get("name").Text())
	if !found {
		return false
	}
	authored := declared.Get("mac-address")
	if !authored.Present() {
		return true
	}
	parsed, err := net.ParseMAC(authored.Text())
	return err == nil && strings.ToLower(parsed.String()) == resolved
}

func bondRefusal(template, bond api.Value, identity string, host installHost) (reason, remediation string) {
	name := bond.Get("name").Text()
	aggregation := bond.Get("link-aggregation")
	ports := aggregation.Get("port")
	carried := ports.Type() == api.Sequence && ports.Len() != 0
	for _, port := range ports.Items() {
		declared, ok := availableNamed(template, port.Text(), "ethernet")
		controller := declared.Get("controller")
		carried = carried && port.Type() == api.String && ok && (!controller.Present() || controller.Text() == name)
	}
	if !carried {
		return "the installer configures a bonded install interface from its named ports, and bond " + name + " of " + identity +
				" names no port, or a port that is not an available ethernet interface aggregated by it",
			"list the ethernet interfaces bond " + name + " aggregates in its link-aggregation.port in the network of " + identity
	}
	mode := aggregation.Get("mode")
	if mode.Type() != api.String || !slices.Contains(bondModes, mode.Text()) {
		declaredMode := "no mode"
		if kind := mode.Type(); kind == api.String || kind == api.Integer || kind == api.Number {
			declaredMode = "mode " + mode.Text()
		}
		return "the installer configures a bond only by its full mode name, and bond " + name + " of " + identity + " declares " + declaredMode,
			"set link-aggregation.mode of " + name + " to one of balance-rr, active-backup, balance-xor, broadcast, 802.3ad, balance-tlb or balance-alb in the network of " + identity
	}
	if host.Known && host.Libvirt && mode.Text() != "active-backup" {
		return "a libvirt bridge qualifies only active-backup bonding, and bond " + name + " of " + identity + " declares mode " + mode.Text(),
			"set link-aggregation.mode of " + name + " on " + identity + " to active-backup; the other modes are qualified on physical hosts (B78)"
	}
	if option := uncarriedBondOption(aggregation.Get("options")); option != "" {
		return "the installer receives bond options as one option list, and link-aggregation.options." + option + " of " + name + " on " + identity + " cannot be written into it",
			"remove link-aggregation.options." + option + " from " + name + " in the network of " + identity + ", or give it a plain value"
	}
	return "", ""
}

func uncarriedBondOption(options api.Value) string {
	if options.Present() && options.Type() != api.Mapping {
		return "*"
	}
	fields := options.Fields()
	slices.SortFunc(fields, func(x, y api.FieldValue) int { return strings.Compare(x.Name, y.Name) })
	for _, option := range fields {
		value, ok := bondOptionValue(option.Value)
		if !bondOptionName.MatchString(option.Name) || option.Name == "mode" || !ok ||
			strings.ContainsAny(value, `;="'\#`) || strings.IndexFunc(value, unicode.IsSpace) >= 0 {
			return option.Name
		}
	}
	return ""
}

func bondOptionValue(value api.Value) (string, bool) {
	switch value.Type() {
	case api.String, api.Integer, api.Number:
		return value.Text(), true
	case api.Boolean:
		return strconv.FormatBool(value.Bool()), true
	}
	return "", false
}

func vlanRefusal(catalog api.Catalog, machine api.Object, template, declared api.Value, host installHost) (reason, remediation string) {
	identity, name := machine.Identity(), declared.Get("name").Text()
	vlan := declared.Get("vlan")
	base := vlan.Get("base-iface")
	_, based := availableNamed(template, base.Text(), "ethernet", "bond")
	id, numbered := vlan.Get("id").Int64()
	protocol := vlan.Get("protocol")
	if base.Type() != api.String || !based || !numbered || id < 1 || id > 4094 ||
		protocol.Present() && (protocol.Type() != api.String || protocol.Text() != "802.1q") || !vlanName(name, strconv.FormatInt(id, 10)) {
		return "the installer configures a VLAN install interface only as an 802.1Q VLAN with an id from 1 to 4094 over an available ethernet or bond interface, named vlan and its id, a prefix, a dot and its id, or a name with no dot that does not start with vlan, and " +
				name + " of " + identity + " is not one",
			"declare " + name + " in the network of " + identity + " with vlan.base-iface an available ethernet or bond interface, vlan.id from 1 to 4094, no vlan.protocol other than 802.1q, and a name of one of those forms"
	}
	if !host.Known || !host.Libvirt {
		return "", ""
	}
	provider, found := machineref.Provider(machine, catalog)
	if !found {
		return "", ""
	}
	attachment, attached := findNamed(provider.Spec().Get("networkAttachments"), "name", machine.Spec().Get("network", "attachmentRef").Text())
	if attached && attachment.Get("libvirt", "management").Text() == "managed" {
		return "a managed libvirt network gives its host an untagged address on " + attachment.Get("libvirt", "bridge").Text() +
				", so the controller cannot reach the VLAN install address of " + identity,
			"attach " + identity + " to an external bridge whose host carries VLAN " + strconv.FormatInt(id, 10) + ", or install over an untagged interface"
	}
	return "", ""
}

func vlanName(name, id string) bool {
	if strings.HasPrefix(name, "vlan") {
		return name == "vlan"+id
	}
	prefix, suffix, dotted := strings.Cut(name, ".")
	return !dotted || prefix != "" && suffix == id
}

func readInstallTopology(template api.Value, iface string) installTopology {
	topology := installTopology{Interface: iface, Type: "ethernet"}
	declared, found := findNamed(template.Get("interfaces"), "name", iface)
	if !found {
		return topology
	}
	topology.Type = declared.Get("type").Text()
	if topology.Type == "vlan" {
		topology.VLANBase = declared.Get("vlan", "base-iface").Text()
		id, _ := declared.Get("vlan", "id").Int64()
		topology.VLANID = int(id)
	}
	bond, ok := installBond(template, declared)
	if !ok {
		return topology
	}
	aggregation := bond.Get("link-aggregation")
	topology.Bond, topology.Ports = bond.Get("name").Text(), aggregation.Get("port").Strings()
	topology.Options = []string{"mode=" + aggregation.Get("mode").Text()}
	fields := aggregation.Get("options").Fields()
	slices.SortFunc(fields, func(x, y api.FieldValue) int { return strings.Compare(x.Name, y.Name) })
	for _, option := range fields {
		value, _ := bondOptionValue(option.Value)
		topology.Options = append(topology.Options, option.Name+"="+value)
	}
	return topology
}

func installGatewayRoute(template api.Value, iface string) (index int, gateway string) {
	for index, route := range template.Get("routes", "config").Items() {
		hop := route.Get("next-hop-interface")
		if route.Get("state").Text() == "absent" || !substrate.DefaultRouteIPv4(route.Get("destination").Text()) || hop.Present() && hop.Text() != iface {
			continue
		}
		next := route.Get("next-hop-address").Text()
		if address, err := netip.ParseAddr(next); err == nil && address.Is4() {
			return index, next
		}
	}
	return -1, ""
}

func interfaceMAC(machine api.Object, nics []Interface, name string) (string, bool) {
	nic := name
	for _, binding := range machine.Spec().Get("network", "interfaceBinding").Items() {
		if binding.Get("interfaceName").Text() == name {
			nic = binding.Get("nicRef").Text()
			break
		}
	}
	for _, declared := range nics {
		if declared.Name == nic {
			parsed, err := net.ParseMAC(declared.MACAddress)
			return strings.ToLower(parsed.String()), err == nil && len(parsed) == 6
		}
	}
	return "", false
}

func installerArguments(catalog api.Catalog, machine api.Object, request Request) ([]string, error) {
	names, err := substrate.EthernetInterfaces(catalog, machine)
	if err != nil {
		return nil, err
	}
	arguments := make([]string, 0, len(names))
	for _, name := range names {
		mac := substrate.InterfaceMAC(request.Identity.Context, machine.Name(), name)
		if request.Target.Hardware != nil {
			resolved, found := interfaceMAC(machine, request.Target.Hardware.Interfaces, name)
			if !found {
				return nil, refusal("lifecycle.unsupported", unboundReason(name, machine.Identity()), unboundRemedy(name, machine.Identity()))
			}
			mac = resolved
		}
		argument := "ifname=" + name + ":" + mac
		if !installerArgument.MatchString(argument) {
			return nil, refusal("lifecycle.state", "the installer argument "+argument+" is not an interface name pinned to a hardware address",
				"correct the interface names in the network of "+machine.Identity())
		}
		arguments = append(arguments, argument)
	}
	return arguments, nil
}
