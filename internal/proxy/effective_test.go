package proxy

import (
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func envWithExternalProxy() *v1alpha1.Environment {
	return &v1alpha1.Environment{
		Metadata: v1alpha1.Metadata{Name: "env"},
		Spec: v1alpha1.EnvironmentSpec{
			BaseDomain: "example.test",
			ProxyFor: v1alpha1.EnvironmentProxyForSpec{
				Bootwright:     "default",
				ClusterInstall: "default",
			},
			InfraComponents: v1alpha1.EnvironmentInfraComponentsSpec{
				Proxies: []v1alpha1.EnvironmentProxyComponent{{
					Name: "default",
					Type: v1alpha1.EnvironmentComponentExternal,
					Spec: &v1alpha1.EnvironmentProxySpec{
						HTTPProxy:  "http://external-proxy:3128",
						HTTPSProxy: "https://external-proxy:3128",
						NoProxy:    []string{"corp.internal", "localhost"},
						Auth: &v1alpha1.EnvironmentProxyAuthSpec{
							ProxyAuthRef: v1alpha1.SecretRef{Name: "proxy-auth"},
						},
					},
				}},
			},
		},
	}
}

func stateWithManagedProxy() v1alpha1.State {
	env := v1alpha1.Environment{
		Metadata: v1alpha1.Metadata{Name: "env"},
		Spec: v1alpha1.EnvironmentSpec{
			BaseDomain: "example.test",
			ProxyFor:   v1alpha1.EnvironmentProxyForSpec{Bootwright: "managed", ClusterInstall: "managed"},
			InfraComponents: v1alpha1.EnvironmentInfraComponentsSpec{
				Proxies: []v1alpha1.EnvironmentProxyComponent{{
					Name:         "managed",
					Type:         v1alpha1.EnvironmentComponentManaged,
					ComponentRef: v1alpha1.LocalObjectReference{Name: "proxy"},
				}},
			},
		},
	}
	return v1alpha1.State{
		Environments: []v1alpha1.Environment{env},
		Hosts: []v1alpha1.Host{{
			Metadata: v1alpha1.Metadata{Name: "lab-host"},
			Spec: v1alpha1.HostSpec{
				Addresses:    []v1alpha1.HostAddress{{Name: "ssh", Address: "10.0.0.5"}},
				SSH:          &v1alpha1.HostSSHSpec{AddressName: "ssh"},
				Capabilities: []string{"container-runtime"},
			},
		}},
		InfraComponents: []v1alpha1.InfraComponent{{
			Metadata: v1alpha1.Metadata{Name: "proxy"},
			Spec: v1alpha1.InfraComponentSpec{
				Proxy: &v1alpha1.ProxyComponent{
					Type:    v1alpha1.InfraComponentTypeSquid,
					HostRef: v1alpha1.LocalObjectReference{Name: "lab-host"},
					Port:    3128,
				},
			},
		}},
		ClusterInfras: []v1alpha1.ClusterInfra{{Metadata: v1alpha1.Metadata{Name: "c1"}}},
	}
}

func TestIsManaged(t *testing.T) {
	if IsManaged(v1alpha1.State{}) {
		t.Fatal("IsManaged(empty) = true")
	}
	if !IsManaged(stateWithManagedProxy()) {
		t.Fatal("IsManaged(managed proxy catalog) = false")
	}
}

func TestResolveNilEnv(t *testing.T) {
	if got := Resolve(v1alpha1.State{}, nil); got != nil {
		t.Fatalf("Resolve(nil env) = %+v, want nil", got)
	}
}

func TestResolveReturnsNilForNone(t *testing.T) {
	env := &v1alpha1.Environment{Spec: v1alpha1.EnvironmentSpec{ProxyFor: v1alpha1.EnvironmentProxyForSpec{Bootwright: v1alpha1.EnvironmentComponentNone}}}
	if got := Resolve(v1alpha1.State{}, env); got != nil {
		t.Fatalf("Resolve = %+v, want nil", got)
	}
}

func TestResolveExternalCopiesEnvProxy(t *testing.T) {
	env := envWithExternalProxy()
	got := Resolve(v1alpha1.State{}, env)
	if got == nil {
		t.Fatal("Resolve returned nil")
	}
	spec := env.Spec.InfraComponents.Proxies[0].Spec
	if got.HTTP != spec.HTTPProxy {
		t.Errorf("HTTP = %q, want %q", got.HTTP, spec.HTTPProxy)
	}
	if got.HTTPS != spec.HTTPSProxy {
		t.Errorf("HTTPS = %q, want %q", got.HTTPS, spec.HTTPSProxy)
	}
	if got.Auth.Name != "proxy-auth" {
		t.Errorf("Auth.Name = %q, want %q", got.Auth.Name, "proxy-auth")
	}
}

func TestResolveNoProxyMergesAndDedupes(t *testing.T) {
	env := envWithExternalProxy()
	state := v1alpha1.State{
		NetworkConfigs: []v1alpha1.NetworkConfig{{
			Metadata: v1alpha1.Metadata{Name: "net"},
			Spec: v1alpha1.NetworkConfigSpec{
				MachineNetwork: []v1alpha1.MachineNetworkCIDR{{CIDR: "10.10.0.0/24"}},
				Template:       v1alpha1.NetworkConfigTemplate{NetworkConfig: map[string]any{}},
			},
		}},
		ClusterInfras: []v1alpha1.ClusterInfra{{
			Metadata: v1alpha1.Metadata{Name: "c1"},
			Spec: v1alpha1.ClusterInfraSpec{
				Endpoints: map[string]v1alpha1.Endpoint{"api": {ExternalVIP: "10.10.0.10"}},
			},
		}},
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "c1"},
			Spec: v1alpha1.ContainerClusterSpec{
				Networking: &v1alpha1.OCPNetworkingSpec{
					ClusterNetwork: []v1alpha1.ContainerClusterNetworkCIDR{{CIDR: "10.128.0.0/14", HostPrefix: 23}},
					ServiceNetwork: []string{"172.30.0.0/16"},
				},
			},
		}},
		Hosts: []v1alpha1.Host{{
			Metadata: v1alpha1.Metadata{Name: "h1"},
			Spec: v1alpha1.HostSpec{
				Addresses: []v1alpha1.HostAddress{{Name: "ssh", Address: "10.0.0.5"}},
				SSH:       &v1alpha1.HostSSHSpec{AddressName: "ssh"},
			},
		}},
	}
	got := Resolve(state, env)
	if got == nil {
		t.Fatal("Resolve returned nil")
	}
	if len(got.NoProxy) < 2 || got.NoProxy[0] != "corp.internal" || got.NoProxy[1] != "localhost" {
		t.Errorf("user entries should lead NoProxy, got %v", got.NoProxy)
	}
	count := 0
	for _, e := range got.NoProxy {
		if e == "localhost" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("localhost should appear exactly once in NoProxy, got %d times: %v", count, got.NoProxy)
	}
	for _, want := range []string{"10.10.0.0/24", "10.10.0.10", "10.128.0.0/14", "172.30.0.0/16", ".c1.example.test", "10.0.0.5"} {
		if !slices.Contains(got.NoProxy, want) {
			t.Errorf("NoProxy missing %q; got %v", want, got.NoProxy)
		}
	}
}

