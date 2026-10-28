package libvirt

import (
	"fmt"
	"maps"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate"
	"github.com/crmarques/bootwright/internal/substrate/kubevirt"
)

// refusalSpec owns this capability's refusal table, read from this package's
// directory.
const refusalSpec = "../../../specs/substrates.md"

// refusalCase is a selected graph that one row of the refusal table refuses:
// the object that row's refusal names, and the value of each placeholder the
// row's reason and remedy write in angle brackets.
type refusalCase struct {
	catalog api.Catalog
	refused string
	values  map[string]string
}

// TestSubstrateRefusalTableMatchesUnsupported holds the provider host
// capabilities' Unsupported, libvirt's and KubeVirt's, to the substrate
// refusal table: every row refuses
// its object with exactly its reason and remedy, and a row added or removed on
// either side fails here. A hosted Machine is refused beside its provider, so
// each row is held to the refusals of its own object.
func TestSubstrateRefusalTableMatchesUnsupported(t *testing.T) {
	vsphere := api.NewObject(api.InfraProvider, "vc", api.Value{}, api.MapValue(field("vsphere", api.MapValue())))
	hosted := api.NewObject(api.Machine, "node-01", api.Value{}, api.MapValue(
		field("substrate", api.MapValue(text("providerRef", "vc"))),
		field("os", api.MapValue(field("provided", api.BoolValue(false)))),
	))
	defaults := provider().Spec().Get("libvirt", "bmcEmulation").With("bindAddress", api.StringValue("0.0.0.0"))
	wildcard := provider(field("libvirt", provider().Spec().Get("libvirt").With("bmcEmulation", defaults)))
	cases := map[string]refusalCase{
		"A provider on an unrealized substrate": {catalogOf(controller(), vsphere), "InfraProvider/vc",
			map[string]string{"<provider>": "InfraProvider/vc"}},
		"A Machine on an unrealized substrate": {catalogOf(controller(), vsphere, hosted), "Machine/node-01",
			map[string]string{"<provider>": "InfraProvider/vc", "<machine>": "Machine/node-01"}},
		"An emulated BMC no endpoint can name": {catalogOf(controller(), wildcard), "InfraProvider/lab-libvirt",
			map[string]string{"<provider>": "InfraProvider/lab-libvirt"}},
		"A provider with more managed attachments than its reservation holds": {catalogOf(controller(), provider(managedAttachments(substrate.MaxManagedAttachments+1))),
			"InfraProvider/lab-libvirt", map[string]string{"<provider>": "InfraProvider/lab-libvirt"}},
		"A provider using more external bridges than its use reservation holds": {catalogOf(controller(), provider(externalAttachments(manyBridges(substrate.MaxExternalBridges+1)...))),
			"InfraProvider/lab-libvirt", map[string]string{"<provider>": "InfraProvider/lab-libvirt"}},
		"A KubeVirt provider on a host this context builds": {catalogOf(controller(), kubevirtProvider("hostClusterRef", "")), "InfraProvider/kv",
			map[string]string{"<provider>": "InfraProvider/kv"}},
		"A Bootwright-installed Machine on KubeVirt": {catalogOf(controller(), networkConfig(), kubevirtProvider("kubeconfigRef", ""),
			kubevirtGuest(text("installProfileRef", "rhel-9-8"))), "Machine/node-01", map[string]string{"<machine>": "Machine/node-01"}},
		"A KubeVirt profile without a size": {catalogOf(controller(), networkConfig(), kubevirtProvider("kubeconfigRef", "", number("cpu", "0")), kubevirtGuest()),
			"Machine/node-01", map[string]string{"<provider>": "InfraProvider/kv", "<profile>": "node"}},
		"A KubeVirt interface on an unrealized network kind": {catalogOf(controller(), networkConfig(), kubevirtProvider("kubeconfigRef", "UserDefinedNetwork"), kubevirtGuest()),
			"Machine/node-01", map[string]string{"<machine>": "Machine/node-01", "<attachment>": "lab-guests"}},
	}
	for _, row := range refusalTable(t) {
		test, found := cases[row.name]
		if !found {
			t.Errorf("the table row %q has no graph here that it refuses", row.name)
			continue
		}
		delete(cases, row.name)
		t.Run(row.name, func(t *testing.T) {
			kind, name, _ := strings.Cut(test.refused, "/")
			want := []lifecycle.Refusal{{Kind: kind, Name: name, Reason: expand(row.reason, test.values), Remediation: expand(row.remedy, test.values)}}
			state := compilation.NewState(test.catalog, test.catalog, nil)
			got := slices.DeleteFunc(append(NewHost(nil).Unsupported(state), kubevirt.NewHost(nil).Unsupported(state)...),
				func(refused lifecycle.Refusal) bool { return refused.Kind != kind || refused.Name != name })
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("the capability refuses %+v, the table %+v", got, want)
			}
		})
	}
	for _, name := range slices.Sorted(maps.Keys(cases)) {
		t.Errorf("the table has no row %q", name)
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

func manyBridges(count int) []string {
	bridges := make([]string, 0, count)
	for index := range count {
		bridges = append(bridges, fmt.Sprintf("br-%02d", index))
	}
	return bridges
}

// kubevirtProvider is a KubeVirt provider on the host its credential names,
// with one profile and one attachment of the network kind given.
func kubevirtProvider(credential, kind string, profile ...api.FieldValue) api.Object {
	group := map[string]string{"": "k8s.cni.cncf.io", "UserDefinedNetwork": "k8s.ovn.org"}[kind]
	if kind == "" {
		kind = "NetworkAttachmentDefinition"
	}
	selected := api.MapValue(text("name", "node"), number("cpu", "4"), number("memoryMiB", "16384"), number("diskGiB", "120"))
	for _, extra := range profile {
		selected = selected.With(extra.Name, extra.Value)
	}
	return api.NewObject(api.InfraProvider, "kv", api.Value{}, api.MapValue(
		field("kubevirt", api.MapValue(text(credential, "host"), text("namespace", "guests"), field("machineProfiles", api.ListValue(selected)))),
		field("networkAttachments", api.ListValue(api.MapValue(text("name", "lab-guests"), field("kubevirt", api.MapValue(field("networkRef",
			api.MapValue(text("apiGroup", group), text("kind", kind), text("name", "guest-l2"), text("namespace", "guests")))))))),
	))
}

func kubevirtGuest(os ...api.FieldValue) api.Object {
	declared := api.MapValue(field("provided", api.BoolValue(false)))
	for _, extra := range os {
		declared = declared.With(extra.Name, extra.Value)
	}
	return api.NewObject(api.Machine, "node-01", api.Value{}, api.MapValue(
		field("substrate", api.MapValue(text("providerRef", "kv"), field("kubevirt", api.MapValue(text("profileRef", "node"))))),
		field("os", declared),
		field("network", api.MapValue(text("configRef", "lab-guests"), text("attachmentRef", "lab-guests"))),
	))
}
