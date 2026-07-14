package stategraph

import (
	"reflect"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func storageConsumerState() v1alpha1.State {
	return v1alpha1.State{
		StorageExports: []v1alpha1.StorageExport{{
			Metadata: v1alpha1.Metadata{Name: "ceph-export"},
			Spec:     v1alpha1.StorageExportSpec{StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"}},
		}},
		ClusterAddons: []v1alpha1.ClusterAddon{{
			Metadata: v1alpha1.Metadata{Name: "odf-consumer"},
			Spec: v1alpha1.ClusterAddonSpec{
				Accepts: v1alpha1.ClusterAddonAccepts{Inputs: []v1alpha1.ClusterAddonAcceptedInput{{
					Name:        "storage",
					ResourceRef: &v1alpha1.ClusterAddonInputRef{Kind: v1alpha1.KindStorageExport},
					Effects: []v1alpha1.ClusterAddonInputEffect{{
						StorageExportAttachment: &v1alpha1.ClusterAddonStorageExportAttachmentEffect{},
					}},
				}}},
			},
		}},
		ClusterAddonBindings: []v1alpha1.ClusterAddonBinding{{
			Spec: v1alpha1.ClusterAddonBindingSpec{
				ClusterRef: v1alpha1.LocalObjectReference{Name: "ocp"},
				AddonRefs:  []v1alpha1.LocalObjectReference{{Name: "odf-consumer"}},
				AddonConfigs: []v1alpha1.ClusterAddonBindingAddonConfig{{
					AddonRef: v1alpha1.LocalObjectReference{Name: "odf-consumer"},
					Inputs: []v1alpha1.ClusterAddonBindingInput{{
						Name:  "storage",
						Value: "ceph-export",
					}},
				}},
			},
		}},
	}
}

func TestStorageConsumerDestroyConflictsRefusesLiveConsumer(t *testing.T) {
	conflicts := StorageConsumerDestroyConflicts(storageConsumerState(), []string{"ceph"}, nil)
	want := []StorageConsumerConflict{{StorageCluster: "ceph", ConsumingClusters: []string{"ocp"}}}
	if !reflect.DeepEqual(conflicts, want) {
		t.Fatalf("destroying ceph while ocp consumes it must conflict, got %+v", conflicts)
	}
}

func TestStorageConsumerDestroyConflictsAllowsCoScopedConsumer(t *testing.T) {
	if conflicts := StorageConsumerDestroyConflicts(storageConsumerState(), []string{"ceph"}, []string{"ocp"}); len(conflicts) != 0 {
		t.Fatalf("destroying ceph together with its consumer ocp must not conflict, got %+v", conflicts)
	}
}

func TestStorageConsumerDestroyConflictsIgnoresUnselectedStorage(t *testing.T) {
	if conflicts := StorageConsumerDestroyConflicts(storageConsumerState(), []string{"other"}, nil); len(conflicts) != 0 {
		t.Fatalf("a storage cluster not in scope must not conflict, got %+v", conflicts)
	}
}
