//go:build linux && amd64

package main

import (
	"context"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/storage/ceph"
)

func TestClusterInfoResourcesRows(t *testing.T) {
	state, _ := compileAcceptance(t, exampleDirectory(t, blockFileExample))
	resolver := buildCapabilities(systemClock{}, exampleControllerPorts(t), exampleMediaRecords{})
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, State: state, Controller: "controller", Context: lifecycle.ContextIdentity{Name: blockFileExample}}
	var definitions []reconciliation.BlockDefinition
	for _, binding := range resolver.Bindings() {
		capability, ok := resolver.Resolve(binding.Kind, binding.Implementation)
		if !ok {
			t.Fatal("unresolved production capability", binding)
		}
		contribution, err := capability.Plan(context.Background(), input)
		if err != nil {
			t.Fatalf("%v: %+v", binding, diagnostics.Of(err))
		}
		definitions = append(definitions, contribution.Definitions...)
	}
	plan, err := reconciliation.NewPlan(reconciliation.Apply, definitions)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		kind  api.Kind
		name  string
		scope string
	}{
		{api.CephPlacementPolicy, "ssd-hosts", "rule ssd-hosts, failure domain host, device class ssd"},
		{api.CephPool, "cephfs-data", "replicated size 2/min 2, application cephfs"},
		{api.CephPool, "cephfs-metadata", "replicated size 2/min 2, application cephfs"},
		{api.CephPool, "rbd-data", "replicated size 2/min 2, application rbd"},
		{api.CephRBDImage, "rbd-disk", "image rbd-data/rbd-disk, 1073741824 bytes"},
		{api.CephClient, "rbd-canary", "client.rbd-canary read-write on pool rbd-data"},
		{api.CephFilesystem, "files", "filesystem files, metadata cephfs-metadata, data cephfs-data"},
		{api.CephSubvolume, "docs", "subvolume files/apps/docs"},
	} {
		if got := ceph.ResourceScope(state.Effective(), row.kind, row.name, plan.Blocks); got != row.scope {
			t.Errorf("%s/%s scope = %q, want %q", row.kind, row.name, got, row.scope)
		}
	}
}
