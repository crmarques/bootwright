package inventory

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func twoPortgroupState() v1alpha1.State {
	return v1alpha1.State{
		InfraProviders: []v1alpha1.InfraProvider{{
			Metadata: v1alpha1.Metadata{Name: "vsphere"},
			Spec: v1alpha1.InfraProviderSpec{
				Type: v1alpha1.ProvisionerVSphere,
				NetworkAttachments: []v1alpha1.NetworkAttachmentCapability{
					{Name: "site-a", VSphere: &v1alpha1.NetworkAttachmentVSphere{Portgroup: "pg-site-a"}},
					{Name: "site-b", VSphere: &v1alpha1.NetworkAttachmentVSphere{Portgroup: "pg-site-b", DistributedSwitch: "dvs-site-b"}},
				},
			},
		}},
	}
}

func twoPortgroupInstall() v1alpha1.ClusterInstall {
	return v1alpha1.ClusterInstall{
		NetworkBindings: []v1alpha1.MachineNetworkBinding{
			{
				MachineName:      "arb-a",
				NetworkConfigRef: v1alpha1.LocalObjectReference{Name: "ceph-public"},
				ProviderRef:      v1alpha1.LocalObjectReference{Name: "vsphere"},
				AttachmentRef:    v1alpha1.LocalObjectReference{Name: "site-a"},
			},
			{
				MachineName:      "arb-b",
				NetworkConfigRef: v1alpha1.LocalObjectReference{Name: "ceph-public"},
				ProviderRef:      v1alpha1.LocalObjectReference{Name: "vsphere"},
				AttachmentRef:    v1alpha1.LocalObjectReference{Name: "site-b"},
			},
		},
	}
}

func installMachineOn(name string) v1alpha1.InstallMachine {
	return v1alpha1.InstallMachine{
		Name:    name,
		Source:  v1alpha1.InstallMachineSource{ProviderRef: v1alpha1.LocalObjectReference{Name: "vsphere"}},
		Network: v1alpha1.MachineNetworkConfig{NetworkConfigRef: v1alpha1.LocalObjectReference{Name: "ceph-public"}},
	}
}

func TestMachineNetworkAttachmentResolvesPerMachine(t *testing.T) {
	state := twoPortgroupState()
	ci := twoPortgroupInstall()

	a := clusterMachineNetworkAttachmentVars(state, ci, installMachineOn("arb-a"))
	if a == nil {
		t.Fatal("arb-a resolved no attachment")
	}
	if got := a["vsphere"].(map[string]any)["portgroup"]; got != "pg-site-a" {
		t.Fatalf("arb-a portgroup = %v, want pg-site-a", got)
	}

	b := clusterMachineNetworkAttachmentVars(state, ci, installMachineOn("arb-b"))
	if b == nil {
		t.Fatal("arb-b resolved no attachment")
	}
	if got := b["vsphere"].(map[string]any)["portgroup"]; got != "pg-site-b" {
		t.Fatalf("arb-b portgroup = %v, want pg-site-b; machines sharing a provider and NetworkConfig must not collapse onto the first binding", got)
	}
}

func TestVSphereAttachmentRendersDistributedSwitchOnlyWhenAuthored(t *testing.T) {
	state := twoPortgroupState()
	ci := twoPortgroupInstall()

	a := clusterMachineNetworkAttachmentVars(state, ci, installMachineOn("arb-a"))["vsphere"].(map[string]any)
	if _, ok := a["distributedSwitch"]; ok {
		t.Fatalf("unauthored distributedSwitch rendered: %v", a)
	}

	b := clusterMachineNetworkAttachmentVars(state, ci, installMachineOn("arb-b"))["vsphere"].(map[string]any)
	if got := b["distributedSwitch"]; got != "dvs-site-b" {
		t.Fatalf("distributedSwitch = %v, want dvs-site-b", got)
	}
}

