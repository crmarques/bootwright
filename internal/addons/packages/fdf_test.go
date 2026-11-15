package packages

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
)

func TestFDFWithoutMultiCloudGatewayKeepsExternalRGW(t *testing.T) {
	i := addons.Instance{Config: packageTestConfig("fdf").With("multiCloudGateway", api.BoolValue(false)), Facts: addons.ClusterFacts{Exports: []addons.ConsumerFacts{{Name: "export", Gateway: true}}}}
	r, err := renderFDF(i)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if r.After[0].Objects[0].Body.Get("spec", "multiCloudGateway", "reconcileStrategy").Text() != "ignore" || strings.Contains(text, "noobaa") || !strings.Contains(text, "ocs-external-storagecluster-ceph-rgw") {
		t.Fatalf("FDF without MCG lost RGW or still requires NooBaa: %s", text)
	}
}

func TestFDFTrustAndCatalogAdmission(t *testing.T) {
	v := packageTestConfig("fdf")
	f := addons.ClusterFacts{Name: "cluster", Exports: []addons.ConsumerFacts{{Name: "export", Gateway: true, TLS: true}}}
	for _, tc := range []struct {
		name   string
		trust  addons.TrustFacts
		issues int
	}{
		{"no bundles", addons.TrustFacts{Policy: "Always"}, 1},
		{"proxy only", addons.TrustFacts{Policy: "Proxyonly", Bundles: 1}, 1},
		{"trusted", addons.TrustFacts{Policy: "Always", Bundles: 1}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f.Trust = tc.trust
			if issues := admitFDF(addons.Instance{Config: v}, f); len(issues) != tc.issues {
				t.Fatal(issues)
			}
		})
	}
	f.Exports = nil
	if issues := admitFDF(addons.Instance{Config: v.WithPath(api.StringValue("unserved"), "subscription", "source")}, f); len(issues) != 1 || issues[0].Field != ".subscription.source" {
		t.Fatal(issues)
	}
	f.Trust = addons.TrustFacts{}
	f.Exports = []addons.ConsumerFacts{{Name: "export", Gateway: true}}
	if issues := admitFDF(addons.Instance{Config: v}, f); len(issues) != 0 {
		t.Fatal(issues)
	}
}

func TestFDFRenderGoldens(t *testing.T) {
	for _, test := range []struct{ gateway, mcg bool }{{false, true}, {true, true}, {false, false}, {true, false}} {
		gateway := test.gateway
		name := "fdf"
		if gateway {
			name += "-gateway"
		}
		if !test.mcg {
			name += "-no-mcg"
		}
		t.Run(name, func(t *testing.T) {
			i := addons.Instance{Config: packageTestConfig("fdf").With("multiCloudGateway", api.BoolValue(test.mcg)), Facts: addons.ClusterFacts{Exports: []addons.ConsumerFacts{{Name: "export", Gateway: gateway}}}}
			r, err := renderFDF(i)
			if err != nil {
				t.Fatal(err)
			}
			p := co.Program{Version: co.Version, Steps: []co.Step{}, Checks: r.Checks}
			for _, phase := range r.After {
				steps, err := co.CompilePhase(phase)
				if err != nil {
					t.Fatal(err)
				}
				p.Steps = append(p.Steps, steps...)
			}
			data, err := co.Freeze(p, co.Options{Namespace: "openshift-storage"})
			if err != nil {
				t.Fatal(err)
			}
			matchesGolden(t, name, data, false)
			if gateway && test.mcg {
				fixture := filepath.Join("..", "..", "..", "ansible", "collections", "ansible_collections", "bootwright", "core", "tests", "unit", "goldens", "fdf-gateway.json")
				got, err := os.ReadFile(fixture)
				if err != nil {
					t.Fatal(err)
				}
				actual, err := co.Canonical(json.RawMessage(got))
				if err != nil || string(actual) != string(data) {
					t.Fatal("adapter fixture differs from rendered program", err)
				}
			}
			if _, err := co.ThawVersion(data, co.Options{Namespace: "openshift-storage"}); err != nil {
				t.Fatal(err)
			}
			if len(r.Requires) != 1 || r.Requires[0].Kind != "CephConsumer" || r.Requires[0].Object != "export" {
				t.Fatal(r.Requires)
			}
		})
	}
}
