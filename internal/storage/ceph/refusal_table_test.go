package ceph

import (
	"maps"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/storage/cephcluster"
)

// refusalSpec owns this capability's refusal table, read from this package's
// directory.
const refusalSpec = "../../../specs/ceph.md"

// refusalCase is the set of edits to the pinned catalog's CephCluster spec
// that one row of the refusal table refuses, and the value of each placeholder
// the row's reason and remedy write in angle brackets, with the objects the
// row adds to the catalog.
type refusalCase struct {
	edits     map[string]func(api.Value) api.Value
	values    map[string]string
	overrides map[string]map[string]string
	extra     []api.Object
	// normalized names the edits whose cluster the admission normalization
	// completes before the capability sees it, as in an effective state.
	normalized []string
}

// TestCephRefusalTableMatchesUnsupported holds the Ceph capability's
// Unsupported to the storage refusal table: each row refuses the CephCluster
// and the CephPool and CephClient that name it with exactly its reason
// and remedy, and a row added or removed on either side fails here. A later
// refusal adds one row to the table and one case here.
func TestCephRefusalTableMatchesUnsupported(t *testing.T) {
	set := func(value string, path ...string) func(api.Value) api.Value {
		return func(spec api.Value) api.Value { return spec.WithPath(api.StringValue(value), path...) }
	}
	cases := map[string]refusalCase{
		"More Ceph hosts than the cluster run deadline admits": {edits: map[string]func(api.Value) api.Value{
			"twelve hosts": func(spec api.Value) api.Value {
				nodes := spec.Get("topology", "nodes").Items()
				return spec.WithPath(api.ListValue(slices.Repeat(nodes[:1], 12)...), "topology", "nodes")
			},
			"eight stretch hosts": func(spec api.Value) api.Value {
				nodes := spec.Get("topology", "nodes").Items()
				return spec.WithPath(api.ListValue(slices.Repeat(nodes[:1], 8)...), "topology", "nodes").WithPath(requestMap("failureDomain", "datacenter"), "topology", "stretch")
			},
		}, values: map[string]string{"<cluster>": "example", "<hosts>": "12", "<deadline>": "6h10m0s", "<most>": "11"},
			overrides: map[string]map[string]string{"eight stretch hosts": {"<hosts>": "8", "<deadline>": "6h3m0s", "<most>": "7"}}},
		"A distribution that core Ceph does not realize": {normalized: []string{"omitted distribution"}, edits: map[string]func(api.Value) api.Value{
			"omitted distribution": func(spec api.Value) api.Value { return spec.Without("distribution") },
			"oss arm": func(spec api.Value) api.Value {
				return spec.WithPath(requestMap("oss", api.MapValue()), "distribution")
			},
			"redhat arm": func(spec api.Value) api.Value {
				return spec.WithPath(requestMap("redhat", api.MapValue()), "distribution")
			},
		}},
		"Incomplete or unsafe Ceph coordinates or transport": {edits: map[string]func(api.Value) api.Value{
			"native transport": set("native", "cephadm", "clusterSSH", "transport"),
			"omitted transport": func(spec api.Value) api.Value {
				return spec.WithPath(requestMap("user", "operator", "keyRef", "cluster-key"), "cephadm", "clusterSSH")
			},
			"empty release":                set("", "release"),
			"empty packageVersion":         set("", "packageVersion"),
			"empty ansible packageVersion": set("", "cephadm", "ansible", "packageVersion"),
			"empty image version":          set("", "image", "version"),
			"empty image base":             set("", "image", "base"),
		}},
		"A native service whose auxiliary image is not selected": auxiliaryServiceCase(),
		"CephSMBResource objects whose samba images are not selected": {edits: map[string]func(api.Value) api.Value{
			"a join auth and a users resource": nil,
			"samba set, samba_metrics not": func(spec api.Value) api.Value {
				return spec.WithPath(requestMap("base", PinnedRegistry+"/synthetic/samba", "version", "synthetic-1"), "images", "samba")
			},
		}, values: map[string]string{"<smb>": "smb-join", "<cluster>": "example", "<name>": "samba"},
			overrides: map[string]map[string]string{"samba set, samba_metrics not": {"<name>": "samba_metrics"}}, extra: []api.Object{smbUsers(), smbJoin()}},
		"An auxiliary image that no declared service needs": {edits: map[string]func(api.Value) api.Value{
			"haproxy without an ingress": func(spec api.Value) api.Value {
				return spec.WithPath(requestMap("base", PinnedRegistry+"/synthetic/haproxy", "version", "synthetic-1"), "images", "haproxy")
			},
		}, values: map[string]string{"<name>": "haproxy", "<cluster>": "example"}},
	}
	refused := map[api.Kind]string{api.CephCluster: "example", api.CephPlacementPolicy: "policy", api.CephPool: "volumes", api.CephClient: "consumer"}
	for _, row := range refusalTable(t) {
		test, found := cases[row.name]
		if !found {
			t.Errorf("the table row %q has no graph here that it refuses", row.name)
			continue
		}
		delete(cases, row.name)
		for _, edit := range slices.Sorted(maps.Keys(test.edits)) {
			t.Run(row.name+"/"+edit, func(t *testing.T) {
				catalog := pinnedCatalog(t, test.edits[edit])
				catalog = api.NewCatalog(append(append(catalog.Objects(), test.extra...), childOf(api.CephPlacementPolicy, "policy", "replicated", requestMap("size", 2))))
				if slices.Contains(test.normalized, edit) {
					catalog = normalizedCatalog(t, catalog)
				}
				state := compilation.NewState(catalog, catalog, nil)
				values := maps.Clone(test.values)
				maps.Copy(values, test.overrides[edit])
				for kind, name := range refused {
					want := []lifecycle.Refusal{{Kind: string(kind), Name: name, Reason: expand(row.reason, values), Remediation: expand(row.remedy, values)}}
					if got := New(kind, nil).Unsupported(state); !reflect.DeepEqual(got, want) {
						t.Fatalf("the capability refuses %+v, the table %+v", got, want)
					}
				}
			})
		}
	}
	for _, name := range slices.Sorted(maps.Keys(cases)) {
		t.Errorf("the table has no row %q", name)
	}
}

