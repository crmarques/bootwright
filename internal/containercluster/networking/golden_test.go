package networking

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
	"github.com/crmarques/bootwright/internal/machine"
)

var update = flag.Bool("update", false, "rewrite networking goldens")

var noMachines = api.NewCatalog(nil)

func matchesGolden(t *testing.T, name string, data []byte, terminated bool) {
	t.Helper()
	body := data
	if terminated {
		trimmed, found := bytes.CutSuffix(data, []byte("\n"))
		if !found || bytes.HasSuffix(trimmed, []byte("\n")) {
			t.Fatalf("%s: a terminated format ends in exactly one LF: %q", name, data[max(0, len(data)-16):])
		}
		body = trimmed
	} else if len(bytes.TrimRight(data, " \t\r\n")) != len(data) {
		t.Fatalf("%s: an unterminated format ends in no whitespace: %q", name, data[max(0, len(data)-16):])
	}
	var compact, indented bytes.Buffer
	if err := json.Compact(&compact, body); err != nil {
		t.Fatalf("%s: the bytes are not one JSON document: %v", name, err)
	}
	if !bytes.Equal(compact.Bytes(), body) {
		t.Fatalf("%s: the bytes are not compact JSON, so an indented golden cannot pin them:\n%s", name, body)
	}
	if err := json.Indent(&indented, body, "", "  "); err != nil {
		t.Fatalf("%s: indenting: %v", name, err)
	}
	indented.WriteByte('\n')
	matchesTextGolden(t, name, indented.Bytes())
}

// matchesTextGolden compares bytes that are not one compact JSON document, such
// as JSON Lines, YAML or indented JSON, with testdata/<name>.golden byte for
// byte. git diff --check refuses a line ending in a space or tab and a blank
// line at the end of a file, so a golden holding either could never be
// committed.
func matchesTextGolden(t *testing.T, name string, data []byte) {
	t.Helper()
	text := string(data)
	if text == "\n" || strings.HasSuffix(text, "\n\n") {
		t.Fatalf("%s: the bytes end in a blank line, which git diff --check refuses", name)
	}
	for number, line := range strings.Split(text, "\n") {
		if strings.TrimRight(line, " \t") != line {
			t.Fatalf("%s: line %d ends in a space or tab, which git diff --check refuses", name, number+1)
		}
	}
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v; run with -update to write it", err)
	}
	if string(want) != text {
		t.Errorf("%s differs (-golden +got); rerun with -update if the change is intended:\n%s%s",
			path, lineDiff(string(want), text), firstDifference(string(want), text))
	}
}

// firstDifference quotes both texts around the first byte they differ at,
// which locates a change inside one long line such as an embedded document.
func firstDifference(want, got string) string {
	at := 0
	for at < len(want) && at < len(got) && want[at] == got[at] {
		at++
	}
	excerpt := func(text string) string { return text[max(0, at-40):min(len(text), at+40)] }
	return fmt.Sprintf("first difference at byte %d: golden %q, got %q", at, excerpt(want), excerpt(got))
}

// lineDiff lists the lines only the golden holds (-) and only the output holds
// (+), each numbered in its own text, along a longest common subsequence.
func lineDiff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	common := make([][]int, len(w)+1)
	for i := range common {
		common[i] = make([]int, len(g)+1)
	}
	for i := len(w) - 1; i >= 0; i-- {
		for j := len(g) - 1; j >= 0; j-- {
			if w[i] == g[j] {
				common[i][j] = common[i+1][j+1] + 1
			} else {
				common[i][j] = max(common[i+1][j], common[i][j+1])
			}
		}
	}
	var out strings.Builder
	for i, j := 0, 0; i < len(w) || j < len(g); {
		switch {
		case i < len(w) && j < len(g) && w[i] == g[j]:
			i, j = i+1, j+1
		case i < len(w) && (j == len(g) || common[i+1][j] >= common[i][j+1]):
			fmt.Fprintf(&out, "-%d: %s\n", i+1, w[i])
			i++
		default:
			fmt.Fprintf(&out, "+%d: %s\n", j+1, g[j])
			j++
		}
	}
	return out.String()
}

func clusterFixture() api.Object {
	network := func(name, vlan string) api.Value {
		return mapping(field("name", text(name)), field("topology", text("Localnet")), field("uplink", mapping(field("interface", text("eth1")), field("bridge", text("br-tenant")))), field("vlan", api.IntegerValue(vlan)), field("ipam", mapping(field("mode", text("Disabled")))), field("namespaceSelector", mapping(field("matchLabels", mapping(field("tenant", text(name)))))))
	}
	return api.NewObject(api.OpenShiftCluster, "cluster", mapping(), mapping(field("distribution", mapping(field("release", mapping(field("version", text("4.21.15")))))), field("nodes", api.ListValue(mapping(field("name", text("node-a")), field("machineRef", text("machine-a"))), mapping(field("name", text("node-b")), field("machineRef", text("machine-b"))))), field("networking", mapping(field("secondaryNetworks", api.ListValue(network("tenant-a", "10"), network("tenant-b", "20")))))))
}
func TestNetworkRequestGolden(t *testing.T) {
	request, err := renderRequest(clusterFixture(), noMachines, "controller")
	if err != nil {
		t.Fatal(err)
	}
	data, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "shared-uplink", data, false)
	decoded, err := DecodeRequest(data)
	if err != nil {
		t.Fatal(err)
	}
	again, err := decoded.Canonical()
	if err != nil || !bytes.Equal(data, again) {
		t.Fatal("request failed round trip", err)
	}
	program, err := decoded.Objects()
	if err != nil {
		t.Fatal(err)
	}
	creates := []co.ObjectEntry{}
	for _, step := range program.Steps {
		if step.Create != nil {
			creates = append(creates, *step.Create)
		}
	}
	if len(creates) != 4 || creates[0].Kind != "NodeNetworkConfigurationPolicy" || creates[2].Kind != "ClusterUserDefinedNetwork" {
		t.Fatal(creates)
	}
	if len(program.Checks) != 2 {
		t.Fatal(program.Checks)
	}
}