func TestManagedProxyURL(t *testing.T) {
	state := stateWithManagedProxy()
	got, err := ManagedProxyURL(state, state.ClusterInfras[0])
	if err != nil {
		t.Fatalf("ManagedProxyURL: %v", err)
	}
	if got != "http://10.0.0.5:3128" {
		t.Errorf("ManagedProxyURL = %q", got)
	}
}

func TestManagedProxyURLDefaultsPort(t *testing.T) {
	state := stateWithManagedProxy()
	state.InfraComponents[0].Spec.Proxy.Port = 0
	got, err := ManagedProxyURL(state, state.ClusterInfras[0])
	if err != nil {
		t.Fatalf("ManagedProxyURL: %v", err)
	}
	if got != "http://10.0.0.5:3128" {
		t.Errorf("default port: got %q", got)
	}
}

func TestManagedProxyURLErrors(t *testing.T) {
	state := stateWithManagedProxy()
	state.Hosts[0].Spec.SSH = &v1alpha1.HostSSHSpec{}
	_, err := ManagedProxyURL(state, state.ClusterInfras[0])
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !strings.Contains(err.Error(), "has no routable address") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestMirrorHost(t *testing.T) {
	cases := map[string]string{
		"registry.local:5000":              "registry.local",
		"registry.local":                   "registry.local",
		"https://registry.local:5000/path": "registry.local",
		"[fd00::1]:5000":                   "fd00::1",
		"fd00::1":                          "fd00::1",
		"https://[fd00::1]:5000":           "fd00::1",
		"":                                 "",
	}
	for in, want := range cases {
		if got := MirrorHost(in); got != want {
			t.Errorf("MirrorHost(%q) = %q, want %q", in, got, want)
		}
	}
}
