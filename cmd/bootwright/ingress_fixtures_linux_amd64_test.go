//go:build linux && amd64

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	effectiveencoding "github.com/crmarques/bootwright/internal/desiredstate/encoding"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const (
	ingressFixtureDir        = "testdata/ingress"
	auxiliaryImageRefusalRow = "A native service whose auxiliary image is not selected"
)

var ingressFixtureNames = []string{"nfs-haproxy-protocol", "nfs-keepalive-only", "object-ingress-two-hosts"}

type ingressFixture struct {
	Base   string           `yaml:"base"`
	Edits  []matrixEdit     `yaml:"edits"`
	Expect []ingressRefusal `yaml:"expect"`
}

type ingressRefusal struct {
	Kind    string `yaml:"kind"`
	Name    string `yaml:"name"`
	Service string `yaml:"service"`
	Image   string `yaml:"image"`
}

func loadIngressFixture(t *testing.T, name string) ingressFixture {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(ingressFixtureDir, name+".yaml"))
	if err != nil {
		t.Fatal(err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var fixture ingressFixture
	if err := decoder.Decode(&fixture); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if fixture.Base == "" || len(fixture.Edits) == 0 || len(fixture.Expect) == 0 {
		t.Fatalf("%s: the fixture needs a base, edits and expectations", name)
	}
	return fixture
}

// TestIngressFixturesRefuseUntilAuxiliaryImagesAreSelected applies each ingress
// variant to its tracked example: it compiles, every object of the cluster
// meets exactly the refusal table's auxiliary-image row filled for that
// ingress, and the effective StorageCluster matches its golden.
func TestIngressFixturesRefuseUntilAuxiliaryImagesAreSelected(t *testing.T) {
	table := markdownTable(t, cephStorageSpec, "### Refusal table", cephRefusalColumns)
	row := slices.IndexFunc(table, func(r map[string]string) bool { return r["Refusal"] == auxiliaryImageRefusalRow })
	if row < 0 {
		t.Fatalf("the refusal table has no row %q", auxiliaryImageRefusalRow)
	}
	reason, remedy := matrixCodeSpan(t, table[row]["Reason"]), matrixCodeSpan(t, table[row]["Remedy"])
	for _, name := range ingressFixtureNames {
		t.Run(name, func(t *testing.T) {
			fixture := loadIngressFixture(t, name)
			sources := matrixFixture{Row: name, Base: fixture.Base, Edits: fixture.Edits}.sources(t)
			state, _, err := wireCompiler().Compile(context.Background(), sources)
			if err != nil {
				t.Fatalf("the fixture must compile: %v", diagnostics.Of(err))
			}
			var got, want []string
			for _, refusal := range productionUnsupported(t, state) {
				got = append(got, strings.Join([]string{refusal.Kind, refusal.Name, refusal.Reason, refusal.Remediation}, " | "))
			}
			for _, e := range fixture.Expect {
				fill := strings.NewReplacer("<family>", "ingress", "<service>", e.Service, "<name>", e.Image, "<cluster>", "ceph-lab")
				r := lifecycle.Refusal{Kind: e.Kind, Name: e.Name, Reason: fill.Replace(reason), Remediation: fill.Replace(remedy)}
				want = append(want, strings.Join([]string{r.Kind, r.Name, r.Reason, r.Remediation}, " | "))
			}
			if diff := multisetDiff(got, want); diff != "" {
				t.Fatalf("%s:\n%s", name, diff)
			}
			cluster := requireObject(t, state.Effective(), api.StorageCluster, "ceph-lab")
			data, err := effectiveencoding.YAML(context.Background(), api.NewCatalog([]api.Object{cluster}))
			if err != nil {
				t.Fatal(err)
			}
			matchesTextGolden(t, "ingress/"+name, data)
		})
	}
}