func addressedFixture(hostAddresses string) (api.Object, api.Catalog) {
	machineObject := func(name string, addresses ...api.Value) api.Object {
		uplink := mapping(field("name", text("eth1")), field("type", text("ethernet")), field("mtu", api.IntegerValue("9000")))
		install := mapping(field("name", text("eth0")), field("type", text("ethernet")))
		return api.NewObject(api.Machine, name, mapping(), mapping(field("network", mapping(field("inline", mapping(field("nmstate", mapping(field("interfaces", api.ListValue(install, uplink)))))), field("addresses", api.ListValue(addresses...))))))
	}
	address := func(name, ip, iface string) api.Value {
		return mapping(field("name", text(name)), field("address", text(ip)), field("interface", text(iface)))
	}
	uplink := mapping(field("interface", text("eth1")), field("bridge", text("br-tenant")))
	if hostAddresses != "" {
		uplink = uplink.With("hostAddresses", text(hostAddresses))
	}
	network := mapping(field("name", text("tenant")), field("topology", text("Localnet")), field("uplink", uplink), field("vlan", api.IntegerValue("10")), field("ipam", mapping(field("mode", text("Disabled")))), field("namespaceSelector", mapping(field("matchLabels", mapping(field("tenant", text("tenant")))))))
	cluster := api.NewObject(api.OpenShiftCluster, "cluster", mapping(), mapping(field("distribution", mapping(field("release", mapping(field("version", text("4.21.15")))))), field("nodes", api.ListValue(mapping(field("name", text("node-a")), field("machineRef", text("machine-a"))), mapping(field("name", text("node-b")), field("machineRef", text("machine-b"))))), field("networking", mapping(field("secondaryNetworks", api.ListValue(network))))))
	return cluster, api.NewCatalog([]api.Object{
		machineObject("machine-a", address("host-0", "198.51.100.10/24", "eth0"), address("host-4", "198.51.100.11/24", "eth1"), address("host-6", "2001:db8:1::11/64", "eth1")),
		machineObject("machine-b", address("host-0", "198.51.100.12/24", "eth0")),
	})
}

func TestAddressedBridgeRequestGolden(t *testing.T) {
	cluster, catalog := addressedFixture("bridge")
	request, err := renderRequest(cluster, catalog, "controller")
	if err != nil {
		t.Fatal(err)
	}
	data, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "addressed-bridge", data, false)
}

func TestHostAddressesNoneRendersTheAddresslessBridge(t *testing.T) {
	cluster, catalog := addressedFixture("")
	want, err := renderRequest(cluster, api.NewCatalog(nil), "controller")
	if err != nil {
		t.Fatal(err)
	}
	for _, placement := range []string{"", "none"} {
		cluster, catalog = addressedFixture(placement)
		got, err := renderRequest(cluster, catalog, "controller")
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got.Program, want.Program) {
			t.Fatalf("hostAddresses %q changed the program of an addressed uplink", placement)
		}
	}
}

func TestAddressedBridgePolicyShape(t *testing.T) {
	cluster, catalog := addressedFixture("bridge")
	networks := cluster.Spec().Get("networking", "secondaryNetworks").Items()
	policy := func(machineName string) []api.Value {
		bound, _ := catalog.Find(api.Machine, machineName)
		native, issues := machine.ComposeNetwork(bound, catalog)
		if len(issues) > 0 {
			t.Fatal(issues)
		}
		return nodePolicy(machineName, networks, native).Body.Get("spec", "desiredState", "interfaces").Items()
	}
	b := policy("machine-b")
	if len(b) != 1 || b[0].Get("bridge", "port").Len() != 1 {
		t.Fatalf("a node with no uplink address renders the plain bridge: %v", b)
	}
	a := policy("machine-a")
	if len(a) != 3 || a[0].Get("type").Text() != "ovs-bridge" || a[1].Get("type").Text() != "ovs-interface" || a[2].Get("name").Text() != "eth1" {
		t.Fatalf("an addressed node renders bridge, bridge interface and restated uplink in name order: %v", a)
	}
	if a[0].Get("bridge", "port").Len() != 2 || a[1].Get("copy-mac-from").Text() != "eth1" || a[1].Get("ipv4", "address").Len() != 1 || a[1].Get("ipv6", "address").Len() != 1 {
		t.Fatalf("bridge interface: %v", a[1])
	}
	if a[2].Get("ipv4", "enabled").Bool() || a[2].Get("ipv6", "enabled").Bool() || a[2].Get("ipv4", "address").Present() {
		t.Fatalf("restated uplink: %v", a[2])
	}
}