// normalizedCatalog completes each object of the catalog by the admission
// normalization the effective state carries.
func normalizedCatalog(t *testing.T, catalog api.Catalog) api.Catalog {
	t.Helper()
	objects := catalog.Objects()
	for index, object := range objects {
		normalized, issues := cephcluster.Normalize(object, catalog)
		if len(issues) != 0 {
			t.Fatalf("normalize %s: %v", object.Identity(), issues)
		}
		objects[index] = normalized
	}
	return api.NewCatalog(objects)
}

// auxiliaryServiceCase refuses one service of each type the auxiliary image
// table lists, and an ingress that only keeps its address, with no image set.
func auxiliaryServiceCase() refusalCase {
	types := map[string]string{
		"alertmanager": "alertmanager", "alloy": "alloy", "grafana": "grafana", "ingress": "haproxy", "loki": "loki",
		"mgmt-gateway": "nginx", "node-exporter": "node_exporter", "nvmeof": "nvmeof", "oauth2-proxy": "oauth2_proxy",
		"prometheus": "prometheus", "promtail": "promtail", "smb": "samba", "snmp-gateway": "snmp_gateway",
	}
	test := refusalCase{edits: map[string]func(api.Value) api.Value{}, overrides: map[string]map[string]string{}, values: map[string]string{"<cluster>": "example"}}
	add := func(edit, service, family, image string, spec api.Value) {
		test.edits[edit] = func(cluster api.Value) api.Value {
			authored := requestMap("service_type", family, "placement", requestMap("label", "mon", "count", 1))
			if spec.Present() {
				authored = authored.With("spec", spec)
			}
			return cluster.WithPath(api.ListValue(append(cluster.Get("services").Items(), authored)...), "services")
		}
		test.overrides[edit] = map[string]string{"<family>": family, "<service>": service, "<name>": image}
	}
	for family, image := range types {
		add(family+" service", family, family, image, api.Value{})
	}
	add("ingress that only keeps its address", "ingress", "ingress", "keepalived", requestMap("keepalive_only", true))
	return test
}

// refusalRow is one row of a refusal table: its name, and its reason and remedy
// read out of their code spans. Its path is for the reader alone.
type refusalRow struct {
	name, reason, remedy string
}

// refusalTable reads the one table under the refusal-table heading of the
// spec. It fails when the heading is missing or repeated, or when the header,
// the separator or a row does not match.
func refusalTable(t *testing.T) []refusalRow {
	t.Helper()
	data, err := os.ReadFile(refusalSpec)
	if err != nil {
		t.Fatal(err)
	}
	const heading = "### Refusal table"
	lines := strings.Split(string(data), "\n")
	start := slices.Index(lines, heading)
	if start < 0 || slices.Contains(lines[start+1:], heading) {
		t.Fatalf("%s must hold exactly one %q heading", refusalSpec, heading)
	}
	var table [][]string
	for _, line := range lines[start+1:] {
		if strings.HasPrefix(line, "|") {
			table = append(table, strings.Split(strings.TrimSuffix(strings.TrimPrefix(line, "| "), " |"), " | "))
			continue
		}
		if len(table) != 0 || strings.HasPrefix(line, "#") {
			break
		}
	}
	if len(table) < 3 || !slices.Equal(table[0], []string{"Refusal", "Path", "Reason", "Remedy"}) ||
		!slices.Equal(table[1], []string{"---", "---", "---", "---"}) {
		t.Fatalf("%q holds no refusal table", heading)
	}
	var rows []refusalRow
	for _, cells := range table[2:] {
		if len(cells) != 4 {
			t.Fatalf("%q has a malformed row %q", heading, cells)
		}
		rows = append(rows, refusalRow{name: cells[0], reason: codeSpan(t, cells[2]), remedy: codeSpan(t, cells[3])})
	}
	return rows
}

// codeSpan reads a cell that must be exactly one code span.
func codeSpan(t *testing.T, cell string) string {
	t.Helper()
	value, opened := strings.CutPrefix(cell, "`")
	value, closed := strings.CutSuffix(value, "`")
	if !opened || !closed || value == "" || strings.Contains(value, "`") {
		t.Fatalf("cell %q is not one code span", cell)
	}
	return value
}

// expand writes each placeholder's value into a row's text.
func expand(text string, values map[string]string) string {
	for placeholder, value := range values {
		text = strings.ReplaceAll(text, placeholder, value)
	}
	return text
}
