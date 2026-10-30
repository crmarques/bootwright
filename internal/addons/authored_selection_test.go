package addons

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func authoredBinding(name string, addons []string, packages api.Value) api.Object {
	fields := []any{"clusterRef", s("cluster"), "addonRefs", api.StringList(addons...)}
	if packages.Present() {
		fields = append(fields, "packages", packages)
	}
	return obj(api.ClusterAddonBinding, name, m(fields...))
}

func olmPackage(name, namespace, olmPackage string) Package {
	p := olmFixture(name, Exact, "")
	p.OLM.Namespace, p.OLM.Package = namespace, olmPackage
	return p
}

func authoredWith(t *testing.T, name string, edit func(api.Value) api.Value) api.Object {
	return authoredDefinition(t, name, func(spec api.Value) api.Value {
		if edit == nil {
			return spec
		}
		return edit(spec)
	})
}

func TestAnAuthoredProviderOrdersInstancesByCapability(t *testing.T) {
	consumer := fixture("consumer")
	consumer.RequiresFor = func(Instance) []string { return []string{"nmstate"} }
	r := mustRegistry(t, consumer)
	provider := authoredWith(t, "netw", func(spec api.Value) api.Value { return spec.With("provides", api.StringList("nmstate")) })
	inert := manifestAddon("inert")
	binding := authoredBinding("binding", []string{"inert", "netw"}, m("consumer", m()))
	selected, issues := Instances(api.NewCatalog([]api.Object{binding, provider, inert}), r)
	if len(issues) != 0 || len(selected) != 2 {
		t.Fatal(selected, issues)
	}
	if selected[0].Package.Name != "netw" || selected[0].Package.Descriptor == "" || selected[0].Instance.Cluster != "cluster" || selected[0].Instance.Binding != "binding" || selected[0].Instance.Config.Get("subscription", "startingCSV").Text() != "example.v1.0.0" {
		t.Fatalf("%+v", selected[0])
	}
	if selected[1].Package.Name != "consumer" || !reflect.DeepEqual(selected[1].Providers, []Provider{{Binding: "binding", Package: "netw"}}) {
		t.Fatalf("%+v", selected[1])
	}
	requiring := authoredWith(t, "needs", func(spec api.Value) api.Value { return spec.With("requires", api.StringList("nmstate")) })
	selected, issues = Instances(api.NewCatalog([]api.Object{authoredBinding("binding", []string{"needs"}, api.Value{}), requiring, provider}), r)
	if len(selected) != 1 || len(issues) != 1 || issues[0].Issue.Field != "$.spec.addonRefs[0]" {
		t.Fatalf("%+v %+v", selected, issues)
	}
}

func TestNonExecutableAuthoredMembersKeepTheirAddonProviderAndNoInstance(t *testing.T) {
	consumer := fixture("consumer")
	consumer.RequiresFor = func(Instance) []string { return []string{"nmstate"} }
	inert := obj(api.ClusterAddon, "inert", m("manifestSet", m("manifests", manifests("manifests/a.yaml")), "provides", api.StringList("nmstate")))
	selected, issues := Instances(api.NewCatalog([]api.Object{authoredBinding("binding", []string{"inert"}, m("consumer", m())), inert}), mustRegistry(t, consumer))
	if len(issues) != 0 || len(selected) != 1 || !reflect.DeepEqual(selected[0].Providers, []Provider{{Binding: "binding", Addon: "inert"}}) {
		t.Fatalf("%+v %+v", selected, issues)
	}
}

func TestAnAuthoredAddOnNamedLikeABuiltInPackageIsRefusedAtItsSelectingEntry(t *testing.T) {
	r := mustRegistry(t, fixture("example"))
	catalog := api.NewCatalog([]api.Object{authoredBinding("binding", []string{"example"}, api.Value{}), authoredWith(t, "example", nil)})
	selected, issues := Instances(catalog, r)
	if len(selected) != 0 || len(issues) != 1 {
		t.Fatalf("%+v %+v", selected, issues)
	}
	want := SelectionIssue{Binding: "binding", Issue: api.Issue{Code: "api.invariant", Field: "$.spec.addonRefs[0]", Message: "ClusterAddon/example has the name of built-in package example", Remediation: "rename ClusterAddon/example; bootwright add-ons list names the built-in packages"}}
	if issues[0] != want {
		t.Fatalf("%+v", issues[0])
	}
	if issues := ValidateWithRegistry(authoredBinding("binding", []string{"example"}, api.Value{}), catalog, r, ClusterFacts{Name: "cluster"}); !hasIssue(issues, "api.invariant", "spec.addonRefs[0]") {
		t.Fatal(issues)
	}
	if _, issues := Instances(catalog, mustRegistry(t, fixture("other"))); len(issues) != 0 {
		t.Fatal(issues)
	}
}

