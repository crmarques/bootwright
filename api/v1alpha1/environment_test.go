package v1alpha1

import "testing"

func specWithProxies(proxyFor EnvironmentProxyForSpec, proxies ...EnvironmentProxyComponent) EnvironmentSpec {
	return EnvironmentSpec{
		ProxyFor:        proxyFor,
		InfraComponents: EnvironmentInfraComponentsSpec{Proxies: proxies},
	}
}

func TestDefaultProxyName(t *testing.T) {
	if got := (EnvironmentSpec{}).DefaultProxyName(); got != "" {
		t.Errorf("no proxies: DefaultProxyName() = %q, want \"\"", got)
	}
	spec := specWithProxies(EnvironmentProxyForSpec{},
		EnvironmentProxyComponent{Name: "corp", Default: true},
		EnvironmentProxyComponent{Name: "lab"},
	)
	if got := spec.DefaultProxyName(); got != "corp" {
		t.Errorf("DefaultProxyName() = %q, want %q", got, "corp")
	}
	// A single un-defaulted proxy is NOT implicitly the default (opt-in via
	// default: true, unlike registries).
	none := specWithProxies(EnvironmentProxyForSpec{}, EnvironmentProxyComponent{Name: "only"})
	if got := none.DefaultProxyName(); got != "" {
		t.Errorf("un-defaulted single proxy: DefaultProxyName() = %q, want \"\"", got)
	}
}

func TestProxyNameFor(t *testing.T) {
	spec := specWithProxies(
		EnvironmentProxyForSpec{
			ContainerClusterInstall: "lab",  // explicit override
			MachineOSInstall:        "none", // opt out
			// Bootwright omitted -> inherit the default
		},
		EnvironmentProxyComponent{Name: "corp", Default: true},
		EnvironmentProxyComponent{Name: "lab"},
	)
	cases := map[string]string{
		ProxyConsumerBootwright:              "corp", // inherits default
		ProxyConsumerContainerClusterInstall: "lab",  // explicit override
		ProxyConsumerMachineOSInstall:        "",     // "none" opt-out
		"unknown-consumer":                   "",
	}
	for consumer, want := range cases {
		if got := spec.ProxyNameFor(consumer); got != want {
			t.Errorf("ProxyNameFor(%q) = %q, want %q", consumer, got, want)
		}
	}

	// With no default proxy, an omitted slot resolves to "" (no proxy), matching
	// the pre-default behaviour.
	noDefault := specWithProxies(EnvironmentProxyForSpec{}, EnvironmentProxyComponent{Name: "corp"})
	if got := noDefault.ProxyNameFor(ProxyConsumerBootwright); got != "" {
		t.Errorf("omitted slot without a default: ProxyNameFor(bootwright) = %q, want \"\"", got)
	}
}
