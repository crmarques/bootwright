package ceph

import (
	"slices"
	"testing"
)

func TestEveryCephFamilyClaimsExactlyItsTableEntry(t *testing.T) {
	serialized := []string{"ceph-mgr/example", "ceph/example"}
	restarting := []string{"ceph-mgr/example"}
	objectRoot := []string{"ceph-rgw/example"}
	unit := func(kind string) Request {
		return Request{Cluster: "example", Service: &ServiceUnit{Type: kind}}
	}
	for _, test := range []struct {
		family  string
		request Request
		claim   []string
	}{
		{"cluster", Request{Cluster: "example"}, serialized},
		{"nvmeof", Request{Cluster: "example"}, serialized},
		{"consumer", Request{Cluster: "example"}, serialized},
		{"replication", Request{Cluster: "example"}, serialized},
		{"service", unit("nvmeof"), serialized},
		{"service", unit("rbd-mirror"), serialized},
		{"settings", Request{Cluster: "example"}, restarting},
		{"service", unit("monitoring"), restarting},
		{"gateway", Request{Cluster: "example"}, objectRoot},
		{"nfs-export", Request{Cluster: "example", NFSExport: &NFSExport{ObjectUser: &NFSExportObjectUser{}}}, objectRoot},
		{"nfs-export", Request{Cluster: "example", NFSExport: &NFSExport{}}, nil},
		{"service", unit("crash"), nil},
		{"service", unit("nfs"), nil},
		{"service", unit("mgmt-gateway"), nil},
		{"smb", Request{Cluster: "example"}, nil},
		{"policy", Request{Cluster: "example"}, nil},
		{"pool", Request{Cluster: "example"}, nil},
		{"client", Request{Cluster: "example"}, nil},
		{"rbd", Request{Cluster: "example"}, nil},
		{"filesystem", Request{Cluster: "example"}, nil},
		{"subvolume", Request{Cluster: "example"}, nil},
		{"object-user", Request{Cluster: "example"}, nil},
		{"object-bucket", Request{Cluster: "example"}, nil},
	} {
		got := exclusiveFor(test.family, test.request)
		if !slices.Equal(got, test.claim) || !slices.IsSorted(got) {
			t.Errorf("%s %+v claims %v, want %v", test.family, test.request.Service, got, test.claim)
		}
	}
	if ThrottleKey("example") != "ceph/example" {
		t.Fatalf("throttle key %q", ThrottleKey("example"))
	}
}
