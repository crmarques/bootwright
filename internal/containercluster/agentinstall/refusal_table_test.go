package agentinstall

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
)

// refusalSpec owns this capability's refusal table, read from this package's
// directory.
const refusalSpec = "../../../specs/container-clusters.md"

// refusalCase is a selected graph that one row of the refusal table refuses:
// the object its one refusal names, and the value of each placeholder the
// row's reason and remedy write in angle brackets.
type refusalCase struct {
	catalog api.Catalog
	refused string
	values  map[string]string
}

// TestContainerClusterRefusalTableMatchesUnsupported holds both capabilities'
// Unsupported to the container-cluster refusal table: every row refuses its
// graph with exactly its reason and remedy, and a row added or removed on
// either side fails here.
func TestContainerClusterRefusalTableMatchesUnsupported(t *testing.T) {
	sno := func(mutate func(api.Value) api.Value) api.Catalog {
		declared := cluster("sno",
			installSelection(endpoints("198.51.100.21", "198.51.100.21", "198.51.100.21", "node")),
			node("master-0", "master", "sno-01", "master-0.sno.lab.example.test"))
		return api.NewCatalog(append(base(), guest("sno-01", "198.51.100.21/24"), declared.WithSpec(mutate(declared.Spec()))))
	}
	onSNO := func(machine api.Object, extra ...api.Object) api.Catalog {
		return api.NewCatalog(append(append(base(), extra...), machine, cluster("sno",
			installSelection(endpoints("198.51.100.21", "198.51.100.21", "198.51.100.21", "node")),
			node("master-0", "master", "sno-01", "master-0.sno.lab.example.test"))))
	}
	installed := guest("sno-01", "198.51.100.21/24")
	installed = installed.WithSpec(installed.Spec().WithPath(api.StringValue("rhel-9-8"), "os", "installProfileRef"))
	unrealized := guest("sno-01", "198.51.100.21/24")
	unrealized = unrealized.WithSpec(unrealized.Spec().WithPath(api.StringValue("vc"), "substrate", "providerRef"))
	vsphere := api.NewObject(api.InfraProvider, "vc", api.Value{}, api.MapValue(field("vsphere", api.MapValue())))
	multiNode := api.NewCatalog(append(base(),
		guest("ocp-01", "198.51.100.31/24"), guest("ocp-02", "198.51.100.32/24"), guest("ocp-03", "198.51.100.33/24"),
		cluster("ocp",
			installSelection(
				endpoints("198.51.100.10", "198.51.100.10", "198.51.100.11", "external"),
				field("platform", api.MapValue(text("type", "vsphere"))),
			),
			node("master-0", "master", "ocp-01", "master-0.ocp.lab.example.test"),
			node("master-1", "master", "ocp-02", "master-1.ocp.lab.example.test"),
			node("master-2", "master", "ocp-03", "master-2.ocp.lab.example.test"))))
	onSNOCluster := map[string]string{"<cluster>": "ContainerCluster/sno"}
	onNode := map[string]string{"<cluster>": "ContainerCluster/sno", "<machine>": "Machine/sno-01"}
	cases := map[string]refusalCase{
		"An OKD cluster": {sno(func(spec api.Value) api.Value {
			return spec.WithPath(api.StringValue("okd"), "distribution", "type")
		}), "ContainerCluster/sno", onSNOCluster},
		"A release pinned by image alone": {sno(func(spec api.Value) api.Value {
			return spec.With("distribution", api.MapValue(text("type", "openshift"),
				field("release", api.MapValue(text("image", "quay.io/openshift/release:4.21.15")))))
		}), "ContainerCluster/sno", onSNOCluster},
		"A disconnected cluster": {sno(func(spec api.Value) api.Value {
			return spec.WithPath(api.StringValue("disconnected"), "install", "mode")
		}), "ContainerCluster/sno", onSNOCluster},
		"A FIPS cluster": {sno(func(spec api.Value) api.Value {
			return spec.With("security", api.MapValue(field("fips", api.MapValue(field("enabled", api.BoolValue(true))))))
		}), "ContainerCluster/sno", onSNOCluster},
		"An installation proxy": {sno(func(spec api.Value) api.Value {
			return spec.WithPath(api.MapValue(text("proxyRef", "lab-proxy")), "install", "proxy")
		}), "ContainerCluster/sno", onSNOCluster},
		"Disk encryption": {sno(func(spec api.Value) api.Value {
			return spec.With("security", api.MapValue(field("diskEncryption", api.MapValue(
				field("unlock", api.MapValue(field("tpm2", api.MapValue())))))))
		}), "ContainerCluster/sno", onSNOCluster},
		"Serving certificates": {sno(func(spec api.Value) api.Value {
			return spec.WithPath(api.MapValue(text("defaultCertificateRef", "ingress-tls")), "install", "servingCertificates", "ingress")
		}), "ContainerCluster/sno", onSNOCluster},
		"A registry policy": {sno(func(spec api.Value) api.Value {
			return spec.WithPath(api.MapValue(field("mirror", api.MapValue(text("registryRef", "mirror")))), "install", "registries")
		}), "ContainerCluster/sno", onSNOCluster},
		"Several nodes on another platform": {multiNode, "ContainerCluster/ocp", map[string]string{"<cluster>": "ContainerCluster/ocp"}},
		"More nodes than the run ceiling fits": {largeCatalog(10), "ContainerCluster/ocp",
			map[string]string{"<cluster>": "ContainerCluster/ocp", "<nodes>": "10", "<deadline>": "6h10m0s"}},
		"A node with an install profile":    {onSNO(installed), "ContainerCluster/sno", onNode},
		"A node on an unrealized substrate": {onSNO(unrealized, vsphere), "ContainerCluster/sno", onSNOCluster},
		"A physical node": {physicalCatalog(), "ContainerCluster/metal",
			map[string]string{"<cluster>": "ContainerCluster/metal", "<machine>": "Machine/metal-01"}},
		"A root device the installer cannot name": {singleNodeWith(text("deviceName", "/dev/mapper/root")), "ContainerCluster/sno", onNode},
		"A root device size the frozen input cannot carry": {
			singleNodeWith(text("deviceName", "/dev/vda"), number("minSizeGigabytes", "9007199254740992")), "ContainerCluster/sno", onNode,
		},
		"A node off the artifact server's host": {
			api.NewCatalog(append(append(base(), hypervisor()...), onRemote(guest("sno-01", "198.51.100.21/24")), cluster("sno",
				installSelection(endpoints("198.51.100.21", "198.51.100.21", "198.51.100.21", "node")),
				node("master-0", "master", "sno-01", "master-0.sno.lab.example.test")))),
			"ContainerCluster/sno",
			map[string]string{
				"<cluster>": "ContainerCluster/sno", "<machine>": "Machine/sno-01", "<provider host>": "Machine/hv-01",
				"<server>": "ArtifactServer/lab-artifacts", "<server host>": "Machine/controller", "<provider>": "InfraProvider/far-libvirt",
			},
		},
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
			if got := (MediaCapability{}).Unsupported(state); !reflect.DeepEqual(got, want) {
				t.Fatalf("the media capability refuses %+v, the table %+v", got, want)
			}
			if got := (InstallCapability{}).Unsupported(state); !reflect.DeepEqual(got, want) {
				t.Fatalf("the installation capability refuses %+v, the table %+v", got, want)
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
