package clients

import (
	"maps"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// refusalSpec owns this capability's refusal table, read from this package's
// directory.
const refusalSpec = "../../../specs/api/environment.md"

// refusalCase is a selected graph that one row of the refusal table refuses,
// and the value of each placeholder the row's reason and remedy write in angle
// brackets. Every refusal names the Environment the capability plans for.
type refusalCase struct {
	objects []api.Object
	values  map[string]string
}

// proxied is the controller Machine selecting the Proxy egress, with bypass
// entries when given.
func proxied(bypass ...string) api.Object {
	choice := api.MapValue(text("proxyRef", "egress"))
	if len(bypass) != 0 {
		choice = choice.With("noProxy", api.StringList(bypass...))
	}
	controller := machine("container-runtime")
	return controller.WithSpec(controller.Spec().With("proxy", choice))
}

// externalProxy is the external Proxy egress with the given connection.
func externalProxy(connection ...api.FieldValue) api.Object {
	return api.NewObject(api.Proxy, "egress", api.Value{}, api.MapValue(text("management", "external"), field("connection", api.MapValue(connection...))))
}

// TestControllerRefusalTableMatchesUnsupported holds the controller stage
// capability's Unsupported to the Environment refusal table: every row refuses
// its graph with exactly its reason and remedy, and a row added or removed on
// either side fails here.
func TestControllerRefusalTableMatchesUnsupported(t *testing.T) {
	endpoint := text("httpsProxy", "http://proxy.example.test:3128")
	names := map[string]string{"<machine>": "Machine/controller", "<proxy>": "Proxy/egress"}
	bypass := make([]string, 129)
	for index := range bypass {
		bypass[index] = "host-" + strconv.Itoa(index) + ".example.test"
	}
	cases := map[string]refusalCase{
		"Another capability": {[]api.Object{machine("container-runtime", "libvirt", "ceph-node")},
			map[string]string{"<machine>": "Machine/controller", "<index>": "2"}},
		"Too many bypass entries": {[]api.Object{proxied(bypass...), externalProxy(endpoint)}, names},
		"Managed Proxy": {[]api.Object{proxied(), api.NewObject(api.Proxy, "egress", api.Value{}, api.MapValue(text("management", "managed")))},
			names},
		"Proxy authentication": {[]api.Object{proxied(), externalProxy(endpoint, field("auth", api.MapValue(text("proxyAuthRef", "proxy-credentials"))))},
			names},
		"Private trust":    {[]api.Object{proxied(), externalProxy(endpoint, text("trustBundleRef", "corporate-ca"))}, names},
		"HTTP proxy alone": {[]api.Object{proxied(), externalProxy(text("httpProxy", "http://proxy.example.test:3128"))}, names},
	}
	for _, row := range refusalTable(t) {
		test, found := cases[row.name]
		if !found {
			t.Errorf("the table row %q has no graph here that it refuses", row.name)
			continue
		}
		delete(cases, row.name)
		t.Run(row.name, func(t *testing.T) {
			want := []lifecycle.Refusal{{Kind: "Environment", Name: "lab", Reason: expand(row.reason, test.values), Remediation: expand(row.remedy, test.values)}}
			if got := (Capability{}).Unsupported(stateOf(append([]api.Object{environment()}, test.objects...)...)); !reflect.DeepEqual(got, want) {
				t.Fatalf("the capability refuses %+v, the table %+v", got, want)
			}
		})
	}
	for _, name := range slices.Sorted(maps.Keys(cases)) {
		t.Errorf("the table has no row %q", name)
	}
	if got := (Capability{}).Unsupported(stateOf(environment(), proxied(".example.test"), externalProxy(endpoint))); len(got) != 0 {
		t.Fatalf("a supported controller was refused: %+v", got)
	}
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
