package arbiter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/cephstate"
	"github.com/crmarques/bootwright/internal/workspace"
)

func stretchCluster() v1alpha1.StorageCluster {
	return v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph-prd-01"},
		Spec: v1alpha1.StorageClusterSpec{
			Type: "ceph",
			Ceph: &v1alpha1.StorageClusterCephSpec{
				Topology: v1alpha1.StorageCephTopology{
					Stretch: &v1alpha1.StorageCephStretch{
						FailureDomain: "datacenter",
						DataSites:     []string{"dc1", "dc2"},
						RuleName:      "stretch-rule",
						Tiebreaker:    v1alpha1.StorageCephTiebreaker{Node: "arb-new", Site: "dc3"},
					},
					Nodes: []v1alpha1.StorageCephNode{
						{Name: "node-01", MachineRef: v1alpha1.LocalObjectReference{Name: "m-01"}, Site: "dc1", Roles: []string{"mon", "osd"}},
						{Name: "node-02", MachineRef: v1alpha1.LocalObjectReference{Name: "m-02"}, Site: "dc1", Roles: []string{"mon", "osd"}},
						{Name: "node-03", MachineRef: v1alpha1.LocalObjectReference{Name: "m-03"}, Site: "dc2", Roles: []string{"mon", "osd"}},
						{Name: "node-04", MachineRef: v1alpha1.LocalObjectReference{Name: "m-04"}, Site: "dc2", Roles: []string{"mon", "osd"}},
						{Name: "arb-new", MachineRef: v1alpha1.LocalObjectReference{Name: "arb-new"}, Site: "dc3", Roles: []string{"mon"}},
					},
				},
			},
		},
	}
}

func discoveryWithMonDump(t *testing.T, dump map[string]any) cephstate.Discovery {
	t.Helper()
	raw, err := json.Marshal(dump)
	if err != nil {
		t.Fatalf("marshal mon dump fixture: %v", err)
	}
	return cephstate.Discovery{Cluster: "ceph-prd-01", Probed: true, Reads: map[string]json.RawMessage{cephstate.ReadMonDump: raw}}
}

func liveMonDump(tiebreaker string, quorum []int) map[string]any {
	return map[string]any{
		"stretch_mode":   true,
		"tiebreaker_mon": tiebreaker,
		"quorum":         quorum,
		"mons": []map[string]any{
			{"rank": 0, "name": "node-01", "crush_location": map[string]string{"datacenter": "dc1"}},
			{"rank": 1, "name": "node-02", "crush_location": map[string]string{"datacenter": "dc1"}},
			{"rank": 2, "name": "node-03", "crush_location": map[string]string{"datacenter": "dc2"}},
			{"rank": 3, "name": "node-04", "crush_location": map[string]string{"datacenter": "dc2"}},
			{"rank": 4, "name": "arb-old", "crush_location": map[string]string{"datacenter": "dc3"}},
		},
	}
}

func settledMonDump() map[string]any {
	return map[string]any{
		"stretch_mode":   true,
		"tiebreaker_mon": "arb-new",
		"quorum":         []int{0, 1, 2, 3, 4},
		"mons": []map[string]any{
			{"rank": 0, "name": "node-01", "crush_location": map[string]string{"datacenter": "dc1"}},
			{"rank": 1, "name": "node-02", "crush_location": map[string]string{"datacenter": "dc1"}},
			{"rank": 2, "name": "node-03", "crush_location": map[string]string{"datacenter": "dc2"}},
			{"rank": 3, "name": "node-04", "crush_location": map[string]string{"datacenter": "dc2"}},
			{"rank": 4, "name": "arb-new", "crush_location": map[string]string{"datacenter": "dc3"}},
		},
	}
}

func TestComputePlansTheSwapAgainstTheLiveTiebreaker(t *testing.T) {
	plan, err := Compute(stretchCluster(), discoveryWithMonDump(t, liveMonDump("arb-old", []int{0, 1, 2, 3, 4})))
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if plan.Settled {
		t.Fatal("a live tiebreaker that is not the desired node must not report settled")
	}
	if plan.DesiredMon != "arb-new" || plan.LiveMon != "arb-old" {
		t.Fatalf("plan resolved desired=%q live=%q, want arb-new/arb-old", plan.DesiredMon, plan.LiveMon)
	}
	if plan.LiveSite != "dc3" || plan.DesiredSite != "dc3" {
		t.Fatalf("plan resolved sites desired=%q live=%q, want both dc3", plan.DesiredSite, plan.LiveSite)
	}
	if plan.Degraded != "" {
		t.Fatalf("a fully in-quorum monmap must not read as degraded, got %q", plan.Degraded)
	}
	if plan.SameSite() {
		t.Fatalf("the replacement arbiter is alone in dc3, got same-site mons %v", plan.SameSiteMons)
	}
	wantDuring := []string{"arb-new", "arb-old", "node-01", "node-02", "node-03", "node-04"}
	if strings.Join(plan.MonHostsDuring, ",") != strings.Join(wantDuring, ",") {
		t.Fatalf("mon hosts during = %v, want %v (both arbiters must hold a mon while the tiebreaker moves)", plan.MonHostsDuring, wantDuring)
	}
	wantAfter := []string{"arb-new", "node-01", "node-02", "node-03", "node-04"}
	if strings.Join(plan.MonHostsAfter, ",") != strings.Join(wantAfter, ",") {
		t.Fatalf("mon hosts after = %v, want %v", plan.MonHostsAfter, wantAfter)
	}
}

