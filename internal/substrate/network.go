package substrate

import (
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

// NetworkTemplate is the native NMState map of that configuration.
func NetworkTemplate(catalog api.Catalog, machine api.Object) (api.Value, error) {
	spec, err := NetworkSpec(catalog, machine)
	if err != nil {
		return api.Value{}, err
	}
	return spec.Get("nmstate"), nil
}

// DefaultGateway reads the next hop of the default route the template declares,
// which is the gateway a static installation configures. A template without one
// installs without a default route rather than inventing one.
func DefaultGateway(template api.Value) string {
	for _, route := range template.Get("routes", "config").Items() {
		if route.Get("destination").Text() == "0.0.0.0/0" {
			return route.Get("next-hop-address").Text()
		}
	}
	return ""
}

// refusal is the one shape every pure substrate derivation refuses in, so a
// caller reads the same diagnostic whichever derivation produced it.
func refusal(code, message, remediation string) error {
	return diagnostics.NewFailureWithRemediation(code, message, "", remediation)
}
