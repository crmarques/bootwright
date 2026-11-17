package cephcluster

import (
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestAClusterResolvesEachPlacementOnceAsItsOwnServicesResolveIt(t *testing.T) {
	cluster, _ := stretchCatalog(t, nil)
	placed := newPlacements(cluster)
	for _, service := range cluster.Spec().Get("services").Items() {
		wantNames, wantIssues := ResolveServicePlacement(cluster, service)
		for range 2 {
			names, issues := placed.resolve(service)
			if !slices.Equal(names, wantNames) || !slices.EqualFunc(issues, wantIssues, func(a, b api.Issue) bool { return a == b }) {
				t.Fatalf("the %s placement resolves to %v %v, want %v %v", NativeServiceName(service), names, issues, wantNames, wantIssues)
			}
		}
	}
	if len(placed.resolved) != cluster.Spec().Get("services").Len() {
		t.Fatalf("the cluster resolved %d placements for %d distinct services", len(placed.resolved), cluster.Spec().Get("services").Len())
	}
	for _, node := range []string{"a1", "b2", "tie"} {
		if !placed.member("mon", node) || placed.member("mgr", "tie") {
			t.Fatalf("the MON members miss %s or the MGR members hold the tiebreaker", node)
		}
	}
}

// A child object's admission resolves the cluster it references once, the
// cluster's own admission keeps its own placements, and a cluster that
// differs in any field is resolved afresh.
func TestAChildAdmissionSharesItsClustersPlacements(t *testing.T) {
	cluster, catalog := stretchCatalog(t, nil)
	pool := object(api.CephPool, "block", m("clusterRef", "storage", "role", "rbd"))
	child := admission{object: pool, catalog: catalog}
	placed := child.placementsOf(cluster)
	for _, service := range cluster.Spec().Get("services").Items() {
		child.placementsOf(cluster).resolve(service)
		child.placementsOf(cluster).resolve(service)
	}
	if child.placementsOf(cluster) != placed || len(placed.resolved) != cluster.Spec().Get("services").Len() {
		t.Fatalf("the child resolved %d placements for %d services, or dropped its cache", len(placed.resolved), cluster.Spec().Get("services").Len())
	}
	changed := cluster.WithSpec(cluster.Spec().WithPath(api.StringValue("x"), "topology", "stretch", "ruleName"))
	if child.placementsOf(changed) == placed || child.placementsOf(changed).cluster.Spec().Equal(cluster.Spec()) {
		t.Fatal("a changed cluster reused the placements of the one before it")
	}
	own := admission{object: cluster, catalog: catalog}
	if own.placementsOf(cluster) != own.placements() || own.foreign != nil {
		t.Fatal("the cluster's own admission did not use its own placements")
	}
}

func BenchmarkValidateTheStretchClustersPools(b *testing.B) {
	cluster, catalog := stretchCatalog(b, nil)
	pools := []api.Object{}
	for _, role := range []string{"rbd", "cephfsData", "cephfsMetadata"} {
		pools = append(pools, object(api.CephPool, role, m("clusterRef", cluster.Name(), "role", role)))
	}
	for b.Loop() {
		for _, pool := range pools {
			Validate(pool, catalog)
		}
	}
}

func BenchmarkValidateAStretchCluster(b *testing.B) {
	cluster, catalog := stretchCatalog(b, nil)
	for b.Loop() {
		if issues := Validate(cluster, catalog); len(issues) != 0 {
			b.Fatal(issues)
		}
	}
}
