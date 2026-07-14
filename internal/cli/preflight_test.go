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
				AddonRefs:  []v1alpha1.LocalObjectReference{{Name: "odf"}},
				AddonConfigs: []v1alpha1.ClusterAddonBindingAddonConfig{{
					AddonRef: v1alpha1.LocalObjectReference{Name: "odf"},
					Inputs:   []v1alpha1.ClusterAddonBindingInput{dataFoundationBindingInput("shared-ceph-data-foundation")},
				}},
			},
		}},
	}
}

func dataFoundationAccepts() v1alpha1.ClusterAddonAccepts {
	return v1alpha1.ClusterAddonAccepts{Inputs: []v1alpha1.ClusterAddonAcceptedInput{{
		Name:        "external-storage",
		ResourceRef: &v1alpha1.ClusterAddonInputRef{Kind: v1alpha1.KindStorageExport},
		Effects: []v1alpha1.ClusterAddonInputEffect{{
			StorageExportAttachment: &v1alpha1.ClusterAddonStorageExportAttachmentEffect{},
		}},
	}}}
}

func dataFoundationBindingInput(export string) v1alpha1.ClusterAddonBindingInput {
	return v1alpha1.ClusterAddonBindingInput{
		Name:  "external-storage",
		Value: export,
	}
}
