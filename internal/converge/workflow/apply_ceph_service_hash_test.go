package workflow

import (
	"encoding/json"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestStatelessCephServicesDeclareStructuralProjection(t *testing.T) {
	for _, kind := range []string{storageSubObjectKindGateway, storageSubObjectKindNFSExport, storageSubObjectKindExport} {
		sub := storageSubObject{Kind: kind, Cluster: "ceph", Name: "svc"}
		if storageSubObjectStructuralSpec(v1alpha1.State{}, sub) == nil {
			t.Fatalf("%s must declare a structural projection so config edits reconcile in place", kind)
		}
		h, err := storageSubObjectStructuralHash(v1alpha1.State{}, sub)
		if err != nil {
			t.Fatalf("%s structural hash: %v", kind, err)
		}
		if h == "" {
			t.Fatalf("%s structural hash must be non-empty (empty forces structural/fail-safe)", kind)
		}
	}
}

func TestCephManagementEditIsStructurallyReconcilable(t *testing.T) {
	proj := func(s v1alpha1.State) string {
		b, err := json.Marshal(storageClusterStructuralHashVars(s, "ceph-bm"))
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return string(b)
	}
	base := bareMetalManagedOSState()
	want := proj(base)

	withMgmt := bareMetalManagedOSState()
	withMgmt.StorageClusters[0].Spec.Ceph.Management = &v1alpha1.StorageCephManagement{}
	if proj(withMgmt) != want {
		t.Fatal("enabling the mgmt-gateway must not move the structural hash")
	}
	withSvc := bareMetalManagedOSState()
	withSvc.StorageClusters[0].Spec.Ceph.Services = []v1alpha1.StorageCephService{{}}
	if proj(withSvc) != want {
		t.Fatal("a cephadm service-spec edit must not move the structural hash")
	}
}
