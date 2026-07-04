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
				Bootwright:              "default",
				ContainerClusterInstall: "default",
			},
			InfraComponents: v1alpha1.EnvironmentInfraComponentsSpec{
				Proxies: []v1alpha1.EnvironmentProxyComponent{{
					Name:       "default",
					Management: v1alpha1.EnvironmentComponentExternal,
					Connection: &v1alpha1.EnvironmentProxyConnection{
						HTTPProxy:  "http://external-proxy:3128",
						HTTPSProxy: "https://external-proxy:3128",
						NoProxy:    []string{"corp.internal", "localhost"},
						Auth: &v1alpha1.EnvironmentProxyAuthSpec{
							ProxyAuthRef: v1alpha1.SecretRef{Name: "proxy-auth"},
						},
						TrustBundleRef: v1alpha1.SecretRef{Name: "proxy-ca"},
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
			ProxyFor:   v1alpha1.EnvironmentProxyForSpec{Bootwright: "managed", ContainerClusterInstall: "managed"},
			InfraComponents: v1alpha1.EnvironmentInfraComponentsSpec{
				Proxies: []v1alpha1.EnvironmentProxyComponent{{
					Name:         "managed",
					Management:   v1alpha1.EnvironmentComponentManaged,
					ComponentRef: v1alpha1.LocalObjectReference{Name: "proxy"},
				}},
			},
		},
	}
	return v1alpha1.State{
		Environments: []v1alpha1.Environment{env},
		Machines: []v1alpha1.Machine{{
			Metadata: v1alpha1.Metadata{Name: "bastion"},
			Spec: v1alpha1.MachineSpec{
				OS: v1alpha1.MachineOSSpec{
					Provided: v1alpha1.BoolPtr(true),
				},
				Addresses: []v1alpha1.MachineAddress{{Name: "ssh", Address: "10.0.0.5"}},
				Access: v1alpha1.MachineAccess{
					SSH: &v1alpha1.MachineSSHSpec{AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"}},
				},
			},
		}},
		InfraComponents: []v1alpha1.InfraComponent{{
			Metadata: v1alpha1.Metadata{Name: "proxy"},
			Spec: v1alpha1.InfraComponentSpec{
				Type: v1alpha1.ComponentSlotProxy,
				Proxy: &v1alpha1.ProxyComponent{
					Implementation: v1alpha1.InfraComponentTypeSquid,
					MachineRef:     v1alpha1.LocalObjectReference{Name: "bastion"},
					Port:           3128,
				},
			},
		}},
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
	connection := env.Spec.InfraComponents.Proxies[0].Connection
	if got.HTTP != connection.HTTPProxy {
		t.Errorf("HTTP = %q, want %q", got.HTTP, connection.HTTPProxy)
	}
	if got.HTTPS != connection.HTTPSProxy {
		t.Errorf("HTTPS = %q, want %q", got.HTTPS, connection.HTTPSProxy)
	}
	if got.Auth.Name != "proxy-auth" {
		t.Errorf("Auth.Name = %q, want %q", got.Auth.Name, "proxy-auth")
	}
	if got.TrustBundle.Name != "proxy-ca" {
		t.Errorf("TrustBundle.Name = %q, want %q", got.TrustBundle.Name, "proxy-ca")
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
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "c1"},
			Spec: v1alpha1.ContainerClusterSpec{
				Install: v1alpha1.OCPInstallSpec{
					Endpoints: map[string]v1alpha1.Endpoint{"api": {Address: "10.10.0.10"}},
				},
				Networking: &v1alpha1.OCPNetworkingSpec{
					ClusterNetwork: []v1alpha1.ContainerClusterNetworkCIDR{{CIDR: "10.128.0.0/14", HostPrefix: 23}},
					ServiceNetwork: []string{"172.30.0.0/16"},
				},
			},
		}},
		Machines: []v1alpha1.Machine{{
			Metadata: v1alpha1.Metadata{Name: "h1"},
			Spec: v1alpha1.MachineSpec{
				OS: v1alpha1.MachineOSSpec{
					Provided: v1alpha1.BoolPtr(true),
				},
				Addresses: []v1alpha1.MachineAddress{{Name: "ssh", Address: "10.0.0.5"}},
				Access: v1alpha1.MachineAccess{
					SSH: &v1alpha1.MachineSSHSpec{AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"}},
				},
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

func TestResolveNoProxyExpandsCIDREntriesForKnownBMCIPs(t *testing.T) {
	env := envWithExternalProxy()
	env.Spec.InfraComponents.Proxies[0].Connection.NoProxy = []string{"10.0.0.0/8"}
	state := stateWithBareMetalBMC("redfish-virtualmedia+https://10.1.255.61/redfish/v1/Systems/1")

	got := Resolve(state, env)
	if got == nil {
		t.Fatal("Resolve returned nil")
	}
	for _, want := range []string{"10.0.0.0/8", "10.1.255.61"} {
		if !slices.Contains(got.NoProxy, want) {
			t.Fatalf("NoProxy missing %q; got %v", want, got.NoProxy)
		}
	}
}

func TestResolveNoProxyDoesNotBypassBMCWithoutMatchingCIDR(t *testing.T) {
	env := envWithExternalProxy()
	state := stateWithBareMetalBMC("redfish-virtualmedia+https://10.1.255.61/redfish/v1/Systems/1")

	got := Resolve(state, env)
	if got == nil {
		t.Fatal("Resolve returned nil")
	}
	if slices.Contains(got.NoProxy, "10.1.255.61") {
		t.Fatalf("NoProxy should not include unmatched BMC IP: %v", got.NoProxy)
	}
}

func stateWithBareMetalBMC(address string) v1alpha1.State {
	return v1alpha1.State{
		Machines: []v1alpha1.Machine{{
			Metadata: v1alpha1.Metadata{Name: "master-0"},
			Spec: v1alpha1.MachineSpec{
				Hardware: v1alpha1.MachineHardware{
					Management: v1alpha1.MachineHardwareManagement{
						BMC: v1alpha1.BMCSpec{Address: address},
					},
				},
			},
		}},
	}
}

func TestManagedProxyURL(t *testing.T) {
	state := stateWithManagedProxy()
	got, err := ManagedProxyURL(state, v1alpha1.ClusterInstall{})
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
	got, err := ManagedProxyURL(state, v1alpha1.ClusterInstall{})
	if err != nil {
		t.Fatalf("ManagedProxyURL: %v", err)
	}
	if got != "http://10.0.0.5:3128" {
		t.Errorf("default port: got %q", got)
	}
}

func TestManagedProxyURLErrors(t *testing.T) {
	state := stateWithManagedProxy()
	state.Machines[0].Spec.Access.SSH = &v1alpha1.MachineSSHSpec{}
	_, err := ManagedProxyURL(state, v1alpha1.ClusterInstall{})
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
