package proxy

import (
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func envWithProxy() *v1alpha1.Environment {
	return &v1alpha1.Environment{
		Metadata: v1alpha1.Metadata{Name: "env"},
		Spec: v1alpha1.EnvironmentSpec{
			BaseDomain: "example.test",
			Proxy: &v1alpha1.EnvironmentProxySpec{
				HTTPProxy:  "http://external-proxy:3128",
				HTTPSProxy: "https://external-proxy:3128",
				NoProxy:    []string{"corp.internal", "localhost"},
				Auth: &v1alpha1.EnvironmentProxyAuthSpec{
					ProxyAuthRef: v1alpha1.SecretRef{Name: "proxy-auth"},
				},
			},
		},
	}
}

func stateWithManagedProxy() v1alpha1.State {
	return v1alpha1.State{
		Hosts: []v1alpha1.Host{{
			Metadata: v1alpha1.Metadata{Name: "lab-host"},
			Spec: v1alpha1.HostSpec{
				Addresses:    []v1alpha1.HostAddress{{Name: "ssh", Address: "10.0.0.5"}},
				SSH:          &v1alpha1.HostSSHSpec{AddressName: "ssh"},
				Capabilities: []string{"container-runtime"},
			},
		}},
		InfraProviders: []v1alpha1.InfraProvider{{
			Metadata: v1alpha1.Metadata{Name: "lab"},
			Spec: v1alpha1.InfraProviderSpec{
				Proxies: []v1alpha1.ProxyCapability{{
					Name:  "default",
					Squid: &v1alpha1.SquidCapability{HostRef: v1alpha1.LocalObjectReference{Name: "lab-host"}},
				}},
			},
		}},
		ClusterInfras: []v1alpha1.ClusterInfra{{
			Metadata: v1alpha1.Metadata{Name: "c1"},
			Spec: v1alpha1.ClusterInfraSpec{
				Components: v1alpha1.ClusterComponents{
					Proxy: &v1alpha1.ClusterComponentRef{
						From: v1alpha1.From{Provider: "lab", Name: "default"},
						Port: 3128,
					},
				},
			},
		}},
	}
}

func TestIsManaged(t *testing.T) {
	cases := []struct {
		name string
		ci   []v1alpha1.ClusterInfra
		want bool
	}{
		{"empty state", nil, false},
		{
			"cluster without proxy component",
			[]v1alpha1.ClusterInfra{{Spec: v1alpha1.ClusterInfraSpec{}}},
			false,
		},
		{
			"cluster with proxy component",
			[]v1alpha1.ClusterInfra{{Spec: v1alpha1.ClusterInfraSpec{
				Components: v1alpha1.ClusterComponents{
					Proxy: &v1alpha1.ClusterComponentRef{From: v1alpha1.From{Provider: "p", Name: "n"}},
				},
			}}},
			true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsManaged(v1alpha1.State{ClusterInfras: tc.ci})
			if got != tc.want {
				t.Fatalf("IsManaged = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestResolveNilEnv(t *testing.T) {
	if got := Resolve(v1alpha1.State{}, nil); got != nil {
		t.Fatalf("Resolve(nil env) = %+v, want nil", got)
	}
}

func TestResolveReturnsNilWhenNoProxyAndNoManaged(t *testing.T) {
	env := &v1alpha1.Environment{Spec: v1alpha1.EnvironmentSpec{BaseDomain: "example.test"}}
	if got := Resolve(v1alpha1.State{}, env); got != nil {
		t.Fatalf("Resolve = %+v, want nil when env has no proxy and no managed proxy", got)
	}
}

func TestResolveExternalCopiesEnvProxy(t *testing.T) {
	env := envWithProxy()
	got := Resolve(v1alpha1.State{}, env)
	if got == nil {
		t.Fatal("Resolve returned nil")
	}
	if got.HTTP != env.Spec.Proxy.HTTPProxy {
		t.Errorf("HTTP = %q, want %q", got.HTTP, env.Spec.Proxy.HTTPProxy)
	}
	if got.HTTPS != env.Spec.Proxy.HTTPSProxy {
		t.Errorf("HTTPS = %q, want %q", got.HTTPS, env.Spec.Proxy.HTTPSProxy)
	}
	if got.Auth.Name != "proxy-auth" {
		t.Errorf("Auth.Name = %q, want %q", got.Auth.Name, "proxy-auth")
	}
}

func TestResolveManagedModeKeepsAuthAndAutoNoProxy(t *testing.T) {
	state := stateWithManagedProxy()
	env := &v1alpha1.Environment{Spec: v1alpha1.EnvironmentSpec{
		BaseDomain: "example.test",
		Proxy: &v1alpha1.EnvironmentProxySpec{
			Auth: &v1alpha1.EnvironmentProxyAuthSpec{
				ProxyAuthRef: v1alpha1.SecretRef{Name: "managed-auth"},
			},
		},
	}}
	got := Resolve(state, env)
	if got == nil {
		t.Fatal("Resolve returned nil for managed-mode env+state")
	}
	if got.HTTP != "" || got.HTTPS != "" {
		t.Errorf("managed mode should not set HTTP/HTTPS on Effective; got HTTP=%q HTTPS=%q", got.HTTP, got.HTTPS)
	}
	if got.Auth.Name != "managed-auth" {
		t.Errorf("Auth.Name = %q, want %q", got.Auth.Name, "managed-auth")
	}
	// Auto noProxy must at least contain the canonical baseline.
	for _, want := range []string{"localhost", "127.0.0.1", "::1", ".svc", ".cluster.local", ".example.test"} {
		if !slices.Contains(got.NoProxy, want) {
			t.Errorf("NoProxy missing %q; got %v", want, got.NoProxy)
		}
	}
}

func TestResolveNoProxyMergesAndDedupes(t *testing.T) {
	env := envWithProxy()
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
				Endpoints: map[string]v1alpha1.Endpoint{
					"api": {ExternalVIP: "10.10.0.10"},
				},
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
	// User entries are emitted first (and "localhost" appears once even
	// though both user and auto include it).
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
	// Auto-extended entries.
	for _, want := range []string{
		"10.10.0.0/24", "10.10.0.10", "10.128.0.0/14", "172.30.0.0/16",
		".c1.example.test", "10.0.0.5",
	} {
		if !slices.Contains(got.NoProxy, want) {
			t.Errorf("NoProxy missing %q; got %v", want, got.NoProxy)
		}
	}
}

func TestManagedProxyURL(t *testing.T) {
	state := stateWithManagedProxy()
	ci := state.ClusterInfras[0]
	got, err := ManagedProxyURL(state, ci)
	if err != nil {
		t.Fatalf("ManagedProxyURL: %v", err)
	}
	want := "http://10.0.0.5:3128"
	if got != want {
		t.Errorf("ManagedProxyURL = %q, want %q", got, want)
	}
}

func TestManagedProxyURLDefaultsPort(t *testing.T) {
	state := stateWithManagedProxy()
	state.ClusterInfras[0].Spec.Components.Proxy.Port = 0
	got, err := ManagedProxyURL(state, state.ClusterInfras[0])
	if err != nil {
		t.Fatalf("ManagedProxyURL: %v", err)
	}
	want := "http://10.0.0.5:3128"
	if got != want {
		t.Errorf("default port: got %q, want %q", got, want)
	}
}

func TestManagedProxyURLNilWhenNoComponent(t *testing.T) {
	ci := v1alpha1.ClusterInfra{Metadata: v1alpha1.Metadata{Name: "c1"}}
	got, err := ManagedProxyURL(v1alpha1.State{}, ci)
	if err != nil {
		t.Fatalf("ManagedProxyURL: %v", err)
	}
	if got != "" {
		t.Errorf("want empty URL when no proxy component, got %q", got)
	}
}

func TestManagedProxyURLErrors(t *testing.T) {
	cases := []struct {
		name        string
		mutate      func(*v1alpha1.State)
		wantErrPart string
	}{
		{
			name:        "missing provider",
			mutate:      func(s *v1alpha1.State) { s.InfraProviders = nil },
			wantErrPart: "not found",
		},
		{
			name: "wrong capability name",
			mutate: func(s *v1alpha1.State) {
				s.ClusterInfras[0].Spec.Components.Proxy.From.Name = "other"
			},
			wantErrPart: "is not a squid capability",
		},
		{
			name: "hostRef has no cluster-facing address",
			mutate: func(s *v1alpha1.State) {
				s.Hosts[0].Spec.SSH = &v1alpha1.HostSSHSpec{}
			},
			wantErrPart: "has no routable address",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := stateWithManagedProxy()
			tc.mutate(&state)
			_, err := ManagedProxyURL(state, state.ClusterInfras[0])
			if err == nil {
				t.Fatalf("want error containing %q, got nil", tc.wantErrPart)
			}
			if !strings.Contains(err.Error(), tc.wantErrPart) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tc.wantErrPart)
			}
		})
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
