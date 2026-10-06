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
			for _, problem := range environment.Select(api.NewCatalog(objects), nil).Problems {
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
	const count = 2000
	declared, missing := clusterNames(count), []string{}
	objects := []api.Object{}
	for index, name := range declared {
		objects = append(objects, selectionContainer(name, name+"-n"))
		missing = append(missing, fmt.Sprintf("m%04d", index))
	}
	for _, row := range []struct {
		name     string
		selected []string
		problems int
	}{{"every entry unresolved", missing, 2 * count}, {"every entry resolved", declared, 0}} {
		t.Run(row.name, func(t *testing.T) {
			catalog := api.NewCatalog(append([]api.Object{selectionEnvironment("env", "containerClusters", row.selected)}, objects...))
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			selection := environment.Select(catalog, nil)
			runtime.ReadMemStats(&after)
			if len(selection.Problems) != row.problems {
				t.Fatalf("%d problems, want %d", len(selection.Problems), row.problems)
			}
			if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 32<<20 {
				t.Fatalf("selecting %d entries over %d clusters allocated %d bytes", len(row.selected), count, allocated)
			}
			runtime.KeepAlive(selection)
		})
	}
}

func clusterNames(count int) []string {
	names := make([]string, 0, count)
	for index := range count {
		names = append(names, fmt.Sprintf("c%02d", index))
	}
	return names
}
