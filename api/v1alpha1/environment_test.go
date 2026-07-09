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
	none := specWithProxies(EnvironmentProxyForSpec{}, EnvironmentProxyComponent{Name: "only"})
	if got := none.DefaultProxyName(); got != "" {
		t.Errorf("un-defaulted single proxy: DefaultProxyName() = %q, want \"\"", got)
	}
}

func TestProxyNameFor(t *testing.T) {
	spec := specWithProxies(
		EnvironmentProxyForSpec{
			ContainerClusterInstall: "lab",
			MachineOSInstall:        "none",
		},
		EnvironmentProxyComponent{Name: "corp", Default: true},
		EnvironmentProxyComponent{Name: "lab"},
	)
	cases := map[string]string{
		ProxyConsumerBootwright:              "corp",
		ProxyConsumerContainerClusterInstall: "lab",
		ProxyConsumerMachineOSInstall:        "",
		"unknown-consumer":                   "",
	}
	for consumer, want := range cases {
		if got := spec.ProxyNameFor(consumer); got != want {
			t.Errorf("ProxyNameFor(%q) = %q, want %q", consumer, got, want)
		}
	}

	noDefault := specWithProxies(EnvironmentProxyForSpec{}, EnvironmentProxyComponent{Name: "corp"})
	if got := noDefault.ProxyNameFor(ProxyConsumerBootwright); got != "" {
		t.Errorf("omitted slot without a default: ProxyNameFor(bootwright) = %q, want \"\"", got)
	}
}
