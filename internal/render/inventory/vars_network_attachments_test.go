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
