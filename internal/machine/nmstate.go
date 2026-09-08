package machine

import (
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/substrate"
)

func Provider(o api.Object, c api.Catalog) (api.Object, bool) {
	return c.Find(api.InfraProvider, o.Spec().Get("substrate", "providerRef").Text())
}

func NetworkConfig(o api.Object, c api.Catalog) (api.Value, bool) {
	network := o.Spec().Get("network")
	if network.Has("inline") && !network.Has("configRef") {
		return network.Get("inline"), true
	}
	if network.Has("configRef") && !network.Has("inline") {
		configuration, found := c.Find(api.NetworkConfig, network.Get("configRef").Text())
		return configuration.Spec(), found
	}
	return api.Value{}, false
}

func ComposeNetwork(o api.Object, c api.Catalog) (api.Value, []api.Issue) {
	configuration, found := NetworkConfig(o, c)
	if !found {
		return api.Value{}, nil
	}
	native := configuration.Get("nmstate")
	path := "$.spec.network.inline.nmstate"
	if o.Spec().Get("network").Has("configRef") {
		path = "$.spec.network.configRef"
	}
	issues := validateNative(native, path, false)
	overrides := o.Spec().Get("network", "overrides")
	if overrides.Present() {
		issues = appendIssues(issues, validateNative(overrides, "$.spec.network.overrides", true)...)
	}
	if len(issues) != 0 {
		return api.Value{}, issues
	}
	if overrides.Present() {
		var mergeIssues []api.Issue
		native, mergeIssues = mergeNative(native, overrides, "$.spec.network.overrides")
		issues = appendIssues(issues, mergeIssues...)
	}
	issues = appendIssues(issues, validateNative(native, "$.spec.network", false)...)
	if len(issues) != 0 {
		return api.Value{}, issues
	}
	interfaces := native.Get("interfaces").Items()
	assignments := map[string]bool{}
	ips := map[netip.Addr]bool{}
	for index, assignment := range o.Spec().Get("network", "addresses").Items() {
		if !assignment.Has("interface") {
			continue
		}
		field := fmt.Sprintf("$.spec.network.addresses[%d]", index)
		prefix, err := netip.ParsePrefix(assignment.Get("address").Text())
		if err != nil || prefix.Bits() == 0 {
			issues = appendIssues(issues, invariant(field+".address", "interface assignments require an IP address with a nonzero prefix"))
			continue
		}
		position := interfaceIndex(interfaces, assignment.Get("interface").Text())
		if position < 0 || unavailableInterface(interfaces[position]) {
			issues = appendIssues(issues, reference(field+".interface", "assignment must name one available composed interface"))
			continue
		}
		family := "ipv6"
		if prefix.Addr().Is4() {
			family = "ipv4"
		}
		identity := assignment.Get("interface").Text() + "/" + family
		if assignments[identity] || ips[prefix.Addr()] {
			issues = appendIssues(issues, invariant(field, "static IPs and interface-family assignments must be unique"))
			continue
		}
		assignments[identity], ips[prefix.Addr()] = true, true
		mode := interfaces[position].Get(family)
		if mode.Has("enabled") && !mode.Get("enabled").Bool() || mode.Get("dhcp").Bool() || family == "ipv6" && mode.Get("autoconf").Bool() {
			issues = appendIssues(issues, invariant(field+".interface", "static assignment conflicts with disabled or dynamic address configuration"))
			continue
		}
		mode = mode.Default("enabled", api.BoolValue(true)).Default("dhcp", api.BoolValue(false))
		if family == "ipv6" {
			mode = mode.Default("autoconf", api.BoolValue(false))
		}
		mode = mode.With("address", api.ListValue(api.MapValue(api.FieldValue{Name: "ip", Value: api.StringValue(prefix.Addr().String())}, api.FieldValue{Name: "prefix-length", Value: api.IntegerValue(fmt.Sprint(prefix.Bits()))})))
		interfaces[position] = interfaces[position].With(family, mode)
	}
	bindings, bindingIssues := resolveBindings(o, c, native)
	issues = appendIssues(issues, bindingIssues...)
	for index, binding := range bindings.Items() {
		position := interfaceIndex(interfaces, binding.Get("interfaceName").Text())
		nic, exists := namedValue(o.Spec().Get("hardware", "nics"), binding.Get("nicRef").Text())
		if position < 0 || !exists || !nic.Has("macAddress") {
			continue
		}
		mac, ok := canonicalMAC(nic.Get("macAddress").Text())
		if !ok {
			continue
		}
		if raw := interfaces[position].Get("mac-address"); raw.Present() {
			previous, valid := canonicalMAC(raw.Text())
			if !valid || previous != mac {
				issues = appendIssues(issues, invariant(fmt.Sprintf("$.spec.network.interfaceBinding[%d]", index), "native interface MAC conflicts with its bound hardware NIC"))
				continue
			}
		}
		interfaces[position] = interfaces[position].With("mac-address", api.StringValue(mac))
	}
	if len(issues) != 0 {
		return api.Value{}, issues
	}
	if native.Has("interfaces") {
		native = native.With("interfaces", api.ListValue(interfaces...))
	}
	return native, nil
}

