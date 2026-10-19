//go:build linux && amd64

package main

import (
	"context"
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/storage/ceph"
)

const blockFileExample = "libvirt-3-nodes-ceph-block-file"

// TestExampleBlockFilePlansAllBlocks plans the block and file example through
// the production capabilities: no capability refuses it, the core Ceph
// capability plans exactly its cluster, policy, three pools, RBD image,
// filesystem, subvolume and client blocks, and each resource block requires
// the object it builds on.
func TestExampleBlockFilePlansAllBlocks(t *testing.T) {
	state, _ := compileAcceptance(t, exampleDirectory(t, blockFileExample))
	if refused := productionUnsupported(t, state); len(refused) != 0 {
		t.Fatalf("the capabilities refuse %+v", refused)
	}
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
	if _, err := reconciliation.NewPlan(reconciliation.Apply, definitions); err != nil {
		t.Fatal(err)
	}
	core := map[string]reconciliation.BlockDefinition{}
	var ids []string
	for _, definition := range definitions {
		if definition.Implementation == ceph.Implementation {
			core[definition.ID] = definition
			ids = append(ids, definition.ID)
		}
	}
	slices.Sort(ids)
	want := []string{
		"ceph-client-rbd-canary", "ceph-cluster-ceph-lab", "ceph-filesystem-files", "ceph-policy-ssd-hosts",
		"ceph-pool-cephfs-data", "ceph-pool-cephfs-metadata", "ceph-pool-rbd-data", "ceph-rbd-rbd-disk", "ceph-subvolume-docs",
	}
	if !slices.Equal(ids, want) {
		t.Fatalf("the core Ceph capability plans %q, want %q", ids, want)
	}
	requires := map[string][]reconciliation.ObjectRef{
		"ceph-pool-rbd-data":    {{Kind: string(api.StoragePlacementPolicy), Object: "ssd-hosts"}},
		"ceph-rbd-rbd-disk":     {{Kind: string(api.StoragePool), Object: "rbd-data"}},
		"ceph-filesystem-files": {{Kind: string(api.StoragePool), Object: "cephfs-metadata"}, {Kind: string(api.StoragePool), Object: "cephfs-data"}},
		"ceph-subvolume-docs":   {{Kind: string(api.StorageFilesystem), Object: "files"}},
	}
	for id, refs := range requires {
		for _, ref := range refs {
			if !slices.Contains(core[id].Requires, ref) {
				t.Errorf("%s requires %v, want it to include %v", id, core[id].Requires, ref)
			}
		}
	}
}