func TestMachineNetworkAttachmentFallsBackWhenBindingsCarryNoMachine(t *testing.T) {
	state := twoPortgroupState()
	ci := v1alpha1.ClusterInstall{
		NetworkBindings: []v1alpha1.MachineNetworkBinding{{
			NetworkConfigRef: v1alpha1.LocalObjectReference{Name: "ceph-public"},
			ProviderRef:      v1alpha1.LocalObjectReference{Name: "vsphere"},
			AttachmentRef:    v1alpha1.LocalObjectReference{Name: "site-a"},
		}},
	}
	vars := clusterMachineNetworkAttachmentVars(state, ci, installMachineOn("arb-a"))
	if vars == nil {
		t.Fatal("machine-less binding resolved no attachment")
	}
	if got := vars["vsphere"].(map[string]any)["portgroup"]; got != "pg-site-a" {
		t.Fatalf("portgroup = %v, want pg-site-a", got)
	}
}

func TestKubeVirtInterfacesRenderDistinctNetworkAttachments(t *testing.T) {
	state := v1alpha1.State{InfraProviders: []v1alpha1.InfraProvider{{
		Metadata: v1alpha1.Metadata{Name: "kubevirt"},
		Spec: v1alpha1.InfraProviderSpec{
			Type: v1alpha1.ProvisionerKubeVirt,
			NetworkAttachments: []v1alpha1.NetworkAttachmentCapability{
				{Name: "machine", KubeVirt: &v1alpha1.NetworkAttachmentKubeVirt{NetworkRef: v1alpha1.KubeVirtNetworkRef{Name: "machine", Namespace: "child"}}},
				{Name: "ceph-public", KubeVirt: &v1alpha1.NetworkAttachmentKubeVirt{NetworkRef: v1alpha1.KubeVirtNetworkRef{Name: "ceph-public", Namespace: "child"}}},
			},
		},
	}}}
	machine := v1alpha1.InstallMachine{
		Source: v1alpha1.InstallMachineSource{ProviderRef: v1alpha1.LocalObjectReference{Name: "kubevirt"}},
		Network: v1alpha1.MachineNetworkConfig{InterfaceAttachments: []v1alpha1.MachineInterfaceAttachment{
			{Interface: "primary", AttachmentRef: v1alpha1.LocalObjectReference{Name: "machine"}},
			{Interface: "ceph-public", AttachmentRef: v1alpha1.LocalObjectReference{Name: "ceph-public"}},
		}},
	}
	interfaces := []v1alpha1.MachineNIC{{Name: "primary"}, {Name: "ceph-public"}}
	got := clusterMachineInterfaceVars(state, v1alpha1.ClusterInstall{}, machine, interfaces)
	primary := got[0].(map[string]any)["networkAttachment"].(map[string]any)
	if nad := primary["kubevirt"].(map[string]any)["nad"]; nad != "child/machine" {
		t.Fatalf("primary NAD = %v, want child/machine", nad)
	}
	cephPublic := got[1].(map[string]any)["networkAttachment"].(map[string]any)
	if nad := cephPublic["kubevirt"].(map[string]any)["nad"]; nad != "child/ceph-public" {
		t.Fatalf("ceph-public NAD = %v, want child/ceph-public", nad)
	}
}

