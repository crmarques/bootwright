package desiredstate

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

func TestNormalizeInjectsMachineFQDN(t *testing.T) {
	state := v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata: v1alpha1.Metadata{Name: "env"},
			Spec:     v1alpha1.EnvironmentSpec{Domains: v1alpha1.EnvironmentDomainsSpec{Base: "example.test"}},
		}},
		Machines: []v1alpha1.Machine{
			{Metadata: v1alpha1.Metadata{Name: "plain"}},
			{
				Metadata: v1alpha1.Metadata{Name: "overridden"},
				Spec: v1alpha1.MachineSpec{Addresses: []v1alpha1.MachineAddress{{
					Name:    v1alpha1.MachineAddressFQDN,
					Address: "srv4009.corp.example.com",
				}}},
			},
		},
	}

	Normalize(&state)

	if got := v1alpha1.MachineFQDNAddress(state.Machines[0]); got != "plain.example.test" {
		t.Fatalf("injected fqdn = %q, want plain.example.test", got)
	}
	if got := v1alpha1.MachineFQDNAddress(state.Machines[1]); got != "srv4009.corp.example.com" {
		t.Fatalf("declared fqdn = %q, want srv4009.corp.example.com kept verbatim", got)
	}
	count := 0
	for _, address := range state.Machines[1].Spec.Addresses {
		if address.Name == v1alpha1.MachineAddressFQDN {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("declared fqdn entries = %d, want exactly one (no duplicate injection)", count)
	}
}

func TestNormalizeSkipsFQDNWithoutBaseDomain(t *testing.T) {
	state := v1alpha1.State{
		Environments: []v1alpha1.Environment{{Metadata: v1alpha1.Metadata{Name: "env"}}},
		Machines:     []v1alpha1.Machine{{Metadata: v1alpha1.Metadata{Name: "plain"}}},
	}

	Normalize(&state)

	if got := v1alpha1.MachineFQDNAddress(state.Machines[0]); got != "" {
		t.Fatalf("fqdn = %q, want none without an Environment baseDomain", got)
	}
}

func TestValidateMachineFQDNMustBeSubdomain(t *testing.T) {
	machine := v1alpha1.Machine{
		Metadata: v1alpha1.Metadata{Name: "node"},
		Spec: v1alpha1.MachineSpec{Addresses: []v1alpha1.MachineAddress{{
			Name:    v1alpha1.MachineAddressFQDN,
			Address: "bad_host!.example.test",
		}}},
	}
	errs := validateMachineAddresses("Machine/node spec", machine)
	if !containsSubstring(errs, "is not a DNS subdomain") {
		t.Fatalf("expected DNS-subdomain error for fqdn address, got %v", errs)
	}

	ok := v1alpha1.Machine{
		Metadata: v1alpha1.Metadata{Name: "node"},
		Spec: v1alpha1.MachineSpec{Addresses: []v1alpha1.MachineAddress{{
			Name:    v1alpha1.MachineAddressFQDN,
			Address: "srv4009.corp.example.com",
		}}},
	}
	if errs := validateMachineAddresses("Machine/node spec", ok); len(errs) != 0 {
		t.Fatalf("valid fqdn subdomain rejected: %v", errs)
	}
}

func machineWithFQDN(name, entry string) v1alpha1.Machine {
	return v1alpha1.Machine{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.MachineSpec{Addresses: []v1alpha1.MachineAddress{{
			Name:    v1alpha1.MachineAddressFQDN,
			Address: entry,
		}}},
	}
}

func TestValidateUniqueMachineFQDNs(t *testing.T) {
	dup := v1alpha1.State{Machines: []v1alpha1.Machine{
		machineWithFQDN("a", "shared.example.test"),
		machineWithFQDN("b", "shared.example.test"),
		machineWithFQDN("c", "unique.example.test"),
	}}
	errs := validateUniqueMachineFQDNs(dup)
	if len(errs) != 1 || !strings.Contains(errs[0], `fqdn "shared.example.test"`) || !strings.Contains(errs[0], "a, b") {
		t.Fatalf("duplicate fqdn must refuse and name both machines, got %v", errs)
	}
	ok := v1alpha1.State{Machines: []v1alpha1.Machine{
		machineWithFQDN("a", "a.example.test"),
		machineWithFQDN("b", "b.example.test"),
	}}
	if e := validateUniqueMachineFQDNs(ok); len(e) != 0 {
		t.Fatalf("distinct fqdn values must pass, got %v", e)
	}
}

