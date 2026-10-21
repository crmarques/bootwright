package addons

import (
	"reflect"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func fixture(name string) Package {
	return Package{Name: name, Budget: time.Minute, Render: func(Instance) (Rendered, error) { return Rendered{}, nil }}
}
func packageBinding(name string, configs api.Value) api.Object {
	return obj(api.ClusterAddonBinding, name, m("clusterRef", s("cluster"), "packages", configs))
}
func TestRegistrySelectionDependencyOrderAndPurity(t *testing.T) {
	first := fixture("first")
	first.Provides = []string{"nmstate"}
	second := fixture("second")
	second.RequiresFor = func(Instance) []string { return []string{"nmstate"} }
	last := fixture("last")
	last.Last = true
	registry, err := NewRegistry([]Package{last, second, first})
	if err != nil {
		t.Fatal(err)
	}
	b := packageBinding("binding", m("second", m(), "last", m(), "first", m()))
	b, _ = NormalizePackages(b, registry)
	again, _ := NormalizePackages(b, registry)
	if !b.Value().Equal(again.Value()) {
		t.Fatal("normalization not idempotent")
	}
	selected, issues := Instances(api.NewCatalog([]api.Object{b}), registry)
	if len(issues) > 0 {
		t.Fatal(issues)
	}
	names := []string{}
	for _, i := range selected {
		names = append(names, i.Package.Name)
	}
	if !reflect.DeepEqual(names, []string{"first", "second", "last"}) {
		t.Fatal(names)
	}
	if selected[1].Providers[0].Package != "first" {
		t.Fatal(selected[1])
	}
	missing := b.WithSpec(b.Spec().With("packages", b.Spec().Get("packages").Without("first")))
	_, issues = Instances(api.NewCatalog([]api.Object{missing}), registry)
	if len(issues) != 1 || issues[0].Issue.Field != "$.spec.packages.second" || !strings.Contains(issues[0].Issue.Message, "nmstate") {
		t.Fatal(issues)
	}
}
func TestRegistryDuplicateLastAndCycleDiagnostics(t *testing.T) {
	a := fixture("a")
	a.Provides = []string{"nmstate"}
	a.Requires = []string{"metallb"}
	b := fixture("b")
	b.Provides = []string{"metallb"}
	b.Requires = []string{"nmstate"}
	r, err := NewRegistry([]Package{a, b})
	if err != nil {
		t.Fatal(err)
	}
	one := packageBinding("one", m("a", m()))
	two := packageBinding("two", m("b", m()))
	_, issues := Instances(api.NewCatalog([]api.Object{two, one}), r)
	if len(issues) != 1 || issues[0].Binding != "one" || issues[0].Issue.Code != "api.invariant" {
		t.Fatal(issues)
	}
	duplicate := packageBinding("two", m("a", m()))
	_, issues = Instances(api.NewCatalog([]api.Object{duplicate, one}), r)
	if !strings.Contains(issues[0].Issue.Message, "ClusterAddonBinding/one") || issues[0].Binding != "two" {
		t.Fatal(issues)
	}
	a = fixture("a")
	b = fixture("b")
	a.Last = true
	b.Last = true
	r, _ = NewRegistry([]Package{a, b})
	_, issues = Instances(api.NewCatalog([]api.Object{one, two}), r)
	if len(issues) < 1 || !strings.Contains(issues[0].Issue.Message, "both run last") {
		t.Fatal(issues)
	}
}
func TestSeveralMissingInputsAndCapabilitiesRemainDistinct(t *testing.T) {
	a := manifestAddon("consumer").WithSpec(m("inputs", l(m("name", s("one"), "resourceKind", s("Machine")), m("name", s("two"), "resourceKind", s("Machine"))), "requires", api.StringList("alpha", "beta")))
	b := obj(api.ClusterAddonBinding, "binding", m("clusterRef", s("cluster"), "addonRefs", api.StringList("consumer"), "addonConfigs", l(m("addonRef", s("consumer")))))
	issues := Validate(b, api.NewCatalog([]api.Object{a, b}))
	if len(issues) != 4 {
		t.Fatal(issues)
	}
	seen := map[string]bool{}
	for _, i := range issues {
		if seen[i.Message] || i.Remediation == "" {
			t.Fatal(issues)
		}
		seen[i.Message] = true
		if i.Field != "$.spec.addonConfigs[0].inputs" && i.Field != "$.spec.addonRefs[0]" {
			t.Fatal(i)
		}
	}
}
func TestPackageAdmissionIndexedHookAndTimeout(t *testing.T) {
	p := fixture("fixture")
	p.Admit = func(Instance, ClusterFacts) []api.Issue {
		return []api.Issue{{Code: "api.invariant", Field: "items[1]", Message: "invalid item", Remediation: "choose another item"}}
	}
	r, _ := NewRegistry([]Package{p})
	b := packageBinding("binding", m("fixture", m("timeout", s("6h"))))
	issues := ValidateWithRegistry(b, api.NewCatalog([]api.Object{b}), r, ClusterFacts{Name: "cluster"})
	if len(issues) != 2 || issues[1].Field != "$.spec.packages.fixture.items[1]" {
		t.Fatal(issues)
	}
}

func TestRegistryConformance(t *testing.T) {
	unread := fixture("unread")
	unread.Provides = []string{"no-reader"}
	if _, err := NewRegistry([]Package{unread}); err == nil {
		t.Fatal("unread token admitted")
	}
	last := fixture("last")
	last.Last = true
	last.Provides = []string{"nmstate"}
	if _, err := NewRegistry([]Package{last}); err == nil {
		t.Fatal("last package providing capability admitted")
	}
	provider := fixture("provider")
	provider.Provides = []string{"custom"}
	consumer := fixture("consumer")
	consumer.Requires = []string{"custom"}
	if _, err := NewRegistry([]Package{provider, consumer}); err != nil {
		t.Fatal(err)
	}
}
func TestCycleDiagnosticExcludesBlockedDependents(t *testing.T) {
	a := fixture("a")
	a.Provides = []string{"nmstate"}
	a.Requires = []string{"metallb"}
	b := fixture("b")
	b.Provides = []string{"metallb"}
	b.Requires = []string{"nmstate"}
	downstream := fixture("down")
	downstream.Requires = []string{"nmstate"}
	r, _ := NewRegistry([]Package{a, b, downstream})
	binding := packageBinding("binding", m("a", m(), "b", m(), "down", m()))
	_, issues := Instances(api.NewCatalog([]api.Object{binding}), r)
	if len(issues) != 1 || strings.Contains(issues[0].Issue.Message, "down") {
		t.Fatal(issues)
	}
}

func TestRegistryDynamicReaderExamplesAndDuplicateOLMPackage(t *testing.T) {
	provider := fixture("provider")
	provider.Provides = []string{"conditional"}
	consumer := fixture("consumer")
	consumer.RequiresFor = func(i Instance) []string {
		if i.Config.Get("enabled").Bool() {
			return []string{"conditional"}
		}
		return nil
	}
	if _, err := NewRegistry([]Package{provider, consumer}); err == nil {
		t.Fatal("unproved dynamic reader admitted")
	}
	r, err := NewRegistryWithExamples([]Package{provider, consumer}, map[string][]api.Value{"consumer": {m("enabled", api.BoolValue(true))}})
	if err != nil {
		t.Fatal(err)
	}
	unread := fixture("unread")
	unread.Provides = []string{"unrelated"}
	if _, err := NewRegistryWithExamples([]Package{provider, consumer, unread}, map[string][]api.Value{"consumer": {m("enabled", api.BoolValue(true))}}); err == nil {
		t.Fatal("unrelated unread token admitted")
	}
	b := packageBinding("binding", m("consumer", m("enabled", api.BoolValue(true)), "provider", m()))
	instances, issues := Instances(api.NewCatalog([]api.Object{b}), r)
	if len(issues) != 0 || len(instances) != 2 || instances[0].Package.Name != "provider" {
		t.Fatal(instances, issues)
	}
	b = b.WithSpec(b.Spec().With("packages", b.Spec().Get("packages").Without("provider")))
	_, issues = Instances(api.NewCatalog([]api.Object{b}), r)
	if len(issues) != 1 || !strings.Contains(issues[0].Issue.Message, "conditional") {
		t.Fatal(issues)
	}
	a := fixture("a")
	a.OLM = &OLM{Namespace: "namespace-a", Package: "same-operator"}
	bPackage := fixture("b")
	bPackage.OLM = &OLM{Namespace: "namespace-b", Package: "same-operator"}
	if _, err := NewRegistry([]Package{a, bPackage}); err == nil {
		t.Fatal("duplicate OLM package admitted")
	}
}
