package desiredstate

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

type addressFamilyFixture struct {
	machineCIDR     string
	endpointAddress string
	clusterCIDR     string
	serviceCIDR     string
}

func (f addressFamilyFixture) build() (v1alpha1.State, v1alpha1.ContainerCluster, map[string]v1alpha1.NetworkConfig) {
	network := v1alpha1.NetworkConfig{
		Metadata: v1alpha1.Metadata{Name: "cluster-net"},
		Spec: v1alpha1.NetworkConfigSpec{
			MachineNetwork: []v1alpha1.MachineNetworkCIDR{{CIDR: f.machineCIDR}},
		},
	}
	machine := v1alpha1.Machine{
		Metadata: v1alpha1.Metadata{Name: "srv1"},
		Spec: v1alpha1.MachineSpec{
			Network: v1alpha1.MachineNetwork{
				Config: v1alpha1.MachineNetworkConfig{
					NetworkConfigRef: v1alpha1.LocalObjectReference{Name: "cluster-net"},
				},
			},
		},
	}
	endpoint := v1alpha1.Endpoint{
		Address: f.endpointAddress,
		Source:  v1alpha1.EndpointSource{Type: v1alpha1.EndpointSourceExternal},
	}
	ocp := v1alpha1.ContainerCluster{
		Metadata: v1alpha1.Metadata{Name: "hub"},
		Spec: v1alpha1.ContainerClusterSpec{
			Install: v1alpha1.OCPInstallSpec{
				Endpoints: map[string]v1alpha1.Endpoint{
					v1alpha1.EndpointAPI:     endpoint,
					v1alpha1.EndpointAPIInt:  endpoint,
					v1alpha1.EndpointIngress: endpoint,
				},
			},
			Networking: &v1alpha1.OCPNetworkingSpec{
				ClusterNetwork: []v1alpha1.ContainerClusterNetworkCIDR{{CIDR: f.clusterCIDR, HostPrefix: 23}},
				ServiceNetwork: []string{f.serviceCIDR},
			},
			Nodes: []v1alpha1.OCPNodeSpec{{
				Name:       "master-0",
				Role:       v1alpha1.NodeRoleMaster,
				MachineRef: v1alpha1.LocalObjectReference{Name: "srv1"},
			}},
		},
	}
	state := v1alpha1.State{
		Machines:          []v1alpha1.Machine{machine},
		NetworkConfigs:    []v1alpha1.NetworkConfig{network},
		ContainerClusters: []v1alpha1.ContainerCluster{ocp},
	}
	return state, ocp, map[string]v1alpha1.NetworkConfig{"cluster-net": network}
}

func (f addressFamilyFixture) validate() []string {
	state, ocp, networkConfigs := f.build()
	return validateClusterSingleAddressFamily(state, ocp, networkConfigs)
}

func singleStackIPv4() addressFamilyFixture {
	return addressFamilyFixture{
		machineCIDR:     "192.168.132.0/24",
		endpointAddress: "192.168.132.10",
		clusterCIDR:     "10.128.0.0/14",
		serviceCIDR:     "172.30.0.0/16",
	}
}

func singleStackIPv6() addressFamilyFixture {
	return addressFamilyFixture{
		machineCIDR:     "fd00:132::/64",
		endpointAddress: "fd00:132::10",
		clusterCIDR:     "fd01::/48",
		serviceCIDR:     "fd02::/112",
	}
}

func TestSingleStackClustersStayLegalInBothFamilies(t *testing.T) {
	if errs := singleStackIPv4().validate(); len(errs) != 0 {
		t.Fatalf("an all-IPv4 cluster is single-stack and must pass, got: %v", errs)
	}
	if errs := singleStackIPv6().validate(); len(errs) != 0 {
		t.Fatalf("an all-IPv6 cluster is single-stack and must pass; this rule is about mixing families, not about requiring IPv4, got: %v", errs)
	}
}

func TestMixedAddressFamiliesFailClosed(t *testing.T) {
	for _, tc := range []struct {
		name    string
		fixture addressFamilyFixture
		fields  []string
	}{
		{
			name: "service network of the other family",
			fixture: func() addressFamilyFixture {
				f := singleStackIPv4()
				f.serviceCIDR = "fd02::/112"
				return f
			}(),
			fields: []string{"NetworkConfig/cluster-net spec.machineNetwork[0].cidr 192.168.132.0/24 is IPv4", "spec.networking.serviceNetwork[0] fd02::/112 is IPv6"},
		},
		{
			name: "cluster network of the other family",
			fixture: func() addressFamilyFixture {
				f := singleStackIPv6()
				f.clusterCIDR = "10.128.0.0/14"
				return f
			}(),
			fields: []string{"NetworkConfig/cluster-net spec.machineNetwork[0].cidr fd00:132::/64 is IPv6", "spec.networking.clusterNetwork[0].cidr 10.128.0.0/14 is IPv4"},
		},
		{
			name: "endpoint address of the other family",
			fixture: func() addressFamilyFixture {
				f := singleStackIPv4()
				f.endpointAddress = "fd00:132::10"
				return f
			}(),
			fields: []string{"NetworkConfig/cluster-net spec.machineNetwork[0].cidr 192.168.132.0/24 is IPv4", "spec.install.endpoints.api.address fd00:132::10 is IPv6"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			errs := tc.fixture.validate()
			if len(errs) != 1 {
				t.Fatalf("expected exactly one refusal, got: %v", errs)
			}
			if !strings.Contains(errs[0], "ContainerCluster/hub mixes IP address families") {
				t.Fatalf("the refusal must name the cluster, got: %q", errs[0])
			}
			for _, field := range tc.fields {
				if !strings.Contains(errs[0], field) {
					t.Fatalf("the refusal must name %q with its family, got: %q", field, errs[0])
				}
			}
			if !strings.Contains(errs[0], "single-stack is the current v1alpha1 scope") {
				t.Fatalf("the refusal must state the declared scope, got: %q", errs[0])
			}
		})
	}
}

func TestMixedAddressFamilyRefusalReachesLoadNormalizeValidate(t *testing.T) {
	dir := t.TempDir()
	files := newBaselineFiles()
	files["cluster.yaml"] = strings.Replace(files["cluster.yaml"],
		"serviceNetwork: [172.30.0.0/16]", "serviceNetwork: [fd02::/112]", 1)
	writeFiles(t, dir, files)
	_, err := LoadNormalizeValidate([]string{dir})
	if err == nil {
		t.Fatal("a cluster whose serviceNetwork is the other family must fail validation")
	}
	if !strings.Contains(err.Error(), "mixes IP address families") {
		t.Fatalf("expected the single-stack refusal, got: %v", err)
	}
}
