package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestSecretListReportsImportedCephExternalDetailsFile(t *testing.T) {
	secretPath := filepath.Join(t.TempDir(), "shared-ceph-external-cluster-details.json")
	if err := os.WriteFile(secretPath, []byte("[]\n"), 0o600); err != nil {
		t.Fatalf("write secret: %v", err)
	}
	state := importedCephSecretState(v1alpha1.SecretSource{File: &v1alpha1.SecretFileSource{Path: secretPath}})
	entries, err := declaredSecretEntriesForContext("test", t.TempDir(), state)
	if err != nil {
		t.Fatalf("declaredSecretEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %+v, want one", entries)
	}
	entry := entries[0]
	if entry.Name != "shared-ceph-external-details" || entry.Type != "file:opaque" || !entry.Present {
		t.Fatalf("secret list entry = %+v", entry)
	}
	if len(entry.Paths) != 1 || entry.Paths[0] != secretPath {
		t.Fatalf("secret list paths = %+v, want %s", entry.Paths, secretPath)
	}
}

func importedCephSecretState(source v1alpha1.SecretSource) v1alpha1.State {
	return v1alpha1.State{
		Secrets: []v1alpha1.Secret{{
			Metadata: v1alpha1.Metadata{Name: "shared-ceph-external-details"},
			Spec:     v1alpha1.SecretSpec{Type: v1alpha1.SecretTypeOpaque, Source: source},
		}},
		StorageExports: []v1alpha1.StorageExport{{
			Metadata: v1alpha1.Metadata{Name: "shared-ceph-data-foundation"},
			Spec: v1alpha1.StorageExportSpec{
				Type:              v1alpha1.StorageExportTypeDataFoundation,
				StorageClusterRef: v1alpha1.LocalObjectReference{Name: "shared-ceph"},
				ExternalDetails: &v1alpha1.StorageExportExternalDetailsSpec{
					FromSecretRef: v1alpha1.SecretRef{Name: "shared-ceph-external-details"},
				},
			},
		}},
		ClusterAddons: []v1alpha1.ClusterAddon{{
			Metadata: v1alpha1.Metadata{Name: "odf"},
			Spec: v1alpha1.ClusterAddonSpec{
				Type:     v1alpha1.ClusterAddonTypeManifestSet,
				Provides: []string{v1alpha1.ClusterAddonProvidesDataFoundation},
				Accepts:  dataFoundationAccepts(),
			},
		}},
		ClusterAddonBindings: []v1alpha1.ClusterAddonBinding{{
			Metadata: v1alpha1.Metadata{Name: "shared-ceph-binding"},
			Spec: v1alpha1.ClusterAddonBindingSpec{
				ClusterRef: v1alpha1.LocalObjectReference{Name: "demo"},
				Addons:     []v1alpha1.ClusterAddonBindingAddon{dataFoundationBindingAddon("shared-ceph-data-foundation")},
			},
		}},
	}
}

func dataFoundationAccepts() v1alpha1.ClusterAddonAccepts {
	return v1alpha1.ClusterAddonAccepts{Inputs: []v1alpha1.ClusterAddonAcceptedInput{{
		Name: "external-storage",
		Schema: v1alpha1.ClusterAddonInputSchema{
			Type:     v1alpha1.ClusterAddonInputSchemaTypeObject,
			Required: []string{"exportRef"},
			Properties: map[string]v1alpha1.ClusterAddonInputProperty{
				"exportRef": {RefKind: v1alpha1.KindStorageExport},
			},
		},
		Effects: []v1alpha1.ClusterAddonInputEffect{{
			Type:     v1alpha1.ClusterAddonInputEffectStorageExportAttachment,
			Provider: v1alpha1.ClusterAddonProvidesDataFoundation,
		}},
	}}}
}

func dataFoundationBindingAddon(export string) v1alpha1.ClusterAddonBindingAddon {
	values := map[string]any{
		"exportRef": export,
	}
	return v1alpha1.ClusterAddonBindingAddon{
		AddonRef: v1alpha1.LocalObjectReference{Name: "odf"},
		Inputs: []v1alpha1.ClusterAddonBindingInput{{
			Name:   "external-storage",
			Values: values,
		}},
	}
}