func TestComputeReportsSettledOnlyWhenNothingIsLeftToRetire(t *testing.T) {
	plan, err := Compute(stretchCluster(), discoveryWithMonDump(t, settledMonDump()))
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if !plan.Settled {
		t.Fatalf("a monmap that names the desired tiebreaker and carries no replaced mon must report settled, got residue %v", plan.Residue)
	}
	if plan.LiveMon != "" {
		t.Fatalf("a settled plan must name no mon to retire, got %q", plan.LiveMon)
	}
}

func TestComputeResumesWhenTheTiebreakerSwitchedButTheReplacedMonRemains(t *testing.T) {
	plan, err := Compute(stretchCluster(), discoveryWithMonDump(t, liveMonDump("arb-new", []int{0, 1, 2, 3, 4})))
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if plan.Settled {
		t.Fatal("a run interrupted after the tiebreaker switch must not report settled while the replaced mon is still in the monmap, or the re-run that finishes the retirement exits 0 having done nothing")
	}
	if plan.LiveMon != "arb-old" {
		t.Fatalf("the resumed plan must retire the mon the topology no longer declares, got %q", plan.LiveMon)
	}
	if len(plan.Residue) == 0 {
		t.Fatal("the resumed plan must name what is left to retire")
	}
}

func TestComputeRefusesAMonmapCarryingSeveralUndeclaredMons(t *testing.T) {
	dump := settledMonDump()
	dump["mons"] = append(dump["mons"].([]map[string]any),
		map[string]any{"rank": 5, "name": "arb-old", "crush_location": map[string]string{"datacenter": "dc3"}},
		map[string]any{"rank": 6, "name": "arb-older", "crush_location": map[string]string{"datacenter": "dc3"}})
	_, err := Compute(stretchCluster(), discoveryWithMonDump(t, dump))
	if err == nil || !strings.Contains(err.Error(), "arb-old") || !strings.Contains(err.Error(), "arb-older") {
		t.Fatalf("Compute with two undeclared mons = %v, want a refusal naming both rather than a guess", err)
	}
}

func TestComputeReportsMonsOutsideQuorumAsDegraded(t *testing.T) {
	plan, err := Compute(stretchCluster(), discoveryWithMonDump(t, liveMonDump("arb-old", []int{0, 1, 4})))
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if !strings.Contains(plan.Degraded, "node-03") || !strings.Contains(plan.Degraded, "node-04") {
		t.Fatalf("degraded reason = %q, want it to name the mons outside quorum", plan.Degraded)
	}
}

func TestComputeRefusesAClusterThatIsNotInStretchModeLive(t *testing.T) {
	_, err := Compute(stretchCluster(), discoveryWithMonDump(t, map[string]any{"stretch_mode": false}))
	if err == nil || !strings.Contains(err.Error(), "not in stretch mode") {
		t.Fatalf("Compute on a non-stretch cluster = %v, want a stretch-mode refusal", err)
	}
}

func TestComputeRefusesAStretchMonmapWithNoTiebreaker(t *testing.T) {
	dump := settledMonDump()
	dump["tiebreaker_mon"] = ""
	_, err := Compute(stretchCluster(), discoveryWithMonDump(t, dump))
	if err == nil || !strings.Contains(err.Error(), "no tiebreaker_mon") {
		t.Fatalf("Compute on a stretch monmap without a tiebreaker = %v, want a refusal before the input is rewritten", err)
	}
}

func TestComputeResolvesTheRetiredHostFromTheOrchestrator(t *testing.T) {
	live := discoveryWithMonDump(t, liveMonDump("arb-old", []int{0, 1, 2, 3, 4}))
	hosts, err := json.Marshal([]map[string]any{{"hostname": "arb-old.ceph.example.com", "addr": "198.51.100.9"}})
	if err != nil {
		t.Fatalf("marshal host list fixture: %v", err)
	}
	live.Reads[cephstate.ReadOrchHostLS] = hosts
	plan, err := Compute(stretchCluster(), live)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if plan.LiveNode != "arb-old.ceph.example.com" {
		t.Fatalf("live node = %q, want the hostname cephadm registered; `ceph orch host rm` takes the registered hostname, not the mon daemon name", plan.LiveNode)
	}
}