func TestNormalizeDefaultsNodeHostnames(t *testing.T) {
	state := v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata: v1alpha1.Metadata{Name: "env"},
			Spec:     v1alpha1.EnvironmentSpec{Domains: v1alpha1.EnvironmentDomainsSpec{Base: "example.test"}},
		}},
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "cluster-a"},
			Spec: v1alpha1.ContainerClusterSpec{Nodes: []v1alpha1.OCPNodeSpec{
				{Name: "node01", Role: "master", MachineRef: v1alpha1.LocalObjectReference{Name: "m1"}},
				{Name: "master-0", Role: "master", MachineRef: v1alpha1.LocalObjectReference{Name: "m2"}},
				{Name: "pinned.corp.example.com", Role: "master", MachineRef: v1alpha1.LocalObjectReference{Name: "m3"}},
			}},
		}},
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: "ceph"},
			Spec: v1alpha1.StorageClusterSpec{
				Type: v1alpha1.StorageClusterTypeCeph,
				Ceph: &v1alpha1.StorageClusterCephSpec{
					Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{
						{Name: "node01", MachineRef: v1alpha1.LocalObjectReference{Name: "s1"}, Roles: []string{v1alpha1.StorageCephRoleMON}},
						{Name: "node02", MachineRef: v1alpha1.LocalObjectReference{Name: "s2"}, Roles: []string{v1alpha1.StorageCephRoleMON}},
					}},
				},
			},
		}},
	}

	Normalize(&state)

	hosts := state.ContainerClusters[0].Spec.Nodes
	if got := hosts[0].Name; got != "node01.cluster-a.example.test" {
		t.Fatalf("omitted hostname = %q, want node01.cluster-a.example.test", got)
	}
	if got := hosts[1].Name; got != "master-0.cluster-a.example.test" {
		t.Fatalf("bare-label hostname = %q, want master-0.cluster-a.example.test", got)
	}
	if got := hosts[2].Name; got != "pinned.corp.example.com" {
		t.Fatalf("dotted hostname = %q, want pinned.corp.example.com verbatim", got)
	}
	ceph := state.StorageClusters[0].Spec.Ceph.Topology.Nodes
	if ceph[0].Name != "node01.ceph.example.test" || ceph[1].Name != "node02.ceph.example.test" {
		t.Fatalf("ceph node names = %q, %q, want node01.ceph.example.test, node02.ceph.example.test", ceph[0].Name, ceph[1].Name)
	}
}

func TestNormalizeComposesPerClassDomains(t *testing.T) {
	state := v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata: v1alpha1.Metadata{Name: "env"},
			Spec: v1alpha1.EnvironmentSpec{Domains: v1alpha1.EnvironmentDomainsSpec{
				Base:              "example.net",
				Machines:          "corp.example.net",
				ContainerClusters: "ocp.cloud.example.net",
				StorageClusters:   "ceph.cloud.example.net",
			}},
		}},
		Machines: []v1alpha1.Machine{{Metadata: v1alpha1.Metadata{Name: "srv01"}}},
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "cluster-a"},
			Spec: v1alpha1.ContainerClusterSpec{Nodes: []v1alpha1.OCPNodeSpec{
				{Name: "node01", Role: "master", MachineRef: v1alpha1.LocalObjectReference{Name: "m1"}},
			}},
		}},
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: "ceph"},
			Spec: v1alpha1.StorageClusterSpec{
				Type: v1alpha1.StorageClusterTypeCeph,
				Ceph: &v1alpha1.StorageClusterCephSpec{
					Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{
						{Name: "node01", MachineRef: v1alpha1.LocalObjectReference{Name: "s1"}, Roles: []string{v1alpha1.StorageCephRoleMON}},
					}},
				},
			},
		}},
	}

	Normalize(&state)

	if got := v1alpha1.MachineFQDNAddress(state.Machines[0]); got != "srv01.corp.example.net" {
		t.Fatalf("machine fqdn = %q, want srv01.corp.example.net (domains.machines)", got)
	}
	if got := state.ContainerClusters[0].Spec.Nodes[0].Name; got != "node01.cluster-a.ocp.cloud.example.net" {
		t.Fatalf("container node = %q, want node01.cluster-a.ocp.cloud.example.net (domains.containerClusters)", got)
	}
	if got := state.StorageClusters[0].Spec.Ceph.Topology.Nodes[0].Name; got != "node01.ceph.ceph.cloud.example.net" {
		t.Fatalf("ceph node = %q, want node01.ceph.ceph.cloud.example.net (domains.storageClusters)", got)
	}
}

func TestNormalizeKeepsBareNodeHostnamesWithoutBaseDomain(t *testing.T) {
	state := v1alpha1.State{
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "cluster-a"},
			Spec: v1alpha1.ContainerClusterSpec{Nodes: []v1alpha1.OCPNodeSpec{
				{Name: "node01", Role: "master", MachineRef: v1alpha1.LocalObjectReference{Name: "m1"}},
				{Name: "master-0", Role: "master", MachineRef: v1alpha1.LocalObjectReference{Name: "m2"}},
			}},
		}},
	}

	Normalize(&state)

	hosts := state.ContainerClusters[0].Spec.Nodes
	if got := hosts[0].Name; got != "node01" {
		t.Fatalf("omitted hostname = %q, want bare node01 without a baseDomain", got)
	}
	if got := hosts[1].Name; got != "master-0" {
		t.Fatalf("bare-label hostname = %q, want master-0 kept bare without a baseDomain", got)
	}
}