func TestAnAuthoredAddOnSharesNoNamespacePackageOrCatalogOnItsCluster(t *testing.T) {
	builtinConfig := func(catalog string) api.Value {
		if catalog == "" {
			return m()
		}
		return m("subscription", m("catalogSource", m("name", s(catalog))))
	}
	for name, test := range map[string]struct {
		pkg     Package
		config  api.Value
		message string
		remedy  string
	}{
		"namespace": {olmPackage("other", "example-operator", "other"), m(), "ClusterAddon/example uses namespace example-operator, which package other also uses on OpenShiftCluster/cluster", "set olm.namespace.name of ClusterAddon/example to a namespace that no other package or add-on selected for the cluster uses"},
		"placement": {func() Package {
			p := olmPackage("other", "other-ns", "other")
			p.PlacementNamespaces = []string{"example-operator"}
			return p
		}(), m(), "ClusterAddon/example uses namespace example-operator, which package other also uses on OpenShiftCluster/cluster", "set olm.namespace.name of ClusterAddon/example to a namespace that no other package or add-on selected for the cluster uses"},
		"package": {olmPackage("other", "other-ns", "example"), m(), "ClusterAddon/example uses OLM package example, which package other also uses on OpenShiftCluster/cluster", "select one of ClusterAddon/example and package other for the cluster, because OLM package example installs once"},
		"catalog": {olmPackage("other", "other-ns", "other"), builtinConfig("example-catalog"), "ClusterAddon/example uses CatalogSource example-catalog, which package other also uses on OpenShiftCluster/cluster", "set olm.catalogSource.name of ClusterAddon/example to a CatalogSource name that no other package or add-on selected for the cluster uses"},
	} {
		t.Run(name, func(t *testing.T) {
			r := mustRegistry(t, test.pkg)
			catalog := api.NewCatalog([]api.Object{authoredBinding("binding", []string{"example"}, m("other", test.config)), authoredWith(t, "example", nil)})
			_, issues := Instances(catalog, r)
			if len(issues) != 1 || issues[0].Binding != "binding" || issues[0].Issue.Code != "api.invariant" || issues[0].Issue.Field != "$.spec.addonRefs[0]" || issues[0].Issue.Message != test.message || issues[0].Issue.Remediation != test.remedy {
				t.Fatalf("%+v", issues)
			}
		})
	}
	r := mustRegistry(t, olmPackage("other", "other-ns", "other"))
	if _, issues := Instances(api.NewCatalog([]api.Object{authoredBinding("binding", []string{"example"}, m("other", m())), authoredWith(t, "example", nil)}), r); len(issues) != 0 {
		t.Fatal(issues)
	}
}

func TestTwoAuthoredAddOnsReportASharedIdentityOnTheLaterOne(t *testing.T) {
	first, second := authoredWith(t, "first", nil), authoredWith(t, "second", nil)
	catalog := api.NewCatalog([]api.Object{authoredBinding("binding", []string{"first", "second"}, api.Value{}), first, second})
	_, issues := Instances(catalog, mustRegistry(t, fixture("builtin")))
	fields := []string{}
	for _, issue := range issues {
		fields = append(fields, issue.Issue.Field+" "+issue.Issue.Message)
	}
	want := []string{
		"$.spec.addonRefs[1] ClusterAddon/second uses namespace example-operator, which ClusterAddon/first also uses on OpenShiftCluster/cluster",
		"$.spec.addonRefs[1] ClusterAddon/second uses OLM package example, which ClusterAddon/first also uses on OpenShiftCluster/cluster",
		"$.spec.addonRefs[1] ClusterAddon/second uses CatalogSource example-catalog, which ClusterAddon/first also uses on OpenShiftCluster/cluster",
	}
	if !slices.Equal(fields, want) {
		t.Fatalf("%q", fields)
	}
	for _, issue := range issues {
		if strings.Contains(issue.Issue.Message, "first uses") {
			t.Fatal("the earlier member reported the shared identity too")
		}
	}
}
