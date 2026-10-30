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

func refusalCatalog(packageName string, addon api.Object) api.Catalog {
	environment := api.NewObject(api.Environment, "environment", api.Value{}, api.MapValue(api.FieldValue{Name: "controller", Value: api.MapValue(api.FieldValue{Name: "machineRef", Value: api.StringValue("controller")})}))
	spec := api.MapValue(api.FieldValue{Name: "clusterRef", Value: api.StringValue("cluster")})
	objects := []api.Object{environment}
	if packageName != "" {
		spec = spec.With("packages", api.MapValue(api.FieldValue{Name: packageName, Value: fixtureConfig()}))
	} else {
		spec = spec.With("addonRefs", api.StringList(addon.Name()))
		objects = append(objects, addon)
	}
	return api.NewCatalog(append(objects, api.NewObject(api.ClusterAddonBinding, "binding", api.Value{}, spec)))
}

type refusalCase struct {
	pkg    addons.Package
	addon  api.Object
	hosted []string
	values map[string]string
}

func authoredRefusalCases(t *testing.T) map[string]refusalCase {
	t.Helper()
	with := func(name string, value api.Value) func(api.Value) api.Value {
		return func(spec api.Value) api.Value { return spec.With(name, value) }
	}
	at := func(value api.Value, path ...string) func(api.Value) api.Value {
		return func(spec api.Value) api.Value { return spec.WithPath(value, path...) }
	}
	without := func(spec api.Value) api.Value { return spec.WithPath(spec.Get("olm").Without("operatorGroup"), "olm") }
	checks := []api.Value{}
	for i := range 33 {
		ref := api.MapValue(api.FieldValue{Name: "apiVersion", Value: api.StringValue("v1")}, api.FieldValue{Name: "kind", Value: api.StringValue("ConfigMap")}, api.FieldValue{Name: "namespace", Value: api.StringValue("widget-ns")}, api.FieldValue{Name: "name", Value: api.StringValue("check-" + strconv.Itoa(i))})
		checks = append(checks, api.MapValue(api.FieldValue{Name: "resourceExists", Value: ref}))
	}
	variant := func(change func(api.Value) api.Value) api.Object { return authoredVariant(t, "widget", change) }
	long := api.NewObject(api.ClusterAddon, strings.Repeat("w", addons.MaxPackageNameBytes+1), api.Value{}, executableAuthoredAddOn(t, "widget").Spec())
	_, synthesis := addons.Authored(long)
	if synthesis == nil {
		t.Fatal("a name over the package limit synthesized")
	}
	named := map[string]string{"<addon>": "ClusterAddon/widget", "<binding>": "ClusterAddonBinding/binding", "<cluster>": "cluster"}
	return map[string]refusalCase{
		"An authored manifest set":                             {addon: variant(with("manifestSet", api.MapValue())), values: named},
		"An authored add-on with steps":                        {addon: variant(with("steps", api.ListValue(api.MapValue()))), values: named},
		"An authored add-on with inputs":                       {addon: variant(with("inputs", api.ListValue(api.MapValue()))), values: named},
		"An authored external namespace":                       {addon: variant(at(api.StringValue("external"), "olm", "namespace", "management")), values: named},
		"An authored add-on with no OperatorGroup":             {addon: variant(without), values: named},
		"An authored OperatorGroup targeting other namespaces": {addon: variant(at(api.StringList("elsewhere"), "olm", "operatorGroup", "targetNamespaces")), values: named},
		"An oversized authored add-on":                         {addon: variant(at(api.ListValue(checks...), "readiness", "checks")), values: map[string]string{"<addon>": "ClusterAddon/widget", "<cluster>": "cluster", "<bound>": "32 checks"}},
		"An authored add-on that cannot be rendered":           {addon: long, values: map[string]string{"<addon>": long.Identity(), "<binding>": "ClusterAddonBinding/binding", "<cluster>": "cluster", "<error>": synthesis.Error()}},
		"An authored hosted namespace":                         {addon: variant(func(spec api.Value) api.Value { return spec }), hosted: []string{"widget-ns"}, values: map[string]string{"<addon>": "ClusterAddon/widget", "<cluster>": "cluster", "<namespace>": "widget-ns"}},
	}
}

func TestAddOnRefusalTableMatchesUnsupported(t *testing.T) {
	failure := errors.New("renderer failed")
	binding := "ClusterAddonBinding/binding"
	cases := authoredRefusalCases(t)
	cases["An oversized instance"] = refusalCase{pkg: syntheticPackage("oversized", func(addons.Instance) (addons.Rendered, error) {
		return addons.Rendered{Checks: manyChecks(33)}, nil
	}), values: map[string]string{"<package>": "oversized", "<cluster>": "cluster", "<binding>": binding, "<bound>": "32 checks"}}
	cases["A package that cannot be rendered"] = refusalCase{pkg: syntheticPackage("broken", func(addons.Instance) (addons.Rendered, error) {
		return addons.Rendered{}, failure
	}), values: map[string]string{"<package>": "broken", "<cluster>": "cluster", "<binding>": binding, "<error>": failure.Error()}}
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
			catalog := refusalCatalog(test.pkg.Name, test.addon)
			facts := func(api.Catalog, string) addons.ClusterFacts {
				hosted := []addons.HostedNamespace{}
				for _, name := range test.hosted {
					hosted = append(hosted, addons.HostedNamespace{Name: name})
				}
				return addons.ClusterFacts{Name: "cluster", Release: "4.21.1", HostedNamespaces: hosted}
			}
			want := []lifecycle.Refusal{{Kind: "ClusterAddonBinding", Name: "binding", Reason: expand(row.reason, test.values), Remediation: expand(row.remedy, test.values)}}
			if got := New(nil, registry, facts).Unsupported(compilation.NewState(catalog, catalog, nil)); !reflect.DeepEqual(got, want) {
				t.Fatalf("the capability refuses %+v, the table %+v", got, want)
			}
			if _, err := New(nil, registry, facts).Plan(context.Background(), lifecycle.PlanInput{Verb: reconciliation.Apply, State: compilation.NewState(catalog, catalog, nil), Controller: "controller"}); err == nil {
				t.Fatal("a refused selection planned")
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
	catalog := refusalCatalog("broken", api.Object{})
	facts := func(api.Catalog, string) addons.ClusterFacts {
		return addons.ClusterFacts{Name: "cluster", Release: "4.21.1"}
	}
	_, err = New(nil, registry, facts).Plan(context.Background(), lifecycle.PlanInput{Verb: reconciliation.Apply, State: compilation.NewState(catalog, catalog, nil), Controller: "controller"})
	if got := diagnostics.Of(err); len(got) != 1 || got[0].Code != "lifecycle.state" || got[0].Remediation == "" {
		t.Fatalf("plan error = %v, diagnostics = %+v", err, got)
	}
}
