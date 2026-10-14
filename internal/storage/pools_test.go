package storage

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestIBMReplicationNeedsAnAdditionalOSDHost(t *testing.T) {
	for _, test := range []struct {
		name       string
		replicated api.Value
		extraOSD   bool
		extraMON   bool
		policy     bool
		stretch    bool
		oss        bool
		bad        bool
	}{
		{name: "explicit two replicas", replicated: m("size", 2, "minSize", 2)},
		{name: "three replicas need four hosts", replicated: m("size", 3, "minSize", 2), bad: true},
		{name: "omitted size retains three", bad: true},
		{name: "zero size retains three", replicated: m("size", 0, "minSize", 0), bad: true},
		{name: "policy default retains three", policy: true, bad: true},
		{name: "four OSD hosts support default three", extraOSD: true},
		{name: "monitor-only host supplies no OSD capacity", extraMON: true, bad: true},
		{name: "stretch protection remains separately owned", extraOSD: true, stretch: true},
		{name: "OSS keeps its existing capacity rule", oss: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			catalog := fixtureCatalog()
			cluster := find(t, catalog, api.StorageCluster, "storage")
			if !test.oss {
				cluster = cluster.WithSpec(cluster.Spec().WithPath(api.StringValue("ibm"), "ceph", "distribution"))
			}
			if test.extraOSD || test.extraMON {
				nodes := cluster.Spec().Get("ceph", "topology", "nodes").Items()
				labels := []string{"mon", "mgr"}
				if test.extraOSD {
					labels = append(labels, "storage")
				}
				nodes = append(nodes, m("name", "node-4", "machineRef", "machine-4", "labels", api.StringList(labels...)))
				cluster = cluster.WithSpec(cluster.Spec().WithPath(api.ListValue(nodes...), "ceph", "topology", "nodes"))
			}
			if test.stretch {
				cluster = cluster.WithSpec(cluster.Spec().WithPath(m("failureDomain", "datacenter"), "ceph", "topology", "stretch"))
			}
			catalog = replace(catalog, cluster)
			pool := find(t, catalog, api.StoragePool, "block")
			if test.replicated.Present() {
				pool = pool.WithSpec(pool.Spec().With("replicated", test.replicated))
			}
			if test.policy {
				pool = pool.WithSpec(pool.Spec().With("placementPolicyRef", api.StringValue("policy")))
			}
			issues := Validate(pool, catalog)
			if !test.bad {
				if len(issues) != 0 {
					t.Fatalf("valid protection refused: %+v", issues)
				}
				return
			}
			if len(issues) != 1 || issues[0].Field != "$.spec.replicated.size" || !strings.Contains(issues[0].Message, "additional OSD-bearing host") || !strings.Contains(issues[0].Remediation, "replicated.size: 2") {
				t.Fatalf("missing actionable provider-capacity refusal: %+v", issues)
			}
		})
	}
}
