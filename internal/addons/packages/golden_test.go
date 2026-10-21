package packages

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
)

var update = flag.Bool("update", false, "rewrite package render goldens")

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

func TestPackageRenderGoldens(t *testing.T) {
	for _, p := range Builtin().Packages() {
		t.Run(p.Name, func(t *testing.T) {
			configuration := config().Without("pools")
			if p.Name == "metallb" {
				configuration = config(pool("services", "192.0.2.80-192.0.2.90"))
			}
			rendered, err := p.Render(addons.Instance{Cluster: "cluster", Binding: "binding", Config: configuration})
			if err != nil {
				t.Fatal(err)
			}
			program := co.Program{Version: co.Version, Steps: []co.Step{}, Checks: rendered.Checks}
			for _, phase := range rendered.After {
				steps, err := co.CompilePhase(phase)
				if err != nil {
					t.Fatal(err)
				}
				program.Steps = append(program.Steps, steps...)
			}
			data, err := co.Freeze(program, co.Options{Namespace: p.OLM.Namespace})
			if err != nil {
				t.Fatal(err)
			}
			matchesGolden(t, p.Name, data, false)
			if _, err := co.ThawVersion(data, co.Options{Namespace: p.OLM.Namespace}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func maximumPools() []api.Value {
	pools := []api.Value{}
	for i := 0; i < 16; i++ {
		addresses := []string{}
		for j := 0; j < 16; j++ {
			addresses = append(addresses, fmt.Sprintf("192.0.%d.%d-192.0.%d.%d", i, 80+j, i, 80+j))
		}
		pools = append(pools, pool(fmt.Sprintf("pool-%02d", i), addresses...))
	}
	return pools
}

func TestMetalLBAdditionalRenderGoldens(t *testing.T) {
	secondary := facts()
	secondary.Gateway = addons.OVNGateway{RoutingViaHost: true, IPForwarding: "Global"}
	secondary.NodeNetworks[0].Interfaces = append(secondary.NodeNetworks[0].Interfaces, addons.NodeInterface{Name: "eth1", Addresses: []netip.Prefix{netip.MustParsePrefix("198.51.100.10/24")}})
	maximum := facts()
	maximum.MachineNetworks = []netip.Prefix{netip.MustParsePrefix("192.0.0.0/20")}
	for _, test := range []struct {
		name          string
		configuration api.Value
		facts         addons.ClusterFacts
		objects       int
	}{
		{"metallb-secondary", config(pool("secondary", "198.51.100.80-198.51.100.90").With("l2", api.MapValue(api.FieldValue{Name: "interface", Value: api.StringValue("eth1")}))), secondary, 3},
		{"metallb-maximum", config(maximumPools()...), maximum, 33},
	} {
		t.Run(test.name, func(t *testing.T) {
			instance := addons.Instance{Cluster: "cluster", Binding: "binding", Config: test.configuration}
			if issues := admitPools(instance, test.facts); len(issues) != 0 {
				t.Fatalf("golden configuration is not admitted: %+v", issues)
			}
			rendered, err := renderMetalLB(instance)
			if err != nil {
				t.Fatal(err)
			}
			program := co.Program{Version: co.Version, Steps: []co.Step{}, Checks: rendered.Checks}
			objects := 0
			for _, phase := range rendered.After {
				objects += len(phase.Objects)
				steps, err := co.CompilePhase(phase)
				if err != nil {
					t.Fatal(err)
				}
				program.Steps = append(program.Steps, steps...)
			}
			if objects != test.objects {
				t.Fatalf("objects: %d", objects)
			}
			data, err := co.Freeze(program, co.Options{Namespace: "metallb-system"})
			if err != nil {
				t.Fatal(err)
			}
			matchesGolden(t, test.name, data, false)
			if _, err := co.ThawVersion(data, co.Options{Namespace: "metallb-system"}); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, configuration := range []api.Value{
		config(append(maximumPools(), pool("overflow", "192.0.15.200-192.0.15.201"))...),
		config(pool("overflow", append(maximumPools()[0].Get("addresses").Strings(), "192.0.0.200-192.0.0.201")...)),
	} {
		issues := admitPools(addons.Instance{Config: configuration}, maximum)
		if len(issues) != 1 || issues[0].Code != "api.value" {
			t.Fatalf("bound overflow: %+v", issues)
		}
	}
}
