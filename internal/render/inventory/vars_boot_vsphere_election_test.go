package inventory

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func vsphereElectionState() v1alpha1.State {
	return v1alpha1.State{
		InfraProviders: []v1alpha1.InfraProvider{{
			Metadata: v1alpha1.Metadata{Name: "lab-vsphere"},
			Spec: v1alpha1.InfraProviderSpec{
				Type: v1alpha1.ProvisionerVSphere,
				VSphere: &v1alpha1.InfraProviderVSphere{
					VCenters: []v1alpha1.VSphereVCenter{{
						Server:      "vcenter.example.test",
						Datacenters: []string{"dc1"},
					}},
					FailureDomains: []v1alpha1.VSphereFailureDomain{
						{
							Name:     "zone-a",
							Server:   "vcenter.example.test",
							Topology: v1alpha1.VSphereFailureTopology{Datacenter: "dc1", Datastore: "/dc1/datastore/ds-a"},
						},
						{
							Name:     "zone-b",
							Server:   "vcenter.example.test",
							Topology: v1alpha1.VSphereFailureTopology{Datacenter: "dc1", Datastore: "/dc1/datastore/ds-b"},
						},
					},
					MachineProfiles: []v1alpha1.MachineProfile{
						{Name: "node-a", FailureDomainRef: v1alpha1.LocalObjectReference{Name: "zone-a"}},
						{Name: "node-b", FailureDomainRef: v1alpha1.LocalObjectReference{Name: "zone-b"}},
					},
				},
			},
		}},
	}
}

func vsphereElectionInstall(profiles map[string]string) v1alpha1.ClusterInstall {
	var machines []v1alpha1.InstallMachine
	for _, name := range []string{"master-0", "master-1", "master-2"} {
		profile, ok := profiles[name]
		if !ok {
			continue
		}
		machines = append(machines, v1alpha1.InstallMachine{
			Name: name,
			Source: v1alpha1.InstallMachineSource{
				ProviderRef: v1alpha1.LocalObjectReference{Name: "lab-vsphere"},
				ProfileRef:  v1alpha1.LocalObjectReference{Name: profile},
			},
		})
	}
	return v1alpha1.ClusterInstall{Machines: machines}
}

func vsphereAgentISO(t *testing.T, state v1alpha1.State, ci v1alpha1.ClusterInstall, machineName string) map[string]any {
	t.Helper()
	for _, m := range ci.Machines {
		if m.Name != machineName {
			continue
		}
		boot := machineBootVars(state, ci, m, "ocp")
		if boot == nil {
			t.Fatalf("machineBootVars(%s) = nil", machineName)
		}
		iso, ok := boot["agentIso"].(map[string]any)
		if !ok {
			t.Fatalf("machine %s boot vars missing agentIso: %v", machineName, boot)
		}
		return iso
	}
	t.Fatalf("machine %s is not part of the install", machineName)
	return nil
}

func TestVSphereAgentISOElectsOneStagerAndOneUploaderPerDatastore(t *testing.T) {
	state := vsphereElectionState()
	ci := vsphereElectionInstall(map[string]string{"master-0": "node-a", "master-1": "node-a", "master-2": "node-b"})

	first := vsphereAgentISO(t, state, ci, "master-0")
	second := vsphereAgentISO(t, state, ci, "master-1")
	third := vsphereAgentISO(t, state, ci, "master-2")

	if first["stagePath"] != second["stagePath"] || first["stagePath"] != third["stagePath"] {
		t.Fatalf("vsphere agent ISO stage paths diverged: %v %v %v", first["stagePath"], second["stagePath"], third["stagePath"])
	}
	if first["fetchUrl"] != second["fetchUrl"] {
		t.Fatalf("machines in one failure domain must share one datastore target: %v vs %v", first["fetchUrl"], second["fetchUrl"])
	}
	if first["fetchUrl"] == third["fetchUrl"] {
		t.Fatalf("machines in different failure domains must upload to different datastores: %v", third["fetchUrl"])
	}
	if first["stageElected"] != true {
		t.Fatalf("master-0 stageElected = %v, want true", first["stageElected"])
	}
	if second["stageElected"] != false || third["stageElected"] != false {
		t.Fatalf("only the alphabetically first machine stages: master-1=%v master-2=%v", second["stageElected"], third["stageElected"])
	}
	if first["uploadElected"] != true || third["uploadElected"] != true {
		t.Fatalf("each datastore needs one uploader: master-0=%v master-2=%v", first["uploadElected"], third["uploadElected"])
	}
	if second["uploadElected"] != false {
		t.Fatalf("master-1 shares master-0's datastore target so uploadElected = %v, want false", second["uploadElected"])
	}
}

func TestVSphereAgentISOElectionIsDeterministicRegardlessOfMachineOrder(t *testing.T) {
	state := vsphereElectionState()
	ci := vsphereElectionInstall(map[string]string{"master-0": "node-a", "master-1": "node-a"})
	reversed := v1alpha1.ClusterInstall{Machines: []v1alpha1.InstallMachine{ci.Machines[1], ci.Machines[0]}}

	if got := vsphereAgentISO(t, state, reversed, "master-0"); got["uploadElected"] != true || got["stageElected"] != true {
		t.Fatalf("master-0 must stay elected when listed last: %v", got)
	}
	if got := vsphereAgentISO(t, state, reversed, "master-1"); got["uploadElected"] != false || got["stageElected"] != false {
		t.Fatalf("master-1 must stay a non-electee when listed first: %v", got)
	}
}

func TestVSphereSingleMachineClusterElectsItself(t *testing.T) {
	state := vsphereElectionState()
	ci := vsphereElectionInstall(map[string]string{"master-0": "node-a"})
	iso := vsphereAgentISO(t, state, ci, "master-0")
	if iso["stageElected"] != true || iso["uploadElected"] != true {
		t.Fatalf("a lone vsphere machine must stage and upload its own media: %v", iso)
	}
}

func TestVSphereMachineOwnedISOAlwaysElected(t *testing.T) {
	state := vsphereElectionState()
	ci := vsphereElectionInstall(map[string]string{"master-0": "node-a", "master-1": "node-a"})
	for _, m := range ci.Machines {
		boot := machineBootVarsWithISO(state, ci, m, "ceph", "os-ceph-"+m.Name+".iso", v1alpha1.ArtifactServerEndpointRef{})
		iso, ok := boot["agentIso"].(map[string]any)
		if !ok {
			t.Fatalf("machine %s managed-OS boot vars missing agentIso: %v", m.Name, boot)
		}
		if iso["stageElected"] != true || iso["uploadElected"] != true {
			t.Fatalf("per-machine install media must not be deduped for %s: %v", m.Name, iso)
		}
	}
}