func TestValidateCandidateRefusesAMachineBoundToAnotherCluster(t *testing.T) {
	state := v1alpha1.State{
		Machines: []v1alpha1.Machine{{
			Metadata: v1alpha1.Metadata{Name: "arb-new"},
			Spec:     v1alpha1.MachineSpec{Capabilities: []string{v1alpha1.MachineCapabilityCephNode, v1alpha1.MachineCapabilityCephArbiter}},
		}},
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: "ceph-other"},
			Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
				Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{
					{Name: "other-01", MachineRef: v1alpha1.LocalObjectReference{Name: "arb-new"}, Roles: []string{"mon"}},
				}},
			}},
		}},
	}
	err := ValidateCandidate(state, stretchCluster(), "arb-new")
	if err == nil || !strings.Contains(err.Error(), "ceph-other") {
		t.Fatalf("ValidateCandidate on a machine bound elsewhere = %v, want a refusal naming the owning cluster", err)
	}
}

func TestValidateCandidateRequiresTheArbiterCapability(t *testing.T) {
	state := v1alpha1.State{Machines: []v1alpha1.Machine{{
		Metadata: v1alpha1.Metadata{Name: "arb-new"},
		Spec:     v1alpha1.MachineSpec{Capabilities: []string{v1alpha1.MachineCapabilityCephNode}},
	}}}
	err := ValidateCandidate(state, stretchCluster(), "arb-new")
	if err == nil || !strings.Contains(err.Error(), v1alpha1.MachineCapabilityCephArbiter) {
		t.Fatalf("ValidateCandidate without the arbiter capability = %v, want a refusal naming it", err)
	}
}

const clusterFixtureYAML = `apiVersion: bootwright.io/v1alpha1
kind: StorageCluster
metadata:
  name: ceph-prd-01
spec:
  type: ceph
  ceph:
    topology:
      stretch:
        failureDomain: datacenter
        tiebreaker:
          node: arb-old
      nodes:
        - name: node-01
          machineRef:
            name: m-01
          site: dc1
          roles: [mon, osd]
        - name: arb-old
          fqdn: arb-old.old.example.test
          machineRef:
            name: m-arb-old
          site: dc3
          roles: [mon]
`

func TestComputePromotionRewritesTheAuthoredTiebreaker(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cluster.yaml")
	if err := os.WriteFile(path, []byte(clusterFixtureYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	cluster := v1alpha1.StorageCluster{
		Metadata:   v1alpha1.Metadata{Name: "ceph-prd-01"},
		SourcePath: path,
		Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Topology: v1alpha1.StorageCephTopology{
				Stretch: &v1alpha1.StorageCephStretch{FailureDomain: "datacenter", Tiebreaker: v1alpha1.StorageCephTiebreaker{Node: "arb-old", Site: "dc3"}},
				Nodes: []v1alpha1.StorageCephNode{
					{Name: "node-01", MachineRef: v1alpha1.LocalObjectReference{Name: "m-01"}, Site: "dc1", Roles: []string{"mon", "osd"}},
					{Name: "arb-old", FQDN: "arb-old.old.example.test", MachineRef: v1alpha1.LocalObjectReference{Name: "m-arb-old"}, Site: "dc3", Roles: []string{"mon"}},
				},
			},
		}},
	}
	promotion, err := ComputePromotion(workspace.Context{Name: "lab", InputDir: dir}, cluster, "arb-new")
	if err != nil {
		t.Fatalf("ComputePromotion: %v", err)
	}
	if promotion.RelPath != "cluster.yaml" {
		t.Fatalf("promotion rel path = %q, want cluster.yaml", promotion.RelPath)
	}
	got := string(promotion.Content)
	for _, want := range []string{"name: arb-new", "node: arb-new"} {
		if !strings.Contains(got, want) {
			t.Fatalf("promoted document missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "m-arb-old") {
		t.Fatalf("promoted document still references the replaced machine:\n%s", got)
	}
	if strings.Contains(got, "arb-old.old.example.test") {
		t.Fatalf("promoted document kept the replaced host's fqdn override:\n%s", got)
	}
	if !strings.Contains(got, "site: dc3") {
		t.Fatalf("promoted document dropped the arbiter site:\n%s", got)
	}
}

func TestComputePromotionIsANoOpWhenTheMachineAlreadyHoldsTheArbiter(t *testing.T) {
	promotion, err := ComputePromotion(workspace.Context{Name: "lab", InputDir: t.TempDir()}, stretchCluster(), "arb-new")
	if err != nil {
		t.Fatalf("ComputePromotion: %v", err)
	}
	if !promotion.Empty() {
		t.Fatal("promoting the machine that already carries the arbiter must produce no input edit")
	}
}