func TestKubeVirtBondMembersRenderDistinctNetworkAttachments(t *testing.T) {
	state := v1alpha1.State{
		NetworkConfigs: []v1alpha1.NetworkConfig{{
			Metadata: v1alpha1.Metadata{Name: "bonded"},
			Spec: v1alpha1.NetworkConfigSpec{Template: v1alpha1.NetworkConfigTemplate{NetworkConfig: map[string]any{
				"interfaces": []any{map[string]any{
					"name": "bond0",
					"type": "bond",
					"link-aggregation": map[string]any{
						"port": []any{"primary", "ceph-public"},
					},
				}},
			}}},
		}},
		InfraProviders: []v1alpha1.InfraProvider{{
			Metadata: v1alpha1.Metadata{Name: "kubevirt"},
			Spec: v1alpha1.InfraProviderSpec{
				Type: v1alpha1.ProvisionerKubeVirt,
				KubeVirt: &v1alpha1.InfraProviderKubeVirt{
					MachineProfiles: []v1alpha1.MachineProfile{{Name: "bonded"}},
				},
				NetworkAttachments: []v1alpha1.NetworkAttachmentCapability{
					{Name: "machine", KubeVirt: &v1alpha1.NetworkAttachmentKubeVirt{NetworkRef: v1alpha1.KubeVirtNetworkRef{Name: "machine", Namespace: "child"}}},
					{Name: "ceph-public", KubeVirt: &v1alpha1.NetworkAttachmentKubeVirt{NetworkRef: v1alpha1.KubeVirtNetworkRef{Name: "ceph-public", Namespace: "child"}}},
				},
			},
		}},
	}
	machine := v1alpha1.InstallMachine{
		Name: "node",
		Source: v1alpha1.InstallMachineSource{
			ProviderRef: v1alpha1.LocalObjectReference{Name: "kubevirt"},
			ProfileRef:  v1alpha1.LocalObjectReference{Name: "bonded"},
		},
		Network: v1alpha1.MachineNetworkConfig{
			NetworkConfigRef: v1alpha1.LocalObjectReference{Name: "bonded"},
			InterfaceAttachments: []v1alpha1.MachineInterfaceAttachment{
				{Interface: "primary", AttachmentRef: v1alpha1.LocalObjectReference{Name: "machine"}},
				{Interface: "ceph-public", AttachmentRef: v1alpha1.LocalObjectReference{Name: "ceph-public"}},
			},
		},
	}

	component := machineComponentVars(state, v1alpha1.ClusterInstall{}, machine, "child", PathOptions{})
	interfaces := component["interfaces"].([]any)
	if len(interfaces) != 2 {
		t.Fatalf("bond member interfaces = %v, want primary and ceph-public", interfaces)
	}
	want := map[string]string{
		"primary":     "child/machine",
		"ceph-public": "child/ceph-public",
	}
	for _, raw := range interfaces {
		entry := raw.(map[string]any)
		name := entry["name"].(string)
		attachment := entry["networkAttachment"].(map[string]any)
		if got := attachment["kubevirt"].(map[string]any)["nad"]; got != want[name] {
			t.Fatalf("bond member %s NAD = %v, want %s", name, got, want[name])
		}
	}
}

func TestKubeVirtSingularAttachmentProjectsToEveryInterface(t *testing.T) {
	state := v1alpha1.State{InfraProviders: []v1alpha1.InfraProvider{{
		Metadata: v1alpha1.Metadata{Name: "kubevirt"},
		Spec: v1alpha1.InfraProviderSpec{
			Type: v1alpha1.ProvisionerKubeVirt,
			NetworkAttachments: []v1alpha1.NetworkAttachmentCapability{{
				Name: "machine",
				KubeVirt: &v1alpha1.NetworkAttachmentKubeVirt{NetworkRef: v1alpha1.KubeVirtNetworkRef{
					Name: "machine", Namespace: "child",
				}},
			}},
		},
	}}}
	machine := v1alpha1.InstallMachine{
		Name:    "node",
		Source:  v1alpha1.InstallMachineSource{ProviderRef: v1alpha1.LocalObjectReference{Name: "kubevirt"}},
		Network: v1alpha1.MachineNetworkConfig{NetworkConfigRef: v1alpha1.LocalObjectReference{Name: "child"}},
	}
	ci := v1alpha1.ClusterInstall{NetworkBindings: []v1alpha1.MachineNetworkBinding{{
		MachineName: "node", ProviderRef: v1alpha1.LocalObjectReference{Name: "kubevirt"}, AttachmentRef: v1alpha1.LocalObjectReference{Name: "machine"},
	}}}
	interfaces := []v1alpha1.MachineNIC{{Name: "primary"}, {Name: "secondary"}}
	got := clusterMachineInterfaceVars(state, ci, machine, interfaces)
	for i, raw := range got {
		attachment := raw.(map[string]any)["networkAttachment"].(map[string]any)
		if nad := attachment["kubevirt"].(map[string]any)["nad"]; nad != "child/machine" {
			t.Fatalf("interface %d NAD = %v, want child/machine", i, nad)
		}
	}
}
