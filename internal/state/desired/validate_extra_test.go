package desiredstate

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestValidateStoragePoolRoleEnum(t *testing.T) {
	clusters := map[string]v1alpha1.StorageCluster{
		"ceph": {
			Metadata: v1alpha1.Metadata{Name: "ceph"},
			Spec: v1alpha1.StorageClusterSpec{
				Type:       v1alpha1.StorageClusterTypeCeph,
				Management: v1alpha1.StorageClusterManagementManaged,
			},
		},
	}
	pool := func(role string) v1alpha1.StoragePool {
		return v1alpha1.StoragePool{
			Metadata: v1alpha1.Metadata{Name: "rbd"},
			Spec: v1alpha1.StoragePoolSpec{
				StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
				Ceph:              v1alpha1.StoragePoolCephSpec{Type: v1alpha1.StoragePoolTypeReplicated, Role: role},
			},
		}
	}

	errs := validateStoragePools([]v1alpha1.StoragePool{pool("bogus")}, clusters, nil)
	if !containsSubstring(errs, `ceph.role "bogus"`) {
		t.Fatalf("expected ceph.role enum error, got %v", errs)
	}

	for _, ok := range []string{"", v1alpha1.StoragePoolRoleRBD, v1alpha1.StoragePoolRoleCephFSData} {
		if containsSubstring(validateStoragePools([]v1alpha1.StoragePool{pool(ok)}, clusters, nil), "ceph.role") {
			t.Fatalf("role %q should be accepted, got a ceph.role error", ok)
		}
	}
}

func TestMachineNetworkOverrideShapeErrors(t *testing.T) {
	template := map[string]any{
		"interfaces": []any{
			map[string]any{"name": "primary", "ipv4": map[string]any{"enabled": true}},
		},
	}

	// Named-interface override merged by name, with a positional address list:
	// both are mergeable, so no error.
	named := map[string]any{
		"interfaces": []any{
			map[string]any{"name": "primary", "ipv4": map[string]any{
				"address": []any{map[string]any{"ip": "192.0.2.20", "prefix-length": 24}},
			}},
		},
	}
	if errs := machineNetworkOverrideShapeErrors("spec.network.config.overrides", template, named); len(errs) != 0 {
		t.Fatalf("named override should be mergeable, got %v", errs)
	}

	// Heterogeneous interfaces list (a scalar mixed with maps) cannot be merged.
	hetero := map[string]any{
		"interfaces": []any{
			"primary",
			map[string]any{"name": "secondary"},
		},
	}
	errs := machineNetworkOverrideShapeErrors("spec.network.config.overrides", template, hetero)
	if !containsSubstring(errs, "spec.network.config.overrides.interfaces override list cannot be merged") {
		t.Fatalf("expected heterogeneous override error, got %v", errs)
	}
}

func containsSubstring(errs []string, want string) bool {
	for _, e := range errs {
		if strings.Contains(e, want) {
			return true
		}
	}
	return false
}
