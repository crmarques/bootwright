package compilation_test

import (
	"reflect"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/environment"
)

func TestGraphSelectionUsesInjectedCapabilitiesAndOwnsResults(t *testing.T) {
	object := api.NewObject(api.Environment, "synthetic", api.Value{}, api.Value{})
	catalog := api.NewCatalog([]api.Object{object})
	attachments := []addons.Attachment{{ClusterRef: "cluster", ExportRef: "export"}}
	excluded := []string{"excluded"}
	issue := api.Issue{Code: "api.reference", Message: "synthetic graph failure"}
	calls := []string{}
	selector := compilation.NewGraphSelector(func(input api.Catalog) []addons.Attachment {
		calls = append(calls, "attachments")
		if !reflect.DeepEqual(input, catalog) {
			t.Fatal("attachment provider received another catalog")
		}
		return attachments
	}, func(input api.Catalog, edges []environment.Attachment) environment.Selection {
		calls = append(calls, "selection")
		if !reflect.DeepEqual(input, catalog) || !reflect.DeepEqual(edges, []environment.Attachment{{ClusterRef: "cluster", ExportRef: "export"}}) {
			t.Fatal("selection lost catalog or translated attachment")
		}
		edges[0].ExportRef = "changed"
		return environment.Selection{Catalog: catalog, ExcludedContainerClusters: excluded, ExcludedStorageClusters: excluded, Problems: []environment.SelectionIssue{{Object: object, Issue: issue}}}
	})
	result := selector.Select(catalog)
	excluded[0] = "changed"
	if !reflect.DeepEqual(calls, []string{"attachments", "selection"}) || attachments[0].ExportRef != "export" {
		t.Fatal("capability ordering or input isolation changed")
	}
	if !reflect.DeepEqual(result.Catalog, catalog) || result.ExcludedContainerClusters[0] != "excluded" || result.ExcludedStorageClusters[0] != "excluded" || !reflect.DeepEqual(result.Problems, []compilation.ObjectIssue{{Object: object, Issue: issue}}) {
		t.Fatal("selection did not preserve independent result values")
	}
}

func TestUnconfiguredGraphSelectionRefusesAdmission(t *testing.T) {
	object := api.NewObject(api.Environment, "synthetic", api.Value{}, api.Value{})
	result := (compilation.GraphSelector{}).Select(api.NewCatalog([]api.Object{object}))
	if len(result.Problems) != 1 || result.Problems[0].Object.Identity() != object.Identity() || result.Problems[0].Issue.Code != "api.invariant" {
		t.Fatal("unconfigured selection did not return an environment admission failure")
	}
}
