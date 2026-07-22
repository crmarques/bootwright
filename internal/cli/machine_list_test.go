package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/ownership"
)

func machineListTestState() v1alpha1.State {
	return v1alpha1.State{
		Machines: []v1alpha1.Machine{
			{
				Metadata: v1alpha1.Metadata{Name: "ceph-0"},
				Spec: v1alpha1.MachineSpec{
					Substrate: v1alpha1.MachineSubstrate{ProviderRef: v1alpha1.LocalObjectReference{Name: "bm"}},
					OS: v1alpha1.MachineOSSpec{
						Provided:          v1alpha1.BoolPtr(false),
						InstallProfileRef: v1alpha1.LocalObjectReference{Name: "rhel9"},
					},
					Addresses: []v1alpha1.MachineAddress{{Name: "ssh", Address: "10.0.0.10"}},
					Access: v1alpha1.MachineAccess{SSH: &v1alpha1.MachineSSHSpec{
						AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"},
						KeyRef:     v1alpha1.SecretRef{Name: "ceph-key"},
					}},
				},
			},
			{
				Metadata: v1alpha1.Metadata{Name: "ocp-0"},
				Spec: v1alpha1.MachineSpec{
					Substrate: v1alpha1.MachineSubstrate{ProviderRef: v1alpha1.LocalObjectReference{Name: "kv"}},
					OS:        v1alpha1.MachineOSSpec{Provided: v1alpha1.BoolPtr(false)},
					Addresses: []v1alpha1.MachineAddress{{Name: "ssh", Address: "10.0.0.20"}},
					Access: v1alpha1.MachineAccess{SSH: &v1alpha1.MachineSSHSpec{
						AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"},
						KeyRef:     v1alpha1.SecretRef{Name: "ocp-key"},
					}},
				},
			},
			{
				Metadata: v1alpha1.Metadata{Name: "bastion"},
				Spec: v1alpha1.MachineSpec{
					OS:        v1alpha1.MachineOSSpec{Provided: v1alpha1.BoolPtr(true)},
					Addresses: []v1alpha1.MachineAddress{{Name: "ssh", Address: "bastion.example.test"}},
					Access: v1alpha1.MachineAccess{SSH: &v1alpha1.MachineSSHSpec{
						AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"},
						User:       "core",
						KeyRef:     v1alpha1.SecretRef{Name: "bastion-key"},
					}},
				},
			},
		},
		InfraProviders: []v1alpha1.InfraProvider{
			{Metadata: v1alpha1.Metadata{Name: "bm"}, Spec: v1alpha1.InfraProviderSpec{Type: v1alpha1.ProvisionerBareMetal}},
			{Metadata: v1alpha1.Metadata{Name: "kv"}, Spec: v1alpha1.InfraProviderSpec{Type: v1alpha1.ProvisionerKubeVirt}},
		},
		MachineInstallProfiles: []v1alpha1.MachineInstallProfile{
			{Metadata: v1alpha1.Metadata{Name: "rhel9"}, Spec: v1alpha1.MachineInstallProfileSpec{
				OS: v1alpha1.MachineInstallOS{Family: "rhel", Version: "9.4", Architecture: "x86_64"},
			}},
		},
		ContainerClusters: []v1alpha1.ContainerCluster{
			{Metadata: v1alpha1.Metadata{Name: "managed-01"}, Spec: v1alpha1.ContainerClusterSpec{
				Nodes: []v1alpha1.OCPNodeSpec{{MachineRef: v1alpha1.LocalObjectReference{Name: "ocp-0"}, Role: v1alpha1.NodeRoleMaster}},
			}},
		},
		StorageClusters: []v1alpha1.StorageCluster{
			{Metadata: v1alpha1.Metadata{Name: "ceph-dc1"}, Spec: v1alpha1.StorageClusterSpec{
				Ceph: &v1alpha1.StorageClusterCephSpec{Topology: v1alpha1.StorageCephTopology{
					Nodes: []v1alpha1.StorageCephNode{{MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-0"}, Roles: []string{"mon", "osd"}}},
				}},
			}},
		},
	}
}

func TestBuildMachineListDerivesStateOSAndCluster(t *testing.T) {
	state := machineListTestState()
	records := []ownership.ResourceRecord{{Kind: string(ownership.KindManagedOSInstall), Name: "ceph-0", Machine: "ceph-0"}}

	entries := buildMachineList(state, records, nil)
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3: %+v", len(entries), entries)
	}
	byName := map[string]machineListEntry{}
	for _, e := range entries {
		byName[e.Name] = e
	}

	if entries[0].Name != "bastion" || entries[1].Name != "ceph-0" || entries[2].Name != "ocp-0" {
		t.Fatalf("entries not sorted by name: %v", []string{entries[0].Name, entries[1].Name, entries[2].Name})
	}

	ceph := byName["ceph-0"]
	if ceph.State != machineStateProvisioned || !ceph.Provisioned {
		t.Fatalf("ceph-0 state = %q provisioned=%v, want provisioned", ceph.State, ceph.Provisioned)
	}
	if ceph.OS != "rhel 9.4 (x86_64)" {
		t.Fatalf("ceph-0 OS = %q", ceph.OS)
	}
	if ceph.Substrate != "baremetal" {
		t.Fatalf("ceph-0 substrate = %q", ceph.Substrate)
	}
	if ceph.Cluster != "ceph-dc1" || ceph.ClusterKind != "storage" || ceph.Role != "mon,osd" {
		t.Fatalf("ceph-0 cluster = %q/%q role=%q", ceph.Cluster, ceph.ClusterKind, ceph.Role)
	}
	if ceph.SSHUser != "root" || ceph.SSHAddress != "10.0.0.10" {
		t.Fatalf("ceph-0 ssh = %q@%q", ceph.SSHUser, ceph.SSHAddress)
	}

	ocp := byName["ocp-0"]
	if ocp.State != machineStateNotProvisioned || ocp.Provisioned {
		t.Fatalf("ocp-0 state = %q provisioned=%v, want not-provisioned", ocp.State, ocp.Provisioned)
	}
	if ocp.OS != "" {
		t.Fatalf("ocp-0 OS = %q, want empty (agent-installed, no profile)", ocp.OS)
	}
	if ocp.Substrate != "kubevirt" || ocp.Cluster != "managed-01" || ocp.Role != "master" {
		t.Fatalf("ocp-0 substrate=%q cluster=%q role=%q", ocp.Substrate, ocp.Cluster, ocp.Role)
	}

	bastion := byName["bastion"]
	if bastion.State != machineStateExternal || bastion.Provisioned {
		t.Fatalf("bastion state = %q, want external", bastion.State)
	}
	if bastion.OS != "provided" || bastion.Substrate != "" || bastion.Cluster != "" {
		t.Fatalf("bastion OS=%q substrate=%q cluster=%q", bastion.OS, bastion.Substrate, bastion.Cluster)
	}
	if bastion.SSHUser != "core" {
		t.Fatalf("bastion ssh user = %q, want core", bastion.SSHUser)
	}
}

func TestBuildMachineListClusterFilter(t *testing.T) {
	state := machineListTestState()
	filter, err := resolveMachineClusterFilter(state, "ceph-dc1")
	if err != nil {
		t.Fatalf("resolveMachineClusterFilter: %v", err)
	}
	entries := buildMachineList(state, nil, filter)
	if len(entries) != 1 || entries[0].Name != "ceph-0" {
		t.Fatalf("filtered entries = %+v, want only ceph-0", entries)
	}
}

func TestResolveMachineClusterFilterRejectsUnknown(t *testing.T) {
	state := machineListTestState()
	_, err := resolveMachineClusterFilter(state, "ceph-dc1,nope")
	if err == nil {
		t.Fatal("expected error for unknown cluster")
	}
	if !strings.Contains(err.Error(), "nope") || !strings.Contains(err.Error(), "available:") {
		t.Fatalf("error = %q, want it to name the unknown cluster and list available", err)
	}
}

func TestMachineListCLIOutputs(t *testing.T) {
	initHostTrustTestContext(t)

	stdout, stderr, code := runCLI(t, "machine", "list")
	if code != 0 {
		t.Fatalf("machine list exited %d, stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "provider-01") || !strings.Contains(stdout, "external OS") {
		t.Fatalf("text output missing machine/state:\n%s", stdout)
	}

	stdout, stderr, code = runCLI(t, "machine", "list", "--output", "json")
	if code != 0 {
		t.Fatalf("machine list json exited %d, stderr=%q", code, stderr)
	}
	var report machineListReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode report: %v\n%s", err, stdout)
	}
	if len(report.Machines) != 1 || report.Machines[0].Name != "provider-01" {
		t.Fatalf("machines = %+v", report.Machines)
	}
	if report.Machines[0].State != machineStateExternal {
		t.Fatalf("provider-01 state = %q, want external", report.Machines[0].State)
	}

	stdout, stderr, code = runCLI(t, "machine", "list", "--silent")
	if code != 0 {
		t.Fatalf("machine list --silent exited %d, stderr=%q", code, stderr)
	}
	if strings.TrimSpace(stdout) != "provider-01" {
		t.Fatalf("silent output = %q, want just the name", stdout)
	}

	if _, _, code = runCLI(t, "machine", "list", "--clusters", "does-not-exist"); code != 2 {
		t.Fatalf("machine list --clusters unknown exited %d, want 2", code)
	}
}
