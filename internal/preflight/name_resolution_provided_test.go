package preflight

import (
	"errors"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func providedStorageNodeState(nameResolution []v1alpha1.EnvironmentNameResolutionComponent) v1alpha1.State {
	yes := true
	return v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata: v1alpha1.Metadata{Name: "env"},
			Spec: v1alpha1.EnvironmentSpec{
				InfraComponents: v1alpha1.EnvironmentInfraComponentsSpec{NameResolution: nameResolution},
			},
		}},
		Machines: []v1alpha1.Machine{{
			Metadata: v1alpha1.Metadata{Name: "arbiter"},
			Spec: v1alpha1.MachineSpec{
				OS: v1alpha1.MachineOSSpec{Provided: &yes},
				Addresses: []v1alpha1.MachineAddress{
					{Name: v1alpha1.MachineAddressFQDN, Address: "arbiter.example.test"},
					{Name: "ssh", Address: "10.22.254.5"},
				},
				Access: v1alpha1.MachineAccess{
					SSH: &v1alpha1.MachineSSHSpec{AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"}},
				},
			},
		}},
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: "ceph-01"},
			Spec: v1alpha1.StorageClusterSpec{
				Ceph: &v1alpha1.StorageClusterCephSpec{
					Topology: v1alpha1.StorageCephTopology{
						Nodes: []v1alpha1.StorageCephNode{{
							Name:       "node-07.ceph-01.example.test",
							MachineRef: v1alpha1.LocalObjectReference{Name: "arbiter"},
						}},
					},
				},
			},
		}},
	}
}

func TestNameResolutionChecksCoverProvidedStorageNode(t *testing.T) {
	state := providedStorageNodeState([]v1alpha1.EnvironmentNameResolutionComponent{{
		Name:       "corp-dns",
		Management: v1alpha1.EnvironmentComponentExternal,
	}})
	deps := Deps{LookupHost: func(string) ([]string, error) { return nil, errors.New("no such host") }}

	checks := nameResolutionChecks(state, []Phase{{Name: "base"}}, deps, nil)
	if len(checks) != 2 {
		t.Fatalf("checks = %+v, want the machine fqdn and the node FQDN of a provided storage node", checks)
	}
	if checks[0].Name != "Machine/arbiter fqdn" {
		t.Fatalf("checks[0].Name = %q, want the machine fqdn lookup", checks[0].Name)
	}
	if checks[1].Name != "node node-07.ceph-01.example.test" {
		t.Fatalf("checks[1].Name = %q, want the node FQDN lookup", checks[1].Name)
	}
	for i, check := range checks {
		if check.Status != StatusFail {
			t.Fatalf("checks[%d] = %+v, want FAIL; nothing publishes a record for a machine that references no resolver", i, check)
		}
	}
}

func TestNameResolutionChecksSkipProvidedNodeWithoutDeclaredResolver(t *testing.T) {
	state := providedStorageNodeState(nil)
	deps := Deps{LookupHost: func(string) ([]string, error) { return nil, errors.New("no such host") }}

	if got := nameResolutionChecks(state, []Phase{{Name: "base"}}, deps, nil); got != nil {
		t.Fatalf("checks = %+v, want none when the environment declares no name resolution that could answer", got)
	}
}

func TestNameResolutionChecksSkipProvidedMachineOutsideAnyCluster(t *testing.T) {
	state := providedStorageNodeState([]v1alpha1.EnvironmentNameResolutionComponent{{
		Name:       "corp-dns",
		Management: v1alpha1.EnvironmentComponentExternal,
	}})
	state.StorageClusters = nil
	deps := Deps{LookupHost: func(string) ([]string, error) { return nil, errors.New("no such host") }}

	if got := nameResolutionChecks(state, []Phase{{Name: "base"}}, deps, nil); got != nil {
		t.Fatalf("checks = %+v, want none for a provided machine no cluster names as a node", got)
	}
}