func InstallAddress(o api.Object, c api.Catalog) (api.Value, []api.Issue) {
	return selectInstallAddress(o, c, true)
}

func selectInstallAddress(o api.Object, c api.Catalog, required bool) (api.Value, []api.Issue) {
	configuration, found := NetworkConfig(o, c)
	if !found {
		return api.Value{}, nil
	}
	native, issues := ComposeNetwork(o, c)
	if len(issues) != 0 || !native.Present() {
		return api.Value{}, nil
	}
	eligible := []api.Value{}
	defaultRoute := []api.Value{}
	for _, address := range o.Spec().Get("network", "addresses").Items() {
		if !address.Has("interface") {
			continue
		}
		prefix, err := netip.ParsePrefix(address.Get("address").Text())
		if err != nil || !insideNetworks(prefix.Addr(), configuration.Get("machineNetwork")) {
			continue
		}
		eligible = append(eligible, address)
		for _, route := range native.Get("routes", "config").Items() {
			cidr, err := netip.ParsePrefix(route.Get("destination").Text())
			if err == nil && cidr.Bits() == 0 && cidr.Addr().Is4() == prefix.Addr().Is4() && route.Get("state").Text() != "absent" && route.Get("next-hop-interface").Equal(address.Get("interface")) {
				defaultRoute = append(defaultRoute, address)
				break
			}
		}
	}
	if selection := o.Spec().Get("network", "installAddressRef"); selection.Present() {
		for _, address := range eligible {
			if address.Get("name").Equal(selection) {
				return address, nil
			}
		}
		return api.Value{}, []api.Issue{reference("$.spec.network.installAddressRef", "install address must select an interface-assigned IP inside the machine networks")}
	}
	if len(defaultRoute) == 1 {
		return defaultRoute[0], nil
	}
	if len(defaultRoute) > 1 || len(eligible) > 1 {
		return api.Value{}, []api.Issue{invariant("$.spec.network.installAddressRef", "installation address is ambiguous; author an explicit eligible address reference")}
	}
	if len(eligible) == 1 {
		return eligible[0], nil
	}
	if required {
		return api.Value{}, []api.Issue{invariant("$.spec.network.installAddressRef", "this consumer requires a static interface-assigned installation address")}
	}
	return api.Value{}, nil
}

func mergeNative(base, override api.Value, path string) (api.Value, []api.Issue) {
	if base.Type() == api.Mapping && override.Type() == api.Mapping {
		result := base
		issues := []api.Issue{}
		for _, field := range override.Fields() {
			value := field.Value
			if previous := base.Get(field.Name); previous.Present() {
				var nested []api.Issue
				value, nested = mergeNative(previous, value, path)
				issues = appendIssues(issues, nested...)
			}
			result = result.With(field.Name, value)
		}
		return result, issues
	}
	if base.Type() != api.Sequence || override.Type() != api.Sequence {
		return override, nil
	}
	left, right := base.Items(), override.Items()
	style := listMergeStyle(append(slices.Clone(left), right...))
	if style == "invalid" {
		return api.Value{}, []api.Issue{invariant(path, "native list merging requires uniformly named maps or uniformly unnamed maps")}
	}
	result := slices.Clone(left)
	issues := []api.Issue{}
	for index, value := range right {
		position := index
		if style == "named" {
			position = interfaceIndex(result, value.Get("name").Text())
		}
		if position < 0 || position >= len(result) {
			result = append(result, value)
			continue
		}
		merged, nested := mergeNative(result[position], value, path)
		result[position] = merged
		issues = appendIssues(issues, nested...)
	}
	return api.ListValue(result...), issues
}

