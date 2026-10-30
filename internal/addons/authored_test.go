package addons

import (
	"slices"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons/clusterobjects"
)

func authoredDefinition(t *testing.T, name string, edit func(api.Value) api.Value) api.Object {
	t.Helper()
	spec := executableSpec().WithPath(m(), "olm", "operatorGroup")
	if edit != nil {
		spec = edit(spec)
	}
	o, issues := Normalize(obj(api.ClusterAddon, name, spec), api.NewCatalog(nil))
	if len(issues) != 0 {
		t.Fatal(issues)
	}
	return o
}

func TestAuthoredSynthesizesAPackageFromTheDefinition(t *testing.T) {
	o := authoredDefinition(t, "example", func(spec api.Value) api.Value {
		spec = spec.With("provides", api.StringList("nmstate", "kubevirt")).With("requires", api.StringList("metallb"))
		spec = spec.WithPath(m("example.test/team", s("platform")), "olm", "namespace", "labels")
		return spec.WithPath(l(
			m("csvSucceeded", m("namespace", s("example-operator"), "subscription", s("example"))),
			m("condition", m("apiVersion", s("example.test/v1"), "kind", s("Widget"), "name", s("example"), "namespace", s("example-operator"), "condition", m("type", s("Ready"), "status", s("True")))),
		), "readiness", "checks")
	})
	p, err := Authored(o)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "example" || p.Summary != authoredSummary || !validSummary(p.Summary) || p.Last || len(p.PlacementNamespaces) != 0 || p.Consumes != nil || p.Budget != 45*time.Minute {
		t.Fatalf("%+v", p)
	}
	if !slices.Equal(p.Provides, []string{"kubevirt", "nmstate"}) || !slices.Equal(p.Requires, []string{"metallb"}) {
		t.Fatalf("%v %v", p.Provides, p.Requires)
	}
	want := OLM{Namespace: "example-operator", NamespaceLabels: map[string]string{"example.test/team": "platform"}, OperatorGroup: OwnNamespace, GroupName: "example-operator", Subscription: "example", Package: "example", DefaultSource: "example-catalog", Approval: Exact}
	if p.OLM == nil || p.OLM.Namespace != want.Namespace || p.OLM.OperatorGroup != want.OperatorGroup || p.OLM.GroupName != want.GroupName || p.OLM.Subscription != want.Subscription || p.OLM.Package != want.Package || p.OLM.DefaultSource != want.DefaultSource || p.OLM.Approval != Exact || p.OLM.OperatorManagedReason != "" || p.OLM.NamespaceLabels["example.test/team"] != "platform" {
		t.Fatalf("%+v", p.OLM)
	}
	digest, err := DescriptorDigest(o.Spec())
	if err != nil || p.Descriptor != digest || len(digest) != 64 || strings.ToLower(digest) != digest {
		t.Fatalf("descriptor %q: %v", p.Descriptor, err)
	}
	rendered, err := p.Render(Instance{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rendered.Before) != 0 || len(rendered.After) != 1 || rendered.SubscriptionConfig != nil || len(rendered.Requires) != 0 {
		t.Fatalf("%+v", rendered)
	}
	phase := rendered.After[0]
	crd := clusterobjects.Ref{APIVersion: "apiextensions.k8s.io/v1", Kind: "CustomResourceDefinition", Name: "widgets.example.test"}
	if len(phase.Gate) != 1 || phase.Gate[0].ResourceExists == nil || *phase.Gate[0].ResourceExists != crd || len(phase.Secrets) != 0 || len(phase.Approvals) != 0 {
		t.Fatalf("%+v", phase)
	}
	if len(phase.Objects) != 1 || phase.Objects[0].Ref != (clusterobjects.Ref{APIVersion: "example.test/v1", Kind: "Widget", Namespace: "example-operator", Name: "example", Namespaced: true}) {
		t.Fatalf("%+v", phase.Objects)
	}
	if len(rendered.Checks) != 1 || rendered.Checks[0].CSVSucceeded != nil || rendered.Checks[0].Condition == nil || rendered.Checks[0].Condition.Type != "Ready" {
		t.Fatalf("authored csvSucceeded must be dropped beside the pinned one subscribe prepends: %+v", rendered.Checks)
	}
	config := authoredConfig(o, p.Budget)
	if config.Get("timeout").Text() != "45m0s" || config.Get("subscription", "startingCSV").Text() != "example.v1.0.0" || config.Get("subscription", "catalogSource", "priority").Text() != "100" || config.Get("subscription", "catalogSource", "securityContextConfig").Text() != "restricted" || config.Get("subscription", "sourceNamespace").Text() != "openshift-marketplace" {
		t.Fatalf("%v", config)
	}
}

func TestAuthoredDefaultReadinessLeavesOnlyThePinnedCSVCheck(t *testing.T) {
	p, err := Authored(authoredDefinition(t, "example", nil))
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := p.Render(Instance{})
	if err != nil || len(rendered.Checks) != 0 {
		t.Fatalf("%+v %v", rendered, err)
	}
}

func TestAuthoredGroupShapesAndNonExecutableShapes(t *testing.T) {
	shape := func(edit func(api.Value) api.Value) api.Object { return authoredDefinition(t, "example", edit) }
	all := shape(func(spec api.Value) api.Value {
		return spec.WithPath(api.ListValue(), "olm", "operatorGroup", "targetNamespaces")
	})
	if p, err := Authored(all); err != nil || p.OLM.OperatorGroup != AllNamespaces {
		t.Fatalf("%+v %v", p.OLM, err)
	}
	for name, test := range map[string]struct {
		edit   func(api.Value) api.Value
		reason string
	}{
		"manifest set": {func(spec api.Value) api.Value {
			return spec.With("manifestSet", m("manifests", manifests("manifests/a.yaml")))
		}, "manifest set"},
		"steps": {func(spec api.Value) api.Value {
			return spec.With("steps", l(m("name", s("one"), "manifests", manifests("manifests/a.yaml"))))
		}, "steps"},
		"inputs": {func(spec api.Value) api.Value {
			return spec.With("inputs", l(m("name", s("one"), "resourceKind", s("Machine"))))
		}, "inputs"},
		"external": {func(spec api.Value) api.Value { return spec.WithPath(s("external"), "olm", "namespace", "management") }, "external"},
		"no group": {func(spec api.Value) api.Value { return spec.With("olm", spec.Get("olm").Without("operatorGroup")) }, "no OperatorGroup"},
		"other target": {func(spec api.Value) api.Value {
			return spec.WithPath(api.StringList("elsewhere"), "olm", "operatorGroup", "targetNamespaces")
		}, "other namespaces"},
		"two targets": {func(spec api.Value) api.Value {
			return spec.WithPath(api.StringList("example-operator", "elsewhere"), "olm", "operatorGroup", "targetNamespaces")
		}, "other namespaces"},
		"no olm": {func(spec api.Value) api.Value { return m("manifestSet", m("manifests", manifests("manifests/a.yaml"))) }, "no olm arm"},
	} {
		t.Run(name, func(t *testing.T) {
			o, _ := Normalize(obj(api.ClusterAddon, "example", test.edit(executableSpec().WithPath(m(), "olm", "operatorGroup"))), api.NewCatalog(nil))
			if _, err := Authored(o); err == nil || !strings.Contains(err.Error(), test.reason) || ExecutableAuthored(o) {
				t.Fatalf("%v", err)
			}
		})
	}
	long := authoredDefinition(t, strings.Repeat("a", MaxPackageNameBytes+1), nil)
	if _, err := Authored(long); err == nil || ExecutableAuthored(long) {
		t.Fatal("name over the package limit synthesized")
	}
	if !ExecutableAuthored(authoredDefinition(t, strings.Repeat("a", MaxPackageNameBytes), nil)) {
		t.Fatal("name at the package limit refused")
	}
}

func TestAuthoredNameOverTheLimitIsRefusedAtAdmission(t *testing.T) {
	long := authoredDefinition(t, strings.Repeat("a", MaxPackageNameBytes+1), nil)
	issues := Validate(long, api.NewCatalog(nil))
	if len(issues) != 1 || issues[0].Code != "api.invariant" || issues[0].Field != "$.metadata.name" || issues[0].Remediation == "" {
		t.Fatal(issues)
	}
	manifest := obj(api.ClusterAddon, strings.Repeat("a", MaxPackageNameBytes+1), m("manifestSet", m("manifests", manifests("manifests/a.yaml"))))
	if issues := Validate(manifest, api.NewCatalog(nil)); len(issues) != 0 {
		t.Fatalf("a non-executable shape keeps its long name: %v", issues)
	}
}

func TestAuthoredDescriptorFollowsTheNormalizedSpec(t *testing.T) {
	digest := func(o api.Object) string {
		t.Helper()
		p, err := Authored(o)
		if err != nil {
			t.Fatal(err)
		}
		return p.Descriptor
	}
	base := authoredDefinition(t, "example", func(spec api.Value) api.Value { return spec.With("provides", api.StringList("nmstate", "kubevirt")) })
	reordered := authoredDefinition(t, "example", func(spec api.Value) api.Value {
		spec = spec.With("provides", api.StringList("kubevirt", "nmstate"))
		fields := spec.Fields()
		slices.Reverse(fields)
		return api.MapValue(fields...)
	})
	if digest(base) != digest(reordered) {
		t.Fatal("descriptor moved with input ordering")
	}
	if digest(base) != digest(authoredDefinition(t, "renamed", func(spec api.Value) api.Value { return spec.With("provides", api.StringList("nmstate", "kubevirt")) })) {
		t.Fatal("the descriptor names the spec, and the request carries the name")
	}
	edits := map[string]func(api.Value) api.Value{
		"timeout": func(spec api.Value) api.Value { return spec.WithPath(s("46m"), "readiness", "timeout") },
		"channel": func(spec api.Value) api.Value { return spec.WithPath(s("beta"), "olm", "subscription", "channel") },
		"csv": func(spec api.Value) api.Value {
			return spec.WithPath(s("example.v1.0.1"), "olm", "subscription", "startingCSV")
		},
		"image": func(spec api.Value) api.Value {
			return spec.WithPath(s("registry.example.test/catalog:v2"), "olm", "catalogSource", "image")
		},
		"priority": func(spec api.Value) api.Value {
			return spec.WithPath(api.IntegerValue("101"), "olm", "catalogSource", "priority")
		},
		"label": func(spec api.Value) api.Value {
			return spec.WithPath(m("example.test/a", s("b")), "olm", "namespace", "labels")
		},
		"requires": func(spec api.Value) api.Value { return spec.With("requires", api.StringList("metallb")) },
		"resource": func(spec api.Value) api.Value {
			item := spec.Get("olm", "customResources").Items()[0]
			return spec.WithPath(l(item.With("resource", item.Get("resource").WithPath(api.IntegerValue("185"), "spec", "count"))), "olm", "customResources")
		},
		"all-group": func(spec api.Value) api.Value {
			return spec.WithPath(api.ListValue(), "olm", "operatorGroup", "targetNamespaces")
		},
	}
	seen := map[string]string{digest(base): "base"}
	for name, edit := range edits {
		d := digest(authoredDefinition(t, "example", func(spec api.Value) api.Value {
			return edit(spec.With("provides", api.StringList("nmstate", "kubevirt")))
		}))
		if previous, ok := seen[d]; ok {
			t.Fatalf("edit %s leaves the descriptor of %s", name, previous)
		}
		seen[d] = name
	}
}

func TestRegistryRefusesABuiltInPackageWithADescriptor(t *testing.T) {
	p := fixture("builtin")
	if _, err := NewRegistry([]Package{p}); err != nil {
		t.Fatal(err)
	}
	p.Descriptor = strings.Repeat("0", 64)
	if _, err := NewRegistry([]Package{p}); err == nil || !strings.Contains(err.Error(), "descriptor") {
		t.Fatalf("a built-in package with a descriptor was admitted: %v", err)
	}
	authored, err := Authored(authoredDefinition(t, "example", nil))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewRegistry([]Package{authored}); err == nil {
		t.Fatal("a synthesized authored package entered the built-in registry")
	}
}
