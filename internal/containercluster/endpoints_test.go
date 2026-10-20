package containercluster

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

const wildcardLabelTable = "../../ansible/collections/ansible_collections/bootwright/core/tests/unit/goldens/containercluster_wildcard_labels.json"

// The install role's resolve.yml renders the same table in
// test_containercluster_wildcard_label_parity.py, so the Go rule and the
// role's Jinja rule cannot drift apart.
func TestWildcardLabelMatchesTheInstallRoleTable(t *testing.T) {
	data, err := os.ReadFile(wildcardLabelTable)
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct{ Case, Cluster, Label string }
	if err := json.Unmarshal(data, &rows); err != nil || len(rows) == 0 {
		t.Fatalf("table %v %v", rows, err)
	}
	trimmed := false
	for _, row := range rows {
		t.Run(row.Case, func(t *testing.T) {
			if got := WildcardLabel(row.Cluster); got != row.Label {
				t.Fatalf("WildcardLabel(%q) = %q, want %q", row.Cluster, got, row.Label)
			}
		})
		if full := "bootwright-" + row.Cluster; len(full) > 63 && full[62] == '-' {
			trimmed = true
		}
	}
	if !trimmed {
		t.Fatal("the table has no cluster whose cut label ends in a hyphen")
	}
}

func TestResolutionNamesAreTheNamesTheInstallationProves(t *testing.T) {
	long := strings.Repeat("c", 51) + "-d" + strings.Repeat("e", 10)
	address := func(value string) api.Value { return m("address", value) }
	for _, row := range []struct {
		cluster   string
		endpoints api.Value
		want      []Endpoint
	}{
		{"sno", m("api", address("192.0.2.10"), "api-int", address("192.0.2.11"), "ingress", address("192.0.2.12")), []Endpoint{
			{Name: "api-int.sno.example.test", Address: "192.0.2.11", Slot: "api-int"},
			{Name: "api.sno.example.test", Address: "192.0.2.10", Slot: "api"},
			{Name: "console-openshift-console.apps.sno.example.test", Address: "192.0.2.12", Slot: "ingress"},
			{Name: "bootwright-sno.apps.sno.example.test", Address: "192.0.2.12", Slot: "ingress-wildcard"},
		}},
		{"sno", m("api", address("192.0.2.10")), []Endpoint{
			{Name: "api.sno.example.test", Address: "192.0.2.10", Slot: "api"},
		}},
		{long, m("ingress", address("192.0.2.12")), []Endpoint{
			{Name: "console-openshift-console.apps." + long + ".example.test", Address: "192.0.2.12", Slot: "ingress"},
			{Name: "bootwright-" + long[:51] + ".apps." + long + ".example.test", Address: "192.0.2.12", Slot: "ingress-wildcard"},
		}},
	} {
		catalog := api.NewCatalog([]api.Object{
			obj(api.Environment, "env", m("domains", m("containerClusters", "example.test"))),
			obj(api.ContainerCluster, row.cluster, m("install", m("endpoints", row.endpoints))),
		})
		cluster, _ := catalog.Find(api.ContainerCluster, row.cluster)
		if got := ResolutionNames(catalog, cluster); !reflect.DeepEqual(got, row.want) {
			t.Fatalf("%s: got %+v, want %+v", row.cluster, got, row.want)
		}
	}
}
