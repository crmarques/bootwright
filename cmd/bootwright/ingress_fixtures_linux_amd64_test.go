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
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	effectiveencoding "github.com/crmarques/bootwright/internal/desiredstate/encoding"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/storage/ceph"
)

const (
	ingressFixtureDir        = "testdata/ingress"
	auxiliaryImageRefusalRow = "A native service whose auxiliary image is not selected"
)

var ingressFixtureNames = []string{"nfs-haproxy-protocol", "nfs-keepalive-only", "object-ingress-two-hosts"}

type ingressFixture struct {
	Base    string           `yaml:"base"`
	Edits   []matrixEdit     `yaml:"edits"`
	Select  []matrixEdit     `yaml:"select"`
	Images  []string         `yaml:"images"`
	Planned []string         `yaml:"planned"`
	Expect  []ingressRefusal `yaml:"expect"`
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
	if fixture.Base == "" || len(fixture.Edits) == 0 || len(fixture.Select) == 0 || len(fixture.Images) == 0 || len(fixture.Planned) == 0 || len(fixture.Expect) == 0 {
		t.Fatalf("%s: the fixture needs a base, edits, a selection, its images, its planned blocks and expectations", name)
	}
	return fixture
}

// TestIngressFixturesWithoutTheirImagesRefuseNamingTheField applies each
// ingress variant to its tracked example without the selection: it compiles
// and every object of the cluster meets exactly the refusal table's
// auxiliary-image row filled for that ingress.
func TestIngressFixturesWithoutTheirImagesRefuseNamingTheField(t *testing.T) {
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
		})
	}
}

// TestIngressFixturesPlanWithSelectedImages selects synthetic auxiliary
// images on each ingress variant: nothing is refused, every production binding
// plans, the image and cluster blocks carry exactly the selected images, and
// the effective CephCluster matches its golden.
func TestIngressFixturesPlanWithSelectedImages(t *testing.T) {
	for _, name := range ingressFixtureNames {
		t.Run(name, func(t *testing.T) {
			fixture := loadIngressFixture(t, name)
			sources := matrixFixture{Row: name, Base: fixture.Base, Edits: slices.Concat(fixture.Edits, fixture.Select)}.sources(t)
			state, _, err := wireCompiler().Compile(context.Background(), sources)
			if err != nil {
				t.Fatalf("the fixture must compile: %v", diagnostics.Of(err))
			}
			if refused := productionUnsupported(t, state); len(refused) != 0 {
				t.Fatalf("the capabilities refuse %+v", refused)
			}
			plan := planIngressFixture(t, state, name)
			block := func(id string) reconciliation.Block {
				index := slices.IndexFunc(plan.Blocks, func(b reconciliation.Block) bool { return b.ID == id })
				if index < 0 {
					t.Fatalf("the plan has no block %s", id)
				}
				return plan.Blocks[index]
			}
			want := selectedIngressImages(t, fixture)
			image, err := ceph.DecodeImageRequest(block("ceph-image-ceph-lab").Request)
			if err != nil {
				t.Fatal(err)
			}
			var gotNames []string
			for _, a := range image.Auxiliary {
				gotNames = append(gotNames, a.Name)
			}
			if !slices.Equal(gotNames, fixture.Images) {
				t.Fatalf("the image block lists %v, want %v", gotNames, fixture.Images)
			}
			request, err := ceph.DecodeRequest(block("ceph-cluster-ceph-lab").Request)
			if err != nil {
				t.Fatal(err)
			}
			var got []ceph.AuxiliaryReference
			for _, a := range request.Auxiliary {
				got = append(got, ceph.AuxiliaryReference{Name: a.Name, Image: a.Image})
			}
			if !slices.Equal(got, want) {
				t.Fatalf("the cluster block carries %v, want %v", got, want)
			}
			block("ceph-settings-ceph-lab")
			for _, id := range fixture.Planned {
				block(id)
			}
			cluster := requireObject(t, state.Effective(), api.CephCluster, "ceph-lab")
			data, err := effectiveencoding.YAML(context.Background(), api.NewCatalog([]api.Object{cluster}))
			if err != nil {
				t.Fatal(err)
			}
			matchesTextGolden(t, "ingress/"+name, data)
		})
	}
}

func selectedIngressImages(t *testing.T, fixture ingressFixture) []ceph.AuxiliaryReference {
	t.Helper()
	var documents struct {
		Spec struct {
			Images map[string]struct {
				Base    string `yaml:"base"`
				Version string `yaml:"version"`
			} `yaml:"images"`
		} `yaml:"spec"`
	}
	var want []ceph.AuxiliaryReference
	for _, edit := range fixture.Select {
		if err := yaml.Unmarshal([]byte("spec:"+edit.With), &documents); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range fixture.Images {
		image, ok := documents.Spec.Images[name]
		if !ok {
			t.Fatalf("the selection authors no %s", name)
		}
		separator := ":"
		if strings.HasPrefix(image.Version, "sha256:") {
			separator = "@"
		}
		want = append(want, ceph.AuxiliaryReference{Name: name, Image: image.Base + separator + image.Version})
	}
	return want
}

func planIngressFixture(t *testing.T, state *compilation.State, name string) reconciliation.Plan {
	t.Helper()
	resolver := buildCapabilities(systemClock{}, exampleControllerPorts(t), exampleMediaRecords{})
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: name}, State: state, Controller: "controller"}
	var definitions []reconciliation.BlockDefinition
	for _, binding := range resolver.Bindings() {
		capability, _ := resolver.Resolve(binding.Kind, binding.Implementation)
		contribution, err := capability.Plan(context.Background(), input)
		if err != nil {
			if strings.HasPrefix(binding.Implementation, "storage-ceph-") {
				t.Fatalf("%v: %+v", binding, diagnostics.Of(err))
			}
			continue
		}
		definitions = append(definitions, contribution.Definitions...)
	}
	plan, err := reconciliation.NewPlan(reconciliation.Apply, definitions)
	if err != nil {
		t.Fatalf("the plan model refuses what the bindings planned: %v", err)
	}
	return plan
}
