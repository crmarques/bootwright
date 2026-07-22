package desiredstate

import (
	"net"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestValidateStorageServiceIDUniqueness(t *testing.T) {
	gw := func(name, cluster, id string) v1alpha1.StorageObjectGateway {
		return v1alpha1.StorageObjectGateway{
			Metadata: v1alpha1.Metadata{Name: name},
			Spec: v1alpha1.StorageObjectGatewaySpec{
				StorageClusterRef: v1alpha1.LocalObjectReference{Name: cluster},
				Ceph:              v1alpha1.StorageObjectGatewayCephSpec{ServiceID: id},
			},
		}
	}

	dup := v1alpha1.State{StorageObjectGateways: []v1alpha1.StorageObjectGateway{
		gw("east", "ceph", "lab"), gw("west", "ceph", "lab"),
	}}
	if got := strings.Join(validateStorageServiceIDUniqueness(dup), "; "); !strings.Contains(got, "duplicates cephadm service \"rgw/lab\"") {
		t.Fatalf("duplicate rgw serviceID should be rejected, got %q", got)
	}

	distinct := v1alpha1.State{StorageObjectGateways: []v1alpha1.StorageObjectGateway{
		gw("east", "ceph", "east"), gw("west", "ceph", "west"),
	}}
	if got := validateStorageServiceIDUniqueness(distinct); len(got) != 0 {
		t.Fatalf("distinct serviceIDs must pass, got %v", got)
	}

	crossCluster := v1alpha1.State{StorageObjectGateways: []v1alpha1.StorageObjectGateway{
		gw("east", "ceph-a", "lab"), gw("west", "ceph-b", "lab"),
	}}
	if got := validateStorageServiceIDUniqueness(crossCluster); len(got) != 0 {
		t.Fatalf("same serviceID on different clusters must pass, got %v", got)
	}

	nfsDup := v1alpha1.State{StorageNFSExports: []v1alpha1.StorageNFSExport{
		{Metadata: v1alpha1.Metadata{Name: "a"}, Spec: v1alpha1.StorageNFSExportSpec{StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"}, Ceph: v1alpha1.StorageNFSExportCephSpec{ServiceID: "nas"}}},
		{Metadata: v1alpha1.Metadata{Name: "b"}, Spec: v1alpha1.StorageNFSExportSpec{StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"}, Ceph: v1alpha1.StorageNFSExportCephSpec{ServiceID: "nas"}}},
	}}
	if got := strings.Join(validateStorageServiceIDUniqueness(nfsDup), "; "); !strings.Contains(got, "duplicates cephadm service \"nfs/nas\"") {
		t.Fatalf("duplicate nfs serviceID should be rejected, got %q", got)
	}
}

func TestValidateStorageMDSStandbyFeasible(t *testing.T) {
	twoHosts := []string{"h1", "h2"}
	unsat := v1alpha1.StorageCephFSMetadataServices{ActiveCount: 2, StandbyCountWanted: 1}
	if got := strings.Join(validateStorageMDSStandbyFeasible("p", unsat, twoHosts), "; "); !strings.Contains(got, "MDS_INSUFFICIENT_STANDBY") {
		t.Fatalf("unsatisfiable standby intent should be rejected, got %q", got)
	}
	replay := v1alpha1.StorageCephFSMetadataServices{ActiveCount: 2, StandbyReplay: true}
	if got := validateStorageMDSStandbyFeasible("p", replay, twoHosts); len(got) == 0 {
		t.Fatal("standby-replay needing 4 daemons on 2 hosts should be rejected")
	}
	ok := v1alpha1.StorageCephFSMetadataServices{ActiveCount: 1, StandbyCountWanted: 1}
	if got := validateStorageMDSStandbyFeasible("p", ok, []string{"h1", "h2", "h3"}); len(got) != 0 {
		t.Fatalf("satisfiable standby intent must pass, got %v", got)
	}
	if got := validateStorageMDSStandbyFeasible("p", unsat, nil); len(got) != 0 {
		t.Fatalf("empty placement must be skipped here, got %v", got)
	}
}

func TestValidateStorageIngressVIP(t *testing.T) {
	if got := validateStorageIngressVIP("p", "10.0.0.9", 24); len(got) != 0 {
		t.Fatalf("valid IPv4 VIP must pass, got %v", got)
	}
	if got := validateStorageIngressVIP("p", "fd00::9", 64); len(got) != 0 {
		t.Fatalf("valid IPv6 VIP must pass, got %v", got)
	}
	if got := strings.Join(validateStorageIngressVIP("p", "dashboard-vip.example", 999), "; "); !strings.Contains(got, "not a valid IP") || !strings.Contains(got, "out of range") {
		t.Fatalf("garbage address/prefix should be rejected, got %q", got)
	}
	if got := strings.Join(validateStorageIngressVIP("p", "10.0.0.9", 40), "; "); !strings.Contains(got, "out of range (1-32)") {
		t.Fatalf("IPv4 VIP prefix 40 should be rejected, got %q", got)
	}
}

func TestValidateManagementIngressStretchCoverage(t *testing.T) {
	cluster := storageValidationState().StorageClusters[0]
	mgmtWith := func(sites []string) *v1alpha1.StorageCephManagement {
		return &v1alpha1.StorageCephManagement{
			DNSName: "dash.example.test",
			Ingress: v1alpha1.StorageCephManagementIngress{
				Name:         "dash",
				Address:      "10.0.0.9",
				PrefixLength: 24,
				Placement:    v1alpha1.StoragePlacement{Sites: sites},
			},
		}
	}
	cluster.Spec.Ceph.Management = mgmtWith([]string{"dc1"})
	if got := strings.Join(validateStorageCephManagement("spec.ceph.management", cluster, v1alpha1.State{}), "; "); !strings.Contains(got, "data site \"dc2\"") {
		t.Fatalf("mgmt ingress narrowed to one site should fail stretch coverage, got %q", got)
	}
	cluster.Spec.Ceph.Management = mgmtWith([]string{"dc1", "dc2"})
	if got := validateStorageCephManagement("spec.ceph.management", cluster, v1alpha1.State{}); len(got) != 0 {
		t.Fatalf("mgmt ingress covering both data sites must pass, got %v", got)
	}
}

func TestValidateStretchDataSitesDerivedMessage(t *testing.T) {
	cluster := storageValidationState().StorageClusters[0]
	cluster.Spec.Ceph.Topology.Stretch.DataSites = []string{"dc1", "dc2", "dc4"}
	got := strings.Join(validateStorageCephStretch(cluster), "; ")
	if !strings.Contains(got, "[dc1 dc2 dc4]") || !strings.Contains(got, "author spec.ceph.topology.stretch.dataSites") {
		t.Fatalf("derived-dataSites message should show the value and remedy, got %q", got)
	}
}

func TestEndpointNetworkMatchesDedupesByConfig(t *testing.T) {
	ci := v1alpha1.ClusterInstall{
		Metadata: v1alpha1.Metadata{Name: "c"},
		Machines: []v1alpha1.InstallMachine{{
			Name: "m0",
			Network: v1alpha1.MachineNetworkConfig{Spec: &v1alpha1.NetworkConfigSpec{
				MachineNetwork: []v1alpha1.MachineNetworkCIDR{{CIDR: "192.168.140.0/23"}, {CIDR: "192.168.141.0/24"}},
			}},
		}},
	}
	matches := endpointNetworkMatches(ci, nil, net.ParseIP("192.168.141.5"))
	if len(matches) != 1 {
		t.Fatalf("overlapping CIDRs of one owner should dedupe to a single match, got %v", matches)
	}
}

func TestVIPCollidesWithNodeInstallIP(t *testing.T) {
	node := func(name, cidr, addr string, withIface bool) v1alpha1.InstallMachine {
		m := v1alpha1.InstallMachine{
			Name:      name,
			Network:   v1alpha1.MachineNetworkConfig{Spec: &v1alpha1.NetworkConfigSpec{MachineNetwork: []v1alpha1.MachineNetworkCIDR{{CIDR: cidr}}}},
			Addresses: []v1alpha1.MachineAddress{{Name: "ip", Address: addr}},
		}
		if withIface {
			m.Network.InterfaceAddresses = []v1alpha1.MachineInterfaceAddress{{Interface: "eth0", AddressRef: v1alpha1.LocalObjectReference{Name: "ip"}, PrefixLength: 24}}
		}
		return m
	}

	multi := v1alpha1.ClusterInstall{
		Metadata: v1alpha1.Metadata{Name: "c"},
		Machines: []v1alpha1.InstallMachine{
			node("m0", "192.168.140.0/24", "192.168.140.20", true),
			node("m1", "192.168.141.0/24", "192.168.141.21", true),
		},
	}
	vip := "192.168.140.20"
	got := strings.Join(validateEndpointAddressNetwork("p", multi, nil, vip, net.ParseIP(vip)), "; ")
	if !strings.Contains(got, "collides with the static install IP of node \"m0\"") {
		t.Fatalf("VIP equal to a node IP should be rejected on a multi-node cluster, got %q", got)
	}

	sno := v1alpha1.ClusterInstall{
		Metadata: v1alpha1.Metadata{Name: "sno"},
		Machines: []v1alpha1.InstallMachine{node("m0", "192.168.140.0/24", "192.168.140.20", true)},
	}
	if got := validateEndpointAddressNetwork("p", sno, nil, vip, net.ParseIP(vip)); len(got) != 0 {
		t.Fatalf("SNO endpoint equal to the lone node IP must pass, got %v", got)
	}
}

func TestNormalizeClusterNetworkFamilyDefaults(t *testing.T) {
	build := func(cidr string) v1alpha1.State {
		return v1alpha1.State{
			Machines: []v1alpha1.Machine{{
				Metadata: v1alpha1.Metadata{Name: "m0"},
				Spec: v1alpha1.MachineSpec{Network: v1alpha1.MachineNetwork{Config: v1alpha1.MachineNetworkConfig{
					Spec: &v1alpha1.NetworkConfigSpec{MachineNetwork: []v1alpha1.MachineNetworkCIDR{{CIDR: cidr}}},
				}}},
			}},
			ContainerClusters: []v1alpha1.ContainerCluster{{
				Metadata: v1alpha1.Metadata{Name: "cl"},
				Spec: v1alpha1.ContainerClusterSpec{
					Nodes: []v1alpha1.OCPNodeSpec{{Name: "m0", MachineRef: v1alpha1.LocalObjectReference{Name: "m0"}}},
				},
			}},
		}
	}

	v6 := build("fd00:132::/64")
	Normalize(&v6)
	net6 := v6.ContainerClusters[0].Spec.Networking
	if net6 == nil || len(net6.ClusterNetwork) != 1 || net6.ClusterNetwork[0].CIDR != defaultClusterNetworkCIDRIPv6 {
		t.Fatalf("v6-only cluster clusterNetwork = %+v, want IPv6 default", net6)
	}
	if len(net6.ServiceNetwork) != 1 || net6.ServiceNetwork[0] != defaultServiceNetworkCIDRIPv6 {
		t.Fatalf("v6-only cluster serviceNetwork = %+v, want IPv6 default", net6.ServiceNetwork)
	}

	v4 := build("192.168.140.0/24")
	Normalize(&v4)
	net4 := v4.ContainerClusters[0].Spec.Networking
	if net4.ClusterNetwork[0].CIDR != v1alpha1.DefaultClusterNetworkCIDR || net4.ServiceNetwork[0] != v1alpha1.DefaultServiceNetworkCIDR {
		t.Fatalf("v4 cluster must keep IPv4 defaults, got clusterNetwork %+v serviceNetwork %v", net4.ClusterNetwork, net4.ServiceNetwork)
	}
}

func TestValidateEnvironmentResourcesDuplicates(t *testing.T) {
	dup := v1alpha1.Environment{
		Metadata: v1alpha1.Metadata{Name: "env"},
		Spec:     v1alpha1.EnvironmentSpec{Resources: []string{"a/b", "a/b"}},
	}
	if got := strings.Join(validateEnvironmentResources(dup), "; "); !strings.Contains(got, "is a duplicate") {
		t.Fatalf("duplicate resource entry should be rejected, got %q", got)
	}
	ok := v1alpha1.Environment{
		Metadata: v1alpha1.Metadata{Name: "env"},
		Spec:     v1alpha1.EnvironmentSpec{Resources: []string{"a/b", "c/d"}},
	}
	if got := validateEnvironmentResources(ok); len(got) != 0 {
		t.Fatalf("distinct resource entries must pass, got %v", got)
	}
}

func TestValidateClusterAddonOLMRejectsInlineSecret(t *testing.T) {
	addonWith := func(cr map[string]any) v1alpha1.ClusterAddon {
		return v1alpha1.ClusterAddon{
			Metadata: v1alpha1.Metadata{Name: "op"},
			Spec: v1alpha1.ClusterAddonSpec{
				Type: v1alpha1.ClusterAddonTypeOLM,
				OLM:  &v1alpha1.ClusterAddonOLMSpec{CustomResources: []map[string]any{cr}},
			},
		}
	}
	secret := addonWith(map[string]any{
		"apiVersion": "v1", "kind": "Secret",
		"metadata":   map[string]any{"name": "creds"},
		"stringData": map[string]any{"password": "hunter2"},
	})
	if got := strings.Join(validateClusterAddonOLM(secret), "; "); !strings.Contains(got, "must not embed secret bytes") {
		t.Fatalf("inline Secret stringData should be rejected, got %q", got)
	}
	data := addonWith(map[string]any{
		"apiVersion": "v1", "kind": "Secret",
		"metadata": map[string]any{"name": "creds"},
		"data":     map[string]any{"password": "aHVudGVyMg=="},
	})
	if got := strings.Join(validateClusterAddonOLM(data), "; "); !strings.Contains(got, "must not embed secret bytes") {
		t.Fatalf("inline Secret data should be rejected, got %q", got)
	}
	cm := addonWith(map[string]any{
		"apiVersion": "v1", "kind": "ConfigMap",
		"metadata": map[string]any{"name": "cfg"},
		"data":     map[string]any{"key": "value"},
	})
	if got := strings.Join(validateClusterAddonOLM(cm), "; "); strings.Contains(got, "must not embed secret bytes") {
		t.Fatalf("non-Secret custom resource must not trip the secret guard, got %q", got)
	}
}
