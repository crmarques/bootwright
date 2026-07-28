package workflow

import (
	"reflect"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

var structuralFieldClass = map[string]string{
	"Environments":             "excluded",
	"Entitlements":             "excluded",
	"Machines":                 "excluded",
	"MachineImages":            "excluded",
	"MachineInstallProfiles":   "excluded",
	"NetworkConfigs":           "excluded",
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
	"CustomPlaybooks":          "excluded",
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

func TestStorageStructuralProjectionClearsReferencedInstallKinds(t *testing.T) {
	state := v1alpha1.State{
		StorageClusters:        []v1alpha1.StorageCluster{{Metadata: v1alpha1.Metadata{Name: "ceph"}}},
		Entitlements:           []v1alpha1.Entitlement{{Metadata: v1alpha1.Metadata{Name: "ceph"}}},
		MachineImages:          []v1alpha1.MachineImage{{Metadata: v1alpha1.Metadata{Name: "ceph"}}},
		MachineInstallProfiles: []v1alpha1.MachineInstallProfile{{Metadata: v1alpha1.Metadata{Name: "ceph"}}},
		NetworkConfigs:         []v1alpha1.NetworkConfig{{Metadata: v1alpha1.Metadata{Name: "ceph"}}},
		CustomPlaybooks:        []v1alpha1.CustomPlaybook{{Metadata: v1alpha1.Metadata{Name: "ceph"}}},
	}
	desired := storageClusterDesiredHashVars(state, "ceph")
	for field, value := range map[string]int{
		"Entitlements":           len(desired.Entitlements),
		"MachineImages":          len(desired.MachineImages),
		"MachineInstallProfiles": len(desired.MachineInstallProfiles),
		"NetworkConfigs":         len(desired.NetworkConfigs),
		"CustomPlaybooks":        len(desired.CustomPlaybooks),
	} {
		if value == 0 {
			t.Fatalf("desired-hash projection must keep referenced kind %q so its edits stay reconcilable drift", field)
		}
	}
	projected := storageClusterStructuralHashVars(state, "ceph")
	for field, value := range map[string]int{
		"Entitlements":           len(projected.Entitlements),
		"MachineImages":          len(projected.MachineImages),
		"MachineInstallProfiles": len(projected.MachineInstallProfiles),
		"NetworkConfigs":         len(projected.NetworkConfigs),
		"CustomPlaybooks":        len(projected.CustomPlaybooks),
	} {
		if structuralFieldClass[field] != "excluded" {
			t.Fatalf("field %q is asserted cleared but not classified excluded", field)
		}
		if value != 0 {
			t.Fatalf("storage structural projection must clear %q so its edits never classify as wipe-and-rebuild, but it survived (%d entries)", field, value)
		}
	}
}
