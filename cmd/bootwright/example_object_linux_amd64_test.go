//go:build linux && amd64

package main

import (
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/storage/ceph"
)

const objectExample = "libvirt-3-nodes-ceph-object"

// TestExampleObjectPlansTheGatewayBlocks plans the object gateway example
// through the production capabilities: no capability refuses it, and the
// gateway capability plans exactly the eight derived zone pool blocks and the
// gateway block, which depends on each of them.
func TestExampleObjectPlansTheGatewayBlocks(t *testing.T) {
	state, _ := compileAcceptance(t, exampleDirectory(t, objectExample))
	if refused := productionUnsupported(t, state); len(refused) != 0 {
		t.Fatalf("the capabilities refuse %+v", refused)
	}
	definitions := productionDefinitions(t, state, objectExample)
	if _, err := reconciliation.NewPlan(reconciliation.Apply, definitions); err != nil {
		t.Fatal(err)
	}
	var ids []string
	var gateway reconciliation.BlockDefinition
	for _, definition := range definitions {
		if definition.Implementation == ceph.Implementation && definition.Kind == string(api.StorageObjectGateway) {
			ids = append(ids, definition.ID)
			if definition.ID == ceph.GatewayBlockID("objects") {
				gateway = definition
			}
		}
	}
	slices.Sort(ids)
	pools := []string{
		"ceph-gateway-objects-pool-buckets-data", "ceph-gateway-objects-pool-buckets-index", "ceph-gateway-objects-pool-buckets-non-ec",
		"ceph-gateway-objects-pool-control", "ceph-gateway-objects-pool-dedup", "ceph-gateway-objects-pool-log", "ceph-gateway-objects-pool-meta",
		"ceph-gateway-objects-pool-otp",
	}
	if want := append([]string{"ceph-gateway-objects"}, pools...); !slices.Equal(ids, want) {
		t.Fatalf("the gateway capability plans %q, want %q", ids, want)
	}
	for _, pool := range pools {
		if !slices.Contains(gateway.Dependencies, pool) {
			t.Errorf("the gateway block depends on %q, want it to include %q", gateway.Dependencies, pool)
		}
	}
}