func listMergeStyle(items []api.Value) string {
	named, unnamed := false, false
	for _, item := range items {
		if item.Type() != api.Mapping {
			return "invalid"
		}
		if item.Get("name").Type() == api.String && item.Get("name").Text() != "" {
			named = true
		} else {
			unnamed = true
		}
	}
	if named && unnamed {
		return "invalid"
	}
	if named {
		return "named"
	}
	return "positional"
}

func validateNative(native api.Value, path string, partial bool) []api.Issue {
	if !native.Present() {
		return nil
	}
	issues := []api.Issue{}
	if native.Has("nameResolutionRefs") {
		issues = appendIssues(issues, invariant(path+".nameResolutionRefs", "Bootwright name-resolution references do not belong inside native NMState"))
	}
	interfaces := native.Get("interfaces")
	if interfaces.Present() && interfaces.Type() != api.Sequence {
		return []api.Issue{typeIssue(path+".interfaces", "native interfaces must be an array")}
	}
	seen := map[string]bool{}
	for index, iface := range interfaces.Items() {
		field := fmt.Sprintf("%s.interfaces[%d]", path, index)
		if iface.Type() != api.Mapping {
			issues = appendIssues(issues, typeIssue(field, "native interfaces must be mappings"))
			continue
		}
		for _, key := range []string{"name", "type", "state", "mac-address"} {
			value := iface.Get(key)
			must := key == "name" || key == "type" && !partial
			if must && !value.Present() || value.Present() && (value.Type() != api.String || value.Text() == "") {
				issues = appendIssues(issues, typeIssue(field+"."+key, "native interface identity and state fields require nonempty strings"))
			}
		}
		if name := iface.Get("name").Text(); name != "" {
			if seen[name] {
				issues = appendIssues(issues, invariant(field+".name", "native interface names must be unique"))
			}
			seen[name] = true
		}
		if mac := iface.Get("mac-address"); mac.Present() {
			if _, ok := canonicalMAC(mac.Text()); !ok {
				issues = appendIssues(issues, invariant(field+".mac-address", "native interface MAC must be EUI-48"))
			}
		}
		for _, family := range []string{"ipv4", "ipv6"} {
			mode := iface.Get(family)
			if mode.Present() && mode.Type() != api.Mapping {
				issues = appendIssues(issues, typeIssue(field+"."+family, "native IP-family configuration must be a mapping"))
				continue
			}
			keys := []string{"enabled", "dhcp"}
			if family == "ipv6" {
				keys = append(keys, "autoconf")
			}
			for _, key := range keys {
				if value := mode.Get(key); value.Present() && value.Type() != api.Boolean {
					issues = appendIssues(issues, typeIssue(field+"."+family+"."+key, "native IP-mode switches must be booleans"))
				}
			}
			if addresses := mode.Get("address"); addresses.Present() {
				if addresses.Type() != api.Sequence {
					issues = appendIssues(issues, typeIssue(field+"."+family+".address", "native static addresses must be an array"))
				} else if addresses.Len() != 0 {
					issues = appendIssues(issues, invariant(field+"."+family+".address", "static addresses must be authored only in Machine network.addresses"))
				}
			}
		}
	}
	if routes := native.Get("routes"); routes.Present() && routes.Type() != api.Mapping {
		issues = appendIssues(issues, typeIssue(path+".routes", "native routes must be a mapping"))
	}
	routes := native.Get("routes", "config")
	if routes.Present() && routes.Type() != api.Sequence {
		issues = appendIssues(issues, typeIssue(path+".routes.config", "native configured routes must be an array"))
	}
	for index, route := range routes.Items() {
		field := fmt.Sprintf("%s.routes.config[%d]", path, index)
		if route.Type() != api.Mapping {
			issues = appendIssues(issues, typeIssue(field, "native routes must be mappings"))
			continue
		}
		for _, key := range []string{"destination", "next-hop-interface", "state"} {
			if value := route.Get(key); value.Present() && (value.Type() != api.String || value.Text() == "") {
				issues = appendIssues(issues, typeIssue(field+"."+key, "native route fields require nonempty strings"))
			}
		}
		if destination := route.Get("destination"); destination.Present() {
			if _, err := netip.ParsePrefix(destination.Text()); err != nil {
				issues = appendIssues(issues, invariant(field+".destination", "native route destination must be a CIDR"))
			}
		}
	}
	return issues
}

