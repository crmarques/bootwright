package workflow

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestHashScopedStateDropsUnreferencedFleetKinds(t *testing.T) {
	state := v1alpha1.State{
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: "ceph"},
			Spec: v1alpha1.StorageClusterSpec{
				Type: "ceph",
				Ceph: &v1alpha1.StorageClusterCephSpec{
					EntitlementRef: v1alpha1.LocalObjectReference{Name: "used-entitlement"},
				},
			},
		}},
		Entitlements: []v1alpha1.Entitlement{
			{Metadata: v1alpha1.Metadata{Name: "used-entitlement"}},
			{Metadata: v1alpha1.Metadata{Name: "someone-elses-entitlement"}},
		},
		Secrets: []v1alpha1.Secret{
			{Metadata: v1alpha1.Metadata{Name: "someone-elses-secret"}},
		},
	}

	scoped := hashScopedState(state)
	if len(scoped.Entitlements) != 1 || scoped.Entitlements[0].Metadata.Name != "used-entitlement" {
		t.Fatalf("referenced entitlement must survive, unrelated one must be pruned: %+v", scoped.Entitlements)
	}
	if len(scoped.Secrets) != 0 {
		t.Fatalf("an unreferenced secret must be pruned from the hash input: %+v", scoped.Secrets)
	}
}

func TestHashScopedStateKeepsTransitivelyReferencedSecret(t *testing.T) {
	state := v1alpha1.State{
		Entitlements: []v1alpha1.Entitlement{{
			Metadata: v1alpha1.Metadata{Name: "used-entitlement"},
			Spec: v1alpha1.EntitlementSpec{
				RHSM: &v1alpha1.EntitlementRHSM{OrganizationRef: v1alpha1.SecretRef{Name: "used-secret"}},
			},
		}},
		Secrets: []v1alpha1.Secret{
			{Metadata: v1alpha1.Metadata{Name: "used-secret"}},
			{Metadata: v1alpha1.Metadata{Name: "orphan-secret"}},
		},
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: "ceph"},
			Spec: v1alpha1.StorageClusterSpec{
				Type: "ceph",
				Ceph: &v1alpha1.StorageClusterCephSpec{
					EntitlementRef: v1alpha1.LocalObjectReference{Name: "used-entitlement"},
				},
			},
		}},
	}

	scoped := hashScopedState(state)
	names := map[string]bool{}
	for _, s := range scoped.Secrets {
		names[s.Metadata.Name] = true
	}
	if !names["used-secret"] {
		t.Fatalf("a secret referenced transitively through a kept entitlement must survive: %+v", scoped.Secrets)
	}
	if names["orphan-secret"] {
		t.Fatalf("an orphan secret must be pruned: %+v", scoped.Secrets)
	}
}

func TestApplyTaskDesiredHashIgnoresUnrelatedFleetObjects(t *testing.T) {
	base := ApplyTask{
		Entry: TaskLedgerEntry{ID: "prov.p1", Kind: ApplyTaskKindProvider},
		State: v1alpha1.State{
			InfraProviders: []v1alpha1.InfraProvider{{Metadata: v1alpha1.Metadata{Name: "p1"}}},
		},
	}
	withUnrelated := base
	withUnrelated.State = v1alpha1.State{
		InfraProviders: []v1alpha1.InfraProvider{{Metadata: v1alpha1.Metadata{Name: "p1"}}},
		Secrets:        []v1alpha1.Secret{{Metadata: v1alpha1.Metadata{Name: "an-unrelated-secret"}}},
		MachineImages:  []v1alpha1.MachineImage{{Metadata: v1alpha1.Metadata{Name: "an-unrelated-image"}}},
	}

	a, err := ApplyTaskDesiredHash(base)
	if err != nil {
		t.Fatalf("hash base: %v", err)
	}
	b, err := ApplyTaskDesiredHash(withUnrelated)
	if err != nil {
		t.Fatalf("hash with unrelated: %v", err)
	}
	if a != b {
		t.Fatalf("adding unrelated fleet objects must not move a task's desired hash:\n base=%s\n with=%s", a, b)
	}
}
