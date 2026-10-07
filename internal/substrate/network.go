package substrate

import (
	"net/netip"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// NetworkSpec resolves the network configuration a Machine selects, whether it
// references a NetworkConfig or declares one inline. Realization reads its
// interfaces and installation reads its resolvers and routes, so both take the
// same value from one place.
func NetworkSpec(catalog api.Catalog, machine api.Object) (api.Value, error) {
	network := machine.Spec().Get("network")
	if inline := network.Get("inline"); inline.Present() {
		return inline, nil
	}
	config, found := catalog.Find(api.NetworkConfig, network.Get("configRef").Text())
	if !found {
		return api.Value{}, refusal("api.reference", "the Machine's network configuration is not in the selected graph", "declare it or correct spec.network.configRef on "+machine.Identity())
	}
	return config.Spec(), nil
}

// NetworkTemplate is the native NMState map of that configuration with the
// Machine's overrides merged in, as admission composes it.
func NetworkTemplate(catalog api.Catalog, machine api.Object) (api.Value, error) {
	spec, err := NetworkSpec(catalog, machine)
	if err != nil {
		return api.Value{}, err
	}
	template := spec.Get("nmstate")
	overrides := machine.Spec().Get("network", "overrides")
	if !overrides.Present() {
		return template, nil
	}
	merged, ok := MergeNative(template, overrides)
	if !ok {
		return api.Value{}, refusal("api.invariant", "the Machine's network overrides do not merge into its network template", "make each list in spec.network.overrides on "+machine.Identity()+" and the list it merges into uniformly named or uniformly unnamed maps")
	}
	return merged, nil
}

// EthernetInterfaces reads the ethernet interfaces of the composed network
// configuration a Machine selects that are neither absent nor ignored, in
// their declared order. It is the one reader of that list, so the interfaces a
// substrate realizes and the interfaces a consumer names are always the same
// set in the same order.
func EthernetInterfaces(catalog api.Catalog, machine api.Object) ([]string, error) {
	template, err := NetworkTemplate(catalog, machine)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, item := range template.Get("interfaces").Items() {
		state := item.Get("state").Text()
		if item.Get("type").Text() != "ethernet" || state == "absent" || state == "ignore" {
			continue
		}
		name := item.Get("name").Text()
		if name == "" {
			return nil, refusal("api.value", "the Machine's network template declares an unnamed interface", "correct the NMState interfaces of "+machine.Identity())
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return nil, refusal("api.value", "the Machine's network template declares no ethernet interface that is not absent or ignored", "declare one on "+machine.Identity())
	}
	return names, nil
}

// DefaultGateway is the authored next hop of the first route that is not
// absent, whose destination is a zero-prefix IPv4 CIDR and whose next hop is an
// IPv4 address; a template without one installs without a default route.
func DefaultGateway(template api.Value) string {
	for _, route := range template.Get("routes", "config").Items() {
		if route.Get("state").Text() == "absent" {
			continue
		}
		destination, err := netip.ParsePrefix(route.Get("destination").Text())
		if err != nil || destination.Bits() != 0 || !destination.Addr().Is4() {
			continue
		}
		hop := route.Get("next-hop-address").Text()
		if next, err := netip.ParseAddr(hop); err == nil && next.Is4() {
			return hop
		}
	}
	return ""
}

// BridgeProviders names, in canonical order, the libvirt providers hosted on
// machine whose managed attachment carries address as its host address. That
// address exists only once the provider's host block creates the bridge, so a
// socket bound to it cannot open before then.
func BridgeProviders(catalog api.Catalog, machine, address string) []string {
	bound, err := netip.ParseAddr(address)
	if err != nil {
		return nil
	}
	bound = bound.WithZone("").Unmap()
	var found []string
	for _, provider := range ProvidersOn(catalog, ArmLibvirt) {
		if provider.Spec().Get("libvirt", "machineRef").Text() != machine {
			continue
		}
		for _, attachment := range provider.Spec().Get("networkAttachments").Items() {
			arm := attachment.Get("libvirt")
			prefix, err := netip.ParsePrefix(arm.Get("address").Text())
			if err == nil && arm.Get("management").Text() == "managed" && prefix.Addr().Unmap() == bound {
				found = append(found, provider.Name())
				break
			}
		}
	}
	return found
}

// refusal is the one shape every pure substrate derivation refuses in, so a
// caller reads the same diagnostic whichever derivation produced it.
func refusal(code, message, remediation string) error {
	return diagnostics.NewFailureWithRemediation(code, message, "", remediation)
}
