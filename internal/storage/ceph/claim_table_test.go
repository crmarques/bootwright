package ceph

import (
	"slices"
	"testing"
)

func TestEveryCephFamilyClaimsExactlyItsTableEntry(t *testing.T) {
	orchestrator := []string{"ceph/example"}
	want := map[string][]string{
		"cluster": orchestrator, "settings": orchestrator, "service": orchestrator, "gateway": orchestrator, "nfs-export": orchestrator,
		"nvmeof": orchestrator, "smb": orchestrator, "consumer": orchestrator, "replication": orchestrator,
		"policy": nil, "pool": nil, "client": nil, "rbd": nil, "filesystem": nil, "subvolume": nil, "object-user": nil, "object-bucket": nil,
	}
	for family, claim := range want {
		if got := exclusiveFor(family, Request{Cluster: "example"}); !slices.Equal(got, claim) {
			t.Errorf("%s claims %v, want %v", family, got, claim)
		}
	}
	if ThrottleKey("example") != "ceph/example" {
		t.Fatalf("throttle key %q", ThrottleKey("example"))
	}
}
