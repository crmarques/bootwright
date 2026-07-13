package workflow

import (
	"reflect"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

// structuralFieldClass records, for every v1alpha1.State field, how a change to
// that kind may affect a cluster's structural (destructive-rebuild) identity:
//
//	identity       - the cluster's own install/Ceph identity; a change may rebuild.
//	subobject      - a storage sub-object classified against its own structural hash.
//	day2           - reconfigure-only intent excluded from the install structural hash.
//	excluded       - fleet-scoped/reference input already cleared from the storage
//	                 structural projection, so editing it never forces a wipe.
//	excludePending - fleet-scoped input that STILL leaks into the storage/install
//	                 structural hash (2026-07-12 lifecycle review H4); clearing it is
//	                 gated on the convergence-record re-stamp migration.
//
// A new State field trips TestStateFieldsAllStructurallyClassified until it is
// classified here, so a new kind cannot silently leak into the "would wipe" hash
// the way the excludePending kinds did.
var structuralFieldClass = map[string]string{
	"Environments":             "excluded",
	"Entitlements":             "excludePending",
	"Machines":                 "excluded",
	"MachineImages":            "excludePending",
	"MachineInstallProfiles":   "excludePending",
	"NetworkConfigs":           "excludePending",
	"InfraProviders":           "excluded",
	"InfraComponents":          "excluded",
	"ContainerClusters":        "identity",
	"StorageClusters":          "identity",
	"StoragePlacementPolicies": "subobject",
	"StoragePools":             "subobject",
	"StorageFilesystems":       "subobject",
	"StorageObjectGateways":    "subobject",
	"StorageNFSExports":        "subobject",
	"StorageExports":           "subobject",
	"ClusterAddons":            "day2",
	"ClusterAddonProfiles":     "day2",
	"ClusterAddonBindings":     "day2",
	"ProvisioningPlaybooks":    "excludePending",
	"Secrets":                  "excluded",
}

func TestStateFieldsAllStructurallyClassified(t *testing.T) {
	typ := reflect.TypeOf(v1alpha1.State{})
	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		if _, ok := structuralFieldClass[name]; !ok {
			t.Fatalf("v1alpha1.State field %q is not classified for the structural-hash projection; add it to structuralFieldClass as identity, subobject, day2, excluded, or excludePending so a new kind cannot silently leak into the destructive-rebuild hash (2026-07-12 lifecycle review H4)", name)
		}
	}
}

func TestStorageStructuralProjectionClearsExcludedKinds(t *testing.T) {
	state := v1alpha1.State{
		StorageClusters: []v1alpha1.StorageCluster{{Metadata: v1alpha1.Metadata{Name: "ceph"}}},
		Environments:    []v1alpha1.Environment{{Metadata: v1alpha1.Metadata{Name: "env"}}},
		Machines:        []v1alpha1.Machine{{Metadata: v1alpha1.Metadata{Name: "node"}}},
		InfraProviders:  []v1alpha1.InfraProvider{{Metadata: v1alpha1.Metadata{Name: "prov"}}},
		InfraComponents: []v1alpha1.InfraComponent{{Metadata: v1alpha1.Metadata{Name: "svc"}}},
		Secrets:         []v1alpha1.Secret{{Metadata: v1alpha1.Metadata{Name: "sec"}}},
	}
	projected := storageClusterStructuralHashVars(state, "ceph")
	for field, value := range map[string]int{
		"Environments":    len(projected.Environments),
		"Machines":        len(projected.Machines),
		"InfraProviders":  len(projected.InfraProviders),
		"InfraComponents": len(projected.InfraComponents),
		"Secrets":         len(projected.Secrets),
	} {
		if structuralFieldClass[field] != "excluded" {
			t.Fatalf("field %q is asserted cleared but not classified excluded", field)
		}
		if value != 0 {
			t.Fatalf("storage structural projection must clear excluded kind %q, but it survived (%d entries)", field, value)
		}
	}
}
