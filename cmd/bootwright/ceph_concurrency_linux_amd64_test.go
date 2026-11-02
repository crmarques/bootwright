//go:build linux && amd64

package main

import (
	"context"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/storage/ceph"
)

func cephExamplePlan(t *testing.T, directory string) (reconciliation.Plan, api.Catalog) {
	t.Helper()
	state, _ := compileAcceptance(t, exampleDirectory(t, directory))
	resolver := buildCapabilities(systemClock{}, exampleControllerPorts(t), exampleMediaRecords{})
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, State: state, Controller: "controller", Context: lifecycle.ContextIdentity{Name: directory}}
	var definitions []reconciliation.BlockDefinition
	for _, binding := range resolver.Bindings() {
		capability, ok := resolver.Resolve(binding.Kind, binding.Implementation)
		if !ok {
			t.Fatal("unresolved production capability", binding)
		}
		contribution, err := capability.Plan(context.Background(), input)
		if err != nil {
			t.Fatalf("%s: %v: %+v", directory, binding, diagnostics.Of(err))
		}
		definitions = append(definitions, contribution.Definitions...)
	}
	plan, err := reconciliation.NewPlan(reconciliation.Apply, definitions)
	if err != nil {
		t.Fatal(err)
	}
	return plan, state.Effective()
}

func cephChild(block reconciliation.Block, cluster string) bool {
	if block.Stage != reconciliation.StageClusters || block.ID == ceph.ClusterBlockID(cluster) || block.ID == ceph.ImageBlockID(cluster) {
		return false
	}
	return block.Throttle == ceph.ThrottleKey(cluster)
}

func cephExpectedClaims(block reconciliation.Block, cluster string) []string {
	serialized := []string{"ceph-mgr/" + cluster, "ceph/" + cluster}
	if block.Kind == string(api.CephConsumer) || block.Kind == string(api.CephNVMeoFSubsystem) {
		return serialized
	}
	if block.Kind == string(api.CephSMBResource) {
		return nil
	}
	request, err := ceph.DecodeRequest(block.Request)
	if err != nil {
		return []string{"undecodable"}
	}
	switch {
	case request.Action == "settings", request.Service != nil && request.Service.Type == "monitoring":
		return []string{"ceph-mgr/" + cluster}
	case request.Service != nil && (request.Service.Type == "nvmeof" || request.Service.Type == "rbd-mirror"):
		return serialized
	case request.Action == "gateway", request.NFSExport != nil && request.NFSExport.ObjectUser != nil:
		return []string{"ceph-rgw/" + cluster}
	}
	return nil
}

func admittedTogether(plan reconciliation.Plan, states map[string]reconciliation.BlockState, bound, throttle int) []reconciliation.Block {
	held, throttled := map[string]bool{}, map[string]int{}
	var admitted []reconciliation.Block
	for _, block := range reconciliation.Startable(plan, states, reconciliation.Selection{}) {
		if len(admitted) == bound || block.Throttle != "" && throttled[block.Throttle] == throttle || slices.ContainsFunc(block.Exclusive, func(key string) bool { return held[key] }) {
			continue
		}
		for _, key := range block.Exclusive {
			held[key] = true
		}
		throttled[block.Throttle]++
		admitted = append(admitted, block)
	}
	return admitted
}

func TestCephChildrenOfOneClusterStartTogetherUnderTheThrottle(t *testing.T) {
	for _, directory := range []string{"libvirt-6-nodes-ceph-services", "libvirt-3-nodes-ceph-object", "libvirt-3-nodes-ceph-block-file"} {
		t.Run(directory, func(t *testing.T) {
			plan, catalog := cephExamplePlan(t, directory)
			clusters := catalog.OfKind(api.CephCluster)
			if len(clusters) != 1 {
				t.Fatalf("%d clusters", len(clusters))
			}
			cluster := clusters[0].Name()
			states := map[string]reconciliation.BlockState{}
			released := []string{}
			for _, block := range plan.Blocks {
				states[block.ID] = reconciliation.BlockDone
				if !cephChild(block, cluster) {
					continue
				}
				states[block.ID] = reconciliation.BlockPending
				if want := cephExpectedClaims(block, cluster); !slices.Equal(block.Exclusive, want) {
					t.Errorf("%s claims %v, want %v", block.ID, block.Exclusive, want)
				}
				if !slices.Contains(block.Exclusive, ceph.OrchestratorKey(cluster)) {
					released = append(released, block.ID)
				}
			}
			if len(released) == 0 {
				t.Fatal("no Ceph child is released from the orchestrator claim")
			}
			startable := reconciliation.Startable(plan, states, reconciliation.Selection{})
			claims := map[string]string{}
			for _, block := range startable {
				for _, key := range block.Exclusive {
					if other := claims[key]; other != "" && !strings.HasPrefix(key, "ceph") {
						t.Errorf("ready siblings %s and %s share %s outside the Ceph claims", other, block.ID, key)
					}
					claims[key] = block.ID
				}
			}
			admitted := admittedTogether(plan, states, lifecycle.MaxRunningBlocks, lifecycle.MaxThrottledBlocks)
			holders := 0
			for _, block := range admitted {
				if slices.Contains(block.Exclusive, ceph.OrchestratorKey(cluster)) {
					holders++
				}
			}
			if len(admitted) != min(lifecycle.MaxThrottledBlocks, len(startable)) || holders > 1 || len(admitted) < 2 {
				var ids []string
				for _, block := range admitted {
					ids = append(ids, block.ID)
				}
				t.Fatalf("after the cluster block %d Ceph children start together (%s) with %d orchestrator holders; %d are startable",
					len(admitted), strings.Join(ids, ", "), holders, len(startable))
			}
		})
	}
}

func TestCephServicesExampleStartsItsPoliciesPoolsAndSettingsTogether(t *testing.T) {
	plan, catalog := cephExamplePlan(t, "libvirt-6-nodes-ceph-services")
	cluster := catalog.OfKind(api.CephCluster)[0].Name()
	states := map[string]reconciliation.BlockState{}
	for _, block := range plan.Blocks {
		states[block.ID] = reconciliation.BlockDone
		if cephChild(block, cluster) {
			states[block.ID] = reconciliation.BlockPending
		}
	}
	ready := map[string]bool{}
	for _, block := range reconciliation.Startable(plan, states, reconciliation.Selection{}) {
		ready[block.ID] = true
	}
	want := []string{ceph.SettingsBlockID(cluster)}
	for _, block := range plan.Blocks {
		request, err := ceph.DecodeRequest(block.Request)
		if err == nil && cephChild(block, cluster) && (request.Action == "policy" || request.Action == "pool" && block.Kind == string(api.CephObjectGateway)) {
			want = append(want, block.ID)
		}
		if err == nil && cephChild(block, cluster) && request.Action == "pool" && block.Kind == string(api.CephPool) && len(block.Dependencies) == 2 && len(block.Requires) == 0 {
			want = append(want, block.ID)
		}
	}
	for _, id := range want {
		if !ready[id] {
			t.Errorf("%s is not startable once the cluster is done", id)
		}
	}
	if len(want) < 3 {
		t.Fatalf("the example plans only %v as first-round children", want)
	}
}
