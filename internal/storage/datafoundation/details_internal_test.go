package datafoundation

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

// TestExternalDetailsRGWPoolPrefixUsesServiceID covers F27: cephadm names RGW
// pools after the rgw service_id (no realm/zone is rendered), so the generated
// Data Foundation ceph-rgw poolPrefix must be the gateway's serviceID, not its
// resource name.
func TestExternalDetailsRGWPoolPrefixUsesServiceID(t *testing.T) {
	state := v1alpha1.State{
		StorageObjectGateways: []v1alpha1.StorageObjectGateway{{
			Metadata: v1alpha1.Metadata{Name: "odf-rgw"},
			Spec: v1alpha1.StorageObjectGatewaySpec{
				Ceph: v1alpha1.StorageObjectGatewayCephSpec{ServiceID: "odf"},
			},
		}},
	}
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec:     v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{}},
	}
	export := v1alpha1.StorageExport{
		Spec: v1alpha1.StorageExportSpec{
			DataFoundation: &v1alpha1.StorageExportDataFoundationSpec{
				RBDPoolRef:       v1alpha1.LocalObjectReference{Name: "odf-rbd"},
				CephFSRef:        v1alpha1.LocalObjectReference{Name: "odf-cephfs"},
				ObjectGatewayRef: v1alpha1.LocalObjectReference{Name: "odf-rgw"},
			},
		},
	}

	input := ExternalDetailsInputFromState(state, cluster, export, "dc1-ocp")
	if input.RGWPoolPrefix != "odf" {
		t.Fatalf("RGWPoolPrefix = %q, want gateway serviceID %q", input.RGWPoolPrefix, "odf")
	}
}
