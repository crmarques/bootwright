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

func BenchmarkValidateAStretchCluster(b *testing.B) {
	cluster, catalog := stretchCatalog(b, nil)
	for b.Loop() {
		if issues := Validate(cluster, catalog); len(issues) != 0 {
			b.Fatal(issues)
		}
	}
}