func resolveBindings(o api.Object, c api.Catalog, native api.Value) (api.Value, []api.Issue) {
	interfaces := native.Get("interfaces").Items()
	bindings := o.Spec().Get("network", "interfaceBinding")
	provider, found := Provider(o, c)
	required := found && substrate.Variant(provider) == "baremetal" && o.Spec().Get("os", "provided").Present() && !o.Spec().Get("os", "provided").Bool()
	if !bindings.Present() && required {
		values := []api.Value{}
		for _, iface := range interfaces {
			if iface.Get("type").Text() == "ethernet" && !unavailableInterface(iface) {
				values = append(values, api.MapValue(api.FieldValue{Name: "nicRef", Value: iface.Get("name")}, api.FieldValue{Name: "interfaceName", Value: iface.Get("name")}))
			}
		}
		bindings = api.ListValue(values...)
	}
	issues := []api.Issue{}
	seenNIC, seenInterface := map[string]bool{}, map[string]bool{}
	for index, binding := range bindings.Items() {
		field := fmt.Sprintf("$.spec.network.interfaceBinding[%d]", index)
		nicName, interfaceName := binding.Get("nicRef").Text(), binding.Get("interfaceName").Text()
		if _, ok := namedValue(o.Spec().Get("hardware", "nics"), nicName); !ok {
			issues = appendIssues(issues, reference(field+".nicRef", "NIC binding requires one declared hardware NIC; author an explicit complete binding when names differ"))
		}
		position := interfaceIndex(interfaces, interfaceName)
		if position < 0 || unavailableInterface(interfaces[position]) || interfaces[position].Get("type").Text() != "ethernet" {
			issues = appendIssues(issues, reference(field+".interfaceName", "NIC binding requires one available physical ethernet interface"))
		}
		if seenNIC[nicName] || seenInterface[interfaceName] {
			issues = appendIssues(issues, invariant(field, "NICs and physical interfaces may be bound only once"))
		}
		seenNIC[nicName], seenInterface[interfaceName] = true, true
	}
	if required {
		for _, iface := range interfaces {
			if iface.Get("type").Text() == "ethernet" && !unavailableInterface(iface) && !seenInterface[iface.Get("name").Text()] {
				issues = appendIssues(issues, invariant("$.spec.network.interfaceBinding", "bare-metal installation requires complete physical-interface NIC binding"))
			}
		}
	}
	return bindings, issues
}

func interfaceIndex(values []api.Value, name string) int {
	position := -1
	for index, value := range values {
		if value.Get("name").Text() == name {
			if position != -1 {
				return -1
			}
			position = index
		}
	}
	return position
}

func unavailableInterface(value api.Value) bool {
	return value.Get("state").Text() == "absent" || value.Get("state").Text() == "ignore"
}

func insideNetworks(address netip.Addr, networks api.Value) bool {
	for _, network := range networks.Items() {
		prefix, err := netip.ParsePrefix(network.Get("cidr").Text())
		if err == nil && prefix.Contains(address) {
			return true
		}
	}
	return false
}

func canonicalMAC(raw string) (string, bool) {
	mac, err := net.ParseMAC(raw)
	if err != nil || len(mac) != 6 {
		return "", false
	}
	return strings.ToLower(mac.String()), true
}

func namedValue(values api.Value, name string) (api.Value, bool) {
	position := interfaceIndex(values.Items(), name)
	if position < 0 {
		return api.Value{}, false
	}
	return values.Items()[position], true
}

func invariant(field, message string) api.Issue {
	return api.Issue{Code: "api.invariant", Field: field, Message: message, Remediation: "make the Machine declaration consistent with its referenced resources"}
}

func reference(field, message string) api.Issue {
	i := invariant(field, message)
	i.Code = "api.reference"
	return i
}

func typeIssue(field, message string) api.Issue {
	i := invariant(field, message)
	i.Code = "api.type"
	return i
}

func appendIssues(issues []api.Issue, additions ...api.Issue) []api.Issue {
	return append(issues, additions[:min(len(additions), 999-len(issues))]...)
}
