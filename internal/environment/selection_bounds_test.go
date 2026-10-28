package environment_test

import (
	"fmt"
	"runtime"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/environment"
)

func TestUnresolvedClusterListsAtMostSixteenDeclaredNames(t *testing.T) {
	const sixteen = "c00, c01, c02, c03, c04, c05, c06, c07, c08, c09, c10, c11, c12, c13, c14, c15"
	rows := []struct {
		name     string
		declared []string
		want     string
	}{
		{"sixteen declared are all listed", clusterNames(16), "select one of: " + sixteen},
		{"seventeen declared list the first sixteen sorted and count the rest", clusterNames(17), "select one of: " + sixteen + ", and 1 more"},
		{"a repeated or invalid declared name is not listed or counted", append(clusterNames(17), "c03", "Bad_Name"), "select one of: " + sixteen + ", and 1 more"},
		{"forty declared count twenty-four more", clusterNames(40), "select one of: " + sixteen + ", and 24 more"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			objects := []api.Object{selectionEnvironment("env", "containerClusters", []string{"missing"})}
			for index := len(row.declared) - 1; index >= 0; index-- {
				objects = append(objects, selectionContainer(row.declared[index], row.declared[index]+"-n"))
			}
			unresolved := []api.Issue{}
			for _, problem := range environment.Select(api.NewCatalog(objects), nil, testClusterMembers).Problems {
				if problem.Issue.Code == "api.reference" {
					unresolved = append(unresolved, problem.Issue)
				}
			}
			if len(unresolved) != 1 || unresolved[0].Field != "$.spec.containerClusters[0]" || unresolved[0].Remediation != row.want {
				t.Fatalf("unresolved = %+v, want one at $.spec.containerClusters[0] with remediation %q", unresolved, row.want)
			}
		})
	}
}

func TestClusterSelectionCostGrowsWithTheInputNotItsSquare(t *testing.T) {
	const small, large = 500, 2000
	for _, row := range []struct {
		name       string
		unresolved bool
	}{{"every entry unresolved", true}, {"every entry resolved", false}} {
		t.Run(row.name, func(t *testing.T) {
			smallBytes, largeBytes := selectionAllocation(t, small, row.unresolved), selectionAllocation(t, large, row.unresolved)
			const factor, ceiling = large / small, 100 << 20
			if largeBytes > ceiling {
				t.Fatalf("selecting %d entries allocated %d bytes, over the %d byte safety net", large, largeBytes, ceiling)
			}
			if largeBytes > smallBytes*factor*3/2 {
				t.Fatalf("selecting %d entries allocated %d bytes and %d entries %d bytes: growth %.1fx for %dx the input, want about %dx",
					small, smallBytes, large, largeBytes, float64(largeBytes)/float64(smallBytes), factor, factor)
			}
		})
	}
}

func selectionAllocation(t *testing.T, count int, unresolved bool) uint64 {
	t.Helper()
	declared, missing := clusterNames(count), []string{}
	objects := []api.Object{}
	for index, name := range declared {
		objects = append(objects, selectionContainer(name, name+"-n"))
		missing = append(missing, fmt.Sprintf("m%04d", index))
	}
	selected, problems := declared, 0
	if unresolved {
		selected, problems = missing, 2*count
	}
	catalog := api.NewCatalog(append([]api.Object{selectionEnvironment("env", "containerClusters", selected)}, objects...))
	environment.Select(catalog, nil, testClusterMembers)
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	selection := environment.Select(catalog, nil, testClusterMembers)
	runtime.ReadMemStats(&after)
	if len(selection.Problems) != problems {
		t.Fatalf("%d problems, want %d", len(selection.Problems), problems)
	}
	runtime.KeepAlive(selection)
	return after.TotalAlloc - before.TotalAlloc
}

func clusterNames(count int) []string {
	names := make([]string, 0, count)
	for index := range count {
		names = append(names, fmt.Sprintf("c%02d", index))
	}
	return names
}
