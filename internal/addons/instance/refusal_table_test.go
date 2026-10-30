package instance

import (
	"context"
	"errors"
	"maps"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const refusalSpec = "../../../specs/add-ons.md"

type refusalRow struct {
	name, reason, remedy string
}

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
	if len(table) < 3 || !slices.Equal(table[0], []string{"Refusal", "Path", "Reason", "Remedy"}) || !slices.Equal(table[1], []string{"---", "---", "---", "---"}) {
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

func codeSpan(t *testing.T, cell string) string {
	t.Helper()
	value, opened := strings.CutPrefix(cell, "`")
	value, closed := strings.CutSuffix(value, "`")
	if !opened || !closed || value == "" || strings.Contains(value, "`") {
		t.Fatalf("cell %q is not one code span", cell)
	}
	return value
}

func expand(text string, values map[string]string) string {
	for placeholder, value := range values {
		text = strings.ReplaceAll(text, placeholder, value)
	}
	return text
}

func syntheticPackage(name string, render func(addons.Instance) (addons.Rendered, error)) addons.Package {
	p := fixturePackage()
	p.Name = name
	p.Render = render
	return p
}

func manyChecks(count int) []co.Check {
	result := []co.Check{}
	for i := range count {
		result = append(result, co.Check{ResourceExists: &co.Ref{APIVersion: "v1", Kind: "ConfigMap", Name: "check-" + strconv.Itoa(i), Namespace: "fixture-operator", Namespaced: true}})
	}
	return result
}

func refusalCatalog(packageName string) api.Catalog {
	environment := api.NewObject(api.Environment, "environment", api.Value{}, api.MapValue(api.FieldValue{Name: "controller", Value: api.MapValue(api.FieldValue{Name: "machineRef", Value: api.StringValue("controller")})}))
	spec := api.MapValue(api.FieldValue{Name: "clusterRef", Value: api.StringValue("cluster")})
	objects := []api.Object{environment}
	if packageName != "" {
		spec = spec.With("packages", api.MapValue(api.FieldValue{Name: packageName, Value: fixtureConfig()}))
	} else {
		spec = spec.With("addonRefs", api.StringList("custom"))
		objects = append(objects, api.NewObject(api.ClusterAddon, "custom", api.Value{}, api.MapValue()))
	}
	return api.NewCatalog(append(objects, api.NewObject(api.ClusterAddonBinding, "binding", api.Value{}, spec)))
}

func TestAddOnRefusalTableMatchesUnsupported(t *testing.T) {
	failure := errors.New("renderer failed")
	binding := "ClusterAddonBinding/binding"
	cases := map[string]struct {
		pkg    addons.Package
		values map[string]string
	}{
		"An authored add-on": {values: map[string]string{"<addon>": "ClusterAddon/custom", "<binding>": binding}},
		"An oversized instance": {pkg: syntheticPackage("oversized", func(addons.Instance) (addons.Rendered, error) {
			return addons.Rendered{Checks: manyChecks(33)}, nil
		}), values: map[string]string{"<package>": "oversized", "<cluster>": "cluster", "<binding>": binding, "<bound>": "32 checks"}},
		"A package that cannot be rendered": {pkg: syntheticPackage("broken", func(addons.Instance) (addons.Rendered, error) {
			return addons.Rendered{}, failure
		}), values: map[string]string{"<package>": "broken", "<cluster>": "cluster", "<binding>": binding, "<error>": failure.Error()}},
	}
	for _, row := range refusalTable(t) {
		test, found := cases[row.name]
		if !found {
			t.Errorf("the table row %q has no input here that it refuses", row.name)
			continue
		}
		delete(cases, row.name)
		t.Run(row.name, func(t *testing.T) {
			packages := []addons.Package{fixturePackage()}
			if test.pkg.Name != "" {
				packages = []addons.Package{test.pkg}
			}
			registry, err := addons.NewRegistry(packages)
			if err != nil {
				t.Fatal(err)
			}
			catalog := refusalCatalog(test.pkg.Name)
			facts := func(api.Catalog, string) addons.ClusterFacts {
				return addons.ClusterFacts{Name: "cluster", Release: "4.21.1"}
			}
			want := []lifecycle.Refusal{{Kind: "ClusterAddonBinding", Name: "binding", Reason: expand(row.reason, test.values), Remediation: expand(row.remedy, test.values)}}
			if got := New(nil, registry, facts).Unsupported(compilation.NewState(catalog, catalog, nil)); !reflect.DeepEqual(got, want) {
				t.Fatalf("the capability refuses %+v, the table %+v", got, want)
			}
		})
	}
	for _, name := range slices.Sorted(maps.Keys(cases)) {
		t.Errorf("the table has no row %q", name)
	}
}

func TestAnUnrenderablePackageIsRefusedNotReturnedBare(t *testing.T) {
	failure := errors.New("renderer failed")
	selection := addons.SelectedInstance{Package: syntheticPackage("broken", nil), Instance: addons.Instance{Cluster: "cluster", Binding: "binding"}}
	got := diagnostics.Of(unrenderableRefusal(selection, failure))
	if len(got) != 1 || got[0].Code != "lifecycle.state" || got[0].Message != "package broken cannot be rendered for cluster: renderer failed" || got[0].Remediation != "correct the configuration of package broken in ClusterAddonBinding/binding" {
		t.Fatalf("diagnostics = %+v", got)
	}
	registry, err := addons.NewRegistry([]addons.Package{syntheticPackage("broken", func(addons.Instance) (addons.Rendered, error) { return addons.Rendered{}, failure })})
	if err != nil {
		t.Fatal(err)
	}
	catalog := refusalCatalog("broken")
	facts := func(api.Catalog, string) addons.ClusterFacts {
		return addons.ClusterFacts{Name: "cluster", Release: "4.21.1"}
	}
	_, err = New(nil, registry, facts).Plan(context.Background(), lifecycle.PlanInput{Verb: reconciliation.Apply, State: compilation.NewState(catalog, catalog, nil), Controller: "controller"})
	if got := diagnostics.Of(err); len(got) != 1 || got[0].Code != "lifecycle.state" || got[0].Remediation == "" {
		t.Fatalf("plan error = %v, diagnostics = %+v", err, got)
	}
}