func TestNormalizeDefaultsCephManagementAndGatewayDNSNames(t *testing.T) {
	state := v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata: v1alpha1.Metadata{Name: "env"},
			Spec: v1alpha1.EnvironmentSpec{Domains: v1alpha1.EnvironmentDomainsSpec{
				Base:            "example.net",
				StorageClusters: "ceph.cloud.example.net",
			}},
		}},
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: "ceph"},
			Spec: v1alpha1.StorageClusterSpec{
				Type: v1alpha1.StorageClusterTypeCeph,
				Ceph: &v1alpha1.StorageClusterCephSpec{
					Management: &v1alpha1.StorageCephManagement{
						Ingress: v1alpha1.StorageCephManagementIngress{Name: "mgmt", Address: "192.168.0.1", PrefixLength: 24},
					},
				},
			},
		}, {
			Metadata: v1alpha1.Metadata{Name: "ceph-east"},
			Spec: v1alpha1.StorageClusterSpec{
				Type: v1alpha1.StorageClusterTypeCeph,
				Ceph: &v1alpha1.StorageClusterCephSpec{
					Management: &v1alpha1.StorageCephManagement{
						DNSLabel: "dashboard",
						Ingress:  v1alpha1.StorageCephManagementIngress{Name: "mgmt", Address: "192.168.0.2", PrefixLength: 24},
					},
				},
			},
		}},
		StorageObjectGateways: []v1alpha1.StorageObjectGateway{
			{
				Metadata: v1alpha1.Metadata{Name: "rgw"},
				Spec: v1alpha1.StorageObjectGatewaySpec{
					StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
				},
			},
			{
				Metadata: v1alpha1.Metadata{Name: "rgw-east"},
				Spec: v1alpha1.StorageObjectGatewaySpec{
					StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
					Public:            v1alpha1.StorageObjectGatewayPublic{DNSLabel: "s3"},
				},
			},
		},
	}

	Normalize(&state)

	if got := state.StorageClusters[0].Spec.Ceph.Management.DNSLabel; got != "mgr" {
		t.Fatalf("management dnsLabel = %q, want the mgr default", got)
	}
	if got := stateview.StorageManagementFQDN(state, state.StorageClusters[0]); got != "mgr.ceph.ceph.cloud.example.net" {
		t.Fatalf("management fqdn = %q, want mgr.ceph.ceph.cloud.example.net", got)
	}
	if got := state.StorageClusters[1].Spec.Ceph.Management.DNSLabel; got != "dashboard" {
		t.Fatalf("declared management dnsLabel = %q, want dashboard kept verbatim", got)
	}
	if got := stateview.StorageManagementFQDN(state, state.StorageClusters[1]); got != "dashboard.ceph-east.ceph.cloud.example.net" {
		t.Fatalf("management fqdn = %q, want dashboard.ceph-east.ceph.cloud.example.net", got)
	}
	if got := state.StorageObjectGateways[0].Spec.Public.DNSLabel; got != "rgw" {
		t.Fatalf("gateway dnsLabel = %q, want the gateway's own name as the default label", got)
	}
	if got := stateview.StorageGatewayFQDN(state, state.StorageObjectGateways[0]); got != "rgw.ceph.ceph.cloud.example.net" {
		t.Fatalf("gateway fqdn = %q, want rgw.ceph.ceph.cloud.example.net (keyed by the gateway's own name)", got)
	}
	if got := state.StorageObjectGateways[1].Spec.Public.DNSLabel; got != "s3" {
		t.Fatalf("declared gateway dnsLabel = %q, want s3 kept verbatim", got)
	}
	if got := stateview.StorageGatewayFQDN(state, state.StorageObjectGateways[1]); got != "s3.ceph.ceph.cloud.example.net" {
		t.Fatalf("gateway fqdn = %q, want s3.ceph.ceph.cloud.example.net", got)
	}
}

