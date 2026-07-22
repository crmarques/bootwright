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
	sole := specWithProxies(EnvironmentProxyForSpec{}, EnvironmentProxyComponent{Name: "only"})
	if got := sole.DefaultProxyName(); got != "only" {
		t.Errorf("sole proxy defaults: DefaultProxyName() = %q, want %q", got, "only")
	}
	ambiguous := specWithProxies(EnvironmentProxyForSpec{},
		EnvironmentProxyComponent{Name: "corp"},
		EnvironmentProxyComponent{Name: "lab"},
	)
	if got := ambiguous.DefaultProxyName(); got != "" {
		t.Errorf("multiple un-defaulted proxies: DefaultProxyName() = %q, want \"\"", got)
	}
}

func TestDefaultArtifactServerName(t *testing.T) {
	if got := (EnvironmentSpec{}).DefaultArtifactServerName(); got != "" {
		t.Errorf("no artifact servers: DefaultArtifactServerName() = %q, want \"\"", got)
	}
	marked := EnvironmentSpec{InfraComponents: EnvironmentInfraComponentsSpec{ArtifactServers: []EnvironmentArtifactServerComponent{
		{Name: "primary", Default: true},
		{Name: "mirror"},
	}}}
	if got := marked.DefaultArtifactServerName(); got != "primary" {
		t.Errorf("DefaultArtifactServerName() = %q, want %q", got, "primary")
	}
	sole := EnvironmentSpec{InfraComponents: EnvironmentInfraComponentsSpec{ArtifactServers: []EnvironmentArtifactServerComponent{{Name: "only"}}}}
	if got := sole.DefaultArtifactServerName(); got != "only" {
		t.Errorf("sole artifact server defaults: DefaultArtifactServerName() = %q, want %q", got, "only")
	}
	ambiguous := EnvironmentSpec{InfraComponents: EnvironmentInfraComponentsSpec{ArtifactServers: []EnvironmentArtifactServerComponent{
		{Name: "primary"},
		{Name: "mirror"},
	}}}
	if got := ambiguous.DefaultArtifactServerName(); got != "" {
		t.Errorf("multiple un-defaulted artifact servers: DefaultArtifactServerName() = %q, want \"\"", got)
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

	sole := specWithProxies(EnvironmentProxyForSpec{}, EnvironmentProxyComponent{Name: "corp"})
	if got := sole.ProxyNameFor(ProxyConsumerBootwright); got != "corp" {
		t.Errorf("omitted slot inherits the sole proxy: ProxyNameFor(bootwright) = %q, want %q", got, "corp")
	}
}
