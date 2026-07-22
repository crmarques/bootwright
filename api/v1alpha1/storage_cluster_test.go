package v1alpha1

import "testing"

func ossClusterWithProfileSubscription(management string) (StorageCluster, State) {
	rhsm := &EntitlementRHSM{Management: management}
	state := State{
		Entitlements: []Entitlement{{
			Metadata: Metadata{Name: "rhel-satellite"},
			Spec:     EntitlementSpec{Type: EntitlementTypeRedHatRHEL, RHSM: rhsm},
		}},
		MachineInstallProfiles: []MachineInstallProfile{{
			Metadata: Metadata{Name: "rhel-9-ceph-node"},
			Spec: MachineInstallProfileSpec{
				Subscription: &MachineOSSubscription{EntitlementRef: LocalObjectReference{Name: "rhel-satellite"}},
			},
		}},
		Machines: []Machine{{
			Metadata: Metadata{Name: "node-01"},
			Spec:     MachineSpec{OS: MachineOSSpec{InstallProfileRef: LocalObjectReference{Name: "rhel-9-ceph-node"}}},
		}},
	}
	cluster := StorageCluster{
		Spec: StorageClusterSpec{
			Type: "ceph",
			Ceph: &StorageClusterCephSpec{
				Distribution: StorageCephDistributionOSS,
				Topology:     StorageCephTopology{Nodes: []StorageCephNode{{MachineRef: LocalObjectReference{Name: "node-01"}}}},
			},
		},
	}
	return cluster, state
}

func TestStorageClusterManagedRegistrationFromProfileSubscription(t *testing.T) {
	cluster, state := ossClusterWithProfileSubscription(EntitlementRHSMManagementManaged)
	if !StorageClusterManagedRegistration(cluster, state) {
		t.Fatal("expected managed registration for OSS cluster whose node profile carries a managed redhat-rhel subscription")
	}
	ent, ok := StorageClusterOSSubscriptionEntitlement(cluster, state)
	if !ok || ent.Metadata.Name != "rhel-satellite" {
		t.Fatalf("OS subscription entitlement = %q, ok=%v", ent.Metadata.Name, ok)
	}
}

func TestStorageClusterManagedRegistrationSkipsExternalSubscription(t *testing.T) {
	cluster, state := ossClusterWithProfileSubscription(EntitlementRHSMManagementExternal)
	if StorageClusterManagedRegistration(cluster, state) {
		t.Fatal("external subscription must not plan a managed registration task")
	}
}

func TestStorageClusterOSSubscriptionRejectsNonRHELType(t *testing.T) {
	cluster, state := ossClusterWithProfileSubscription(EntitlementRHSMManagementManaged)
	state.Entitlements[0].Spec.Type = EntitlementTypeRedHatCeph
	if _, ok := StorageClusterOSSubscriptionEntitlement(cluster, state); ok {
		t.Fatal("OS subscription entitlement must be redhat-rhel")
	}
}

func TestStorageClusterManagedRegistrationEmptyWithoutSubscription(t *testing.T) {
	cluster, state := ossClusterWithProfileSubscription(EntitlementRHSMManagementManaged)
	state.MachineInstallProfiles[0].Spec.Subscription = nil
	if StorageClusterManagedRegistration(cluster, state) {
		t.Fatal("OSS cluster without a profile subscription must not plan registration")
	}
}