func TestNormalizeKeepsCephDNSNamesEmptyWithoutStorageDomain(t *testing.T) {
	state := v1alpha1.State{
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: "ceph"},
			Spec: v1alpha1.StorageClusterSpec{
				Type: v1alpha1.StorageClusterTypeCeph,
				Ceph: &v1alpha1.StorageClusterCephSpec{
					Management: &v1alpha1.StorageCephManagement{
						Ingress: v1alpha1.StorageCephManagementIngress{Name: "mgmt", Address: "192.168.0.1", PrefixLength: 24},
					},
				},
			},
		}},
		StorageObjectGateways: []v1alpha1.StorageObjectGateway{{
			Metadata: v1alpha1.Metadata{Name: "rgw"},
			Spec: v1alpha1.StorageObjectGatewaySpec{
				StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
			},
		}},
	}

	Normalize(&state)

	if got := stateview.StorageManagementFQDN(state, state.StorageClusters[0]); got != "" {
		t.Fatalf("management fqdn = %q, want empty without an Environment storage domain", got)
	}
	if got := stateview.StorageGatewayFQDN(state, state.StorageObjectGateways[0]); got != "" {
		t.Fatalf("gateway fqdn = %q, want empty without an Environment storage domain", got)
	}
}

func TestStorageBootstrapHostRejectsMachineName(t *testing.T) {
	const environment = `apiVersion: bootwright.io/v1alpha1
kind: Environment
metadata:
  name: env
spec:
  domains:
    base: bootwright.test
`
	const machine = `apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata:
  name: ceph-0
spec:
  capabilities:
    - ceph-node
  os:
    provided: true
  addresses:
    - name: ssh
      address: 192.0.2.10
  access:
    ssh:
      user: root
      auth:
        privateKeyRef: ceph-node-ssh
      addressRef: ssh
`
	const storage = `apiVersion: bootwright.io/v1alpha1
kind: StorageCluster
metadata:
  name: ceph
spec:
  type: ceph
  ceph:
    cephadm:
      clusterSSH:
        user: root
      bootstrap:
        node: ceph-0
    topology:
      nodes:
        - name: node01
          machineRef: ceph-0
          roles: [mon]
`
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"environment.yaml": environment,
		"secrets.yaml":     cephNodeSSHSecretYAML,
		"machine.yaml":     machine,
		"storage.yaml":     storage,
	})
	_, err := LoadNormalizeValidate([]string{dir})
	if err == nil {
		t.Fatal("expected machine-name bootstrap.node to be rejected, got nil")
	}
	want := `spec.ceph.cephadm.bootstrap.node "ceph-0" does not match any node name in spec.ceph.topology.nodes; it names the bound Machine, but clusters reference nodes — use the node's name`
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not contain %q", err, want)
	}
}

func TestStorageStretchTiebreakerRejectsMachineName(t *testing.T) {
	state := storageValidationState()
	state.StorageClusters[0].Spec.Ceph.Topology.Nodes[6].Name = "arbiter"
	got := strings.Join(validateStorage(state), "; ")
	want := `tiebreaker.node "ceph-arbiter" must name a spec.ceph.topology.nodes[] entry by node name; it names the bound Machine, but clusters reference nodes — use the node's name`
	if !strings.Contains(got, want) {
		t.Fatalf("validateStorage errors = %q, want substring %q", got, want)
	}
}

func TestValidateClusterBoundHostnameSourceMachineName(t *testing.T) {
	clusterBound := func(name string) v1alpha1.Machine {
		return v1alpha1.Machine{
			Metadata: v1alpha1.Metadata{Name: name},
			Spec: v1alpha1.MachineSpec{OS: v1alpha1.MachineOSSpec{
				Provided:          v1alpha1.BoolPtr(false),
				InstallProfileRef: v1alpha1.LocalObjectReference{Name: "rhel"},
			}},
		}
	}
	state := v1alpha1.State{
		MachineInstallProfiles: []v1alpha1.MachineInstallProfile{{
			Metadata: v1alpha1.Metadata{Name: "rhel"},
			Spec: v1alpha1.MachineInstallProfileSpec{
				Customizations: v1alpha1.MachineInstallCustomizations{
					Hostname: v1alpha1.MachineInstallHostname{Source: v1alpha1.MachineInstallHostnameMachineName},
				},
			},
		}},
		Machines: []v1alpha1.Machine{clusterBound("ceph-0"), clusterBound("standalone")},
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: "ceph"},
			Spec: v1alpha1.StorageClusterSpec{
				Type: v1alpha1.StorageClusterTypeCeph,
				Ceph: &v1alpha1.StorageClusterCephSpec{
					Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{{
						Name:       "node01",
						MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-0"},
						Roles:      []string{v1alpha1.StorageCephRoleMON},
					}}},
				},
			},
		}},
	}
	errs := validateClusterBoundHostnameSource(state)
	if len(errs) != 1 {
		t.Fatalf("want exactly one error for the cluster-bound machine (standalone must pass), got %v", errs)
	}
	if !strings.Contains(errs[0], "Machine/ceph-0") || !strings.Contains(errs[0], "cluster node's OS hostname must equal its node FQDN") {
		t.Fatalf("error %q must name Machine/ceph-0 and explain the node-FQDN requirement", errs[0])
	}
}
