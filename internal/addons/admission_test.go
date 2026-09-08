package addons

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestNormalizationPreservesPresenceAndOrder(t *testing.T) {
	base := m("olm", m("namespace", m("name", s("operators")), "subscription", m("package", s("example-operator"), "channel", s("stable")), "catalogSource", m("name", s("example-catalog"), "image", s("registry.example.test/catalog:v1"))), "provides", api.StringList("z", "a"), "inputs", l(m("name", s("z"), "resourceKind", s("Machine"), "required", api.BoolValue(false)), m("name", s("a"), "resourceKind", s("Machine"))), "steps", l(m("name", s("later"), "follows", s("ready"), "manifests", manifests("manifests/later.yaml")), m("name", s("earlier"), "gates", s("apply"), "manifests", manifests("manifests/earlier.yaml"))))
	object := obj(api.ClusterAddon, "example", base)
	normalized, issues := Normalize(object, api.NewCatalog(nil))
	if len(issues) != 0 {
		t.Fatal(issues)
	}
	got := normalized.Spec()
	if !object.Spec().Equal(base) || got.Has("olm", "operatorGroup") || object.Spec().Has("readiness") {
		t.Fatal("normalization changed authored presence or created an omitted OperatorGroup")
	}
	if got.Get("olm", "namespace", "management").Text() != "managed" || got.Get("olm", "subscription", "name").Text() != "example-operator" || got.Get("olm", "subscription", "source").Text() != "example-catalog" || got.Get("olm", "subscription", "sourceNamespace").Text() != "openshift-marketplace" || got.Get("olm", "subscription", "installPlanApproval").Text() != "Automatic" {
		t.Fatal("OLM defaults were not resolved")
	}
	if got.Get("readiness", "timeout").Text() != "30m" || got.Get("readiness", "checks").Len() != 1 || got.Get("readiness", "checks").Items()[0].Get("csvSucceeded", "subscription").Text() != "example-operator" {
		t.Fatal("OLM readiness default did not use the effective subscription")
	}
	if !slices.Equal(got.Get("provides").Strings(), []string{"a", "z"}) || got.Get("inputs").Items()[0].Get("name").Text() != "a" || !got.Get("inputs").Items()[0].Get("required").Bool() || got.Get("inputs").Items()[1].Get("required").Bool() {
		t.Fatal("set defaults or explicit false changed")
	}
	if got.Get("steps").Items()[0].Get("name").Text() != "later" || got.Get("steps").Items()[0].Has("timeout") || got.Get("steps").Items()[0].Has("target") {
		t.Fatal("ordered steps or execution-only defaults changed")
	}
	again, _ := Normalize(normalized, api.NewCatalog(nil))
	if !again.Value().Equal(normalized.Value()) {
		t.Fatal("normalization is not idempotent")
	}

	explicit := base.WithPath(m("targetNamespaces", l()), "olm", "operatorGroup").With("readiness", m("checks", l()))
	normalized, _ = Normalize(object.WithSpec(explicit), api.NewCatalog(nil))
	if normalized.Spec().Get("readiness", "checks").Len() != 0 || normalized.Spec().Get("olm", "operatorGroup", "targetNamespaces").Len() != 0 || normalized.Spec().Get("olm", "operatorGroup", "name").Text() != "operators" {
		t.Fatal("explicit empty arrays were replaced")
	}
	if !hasCode(Validate(normalized, api.NewCatalog(nil)), "api.invariant") {
		t.Fatal("capability with explicit empty readiness was accepted")
	}
}

func TestAddonSemanticFailures(t *testing.T) {
	base := manifestAddon("example").Spec()
	step := m("name", s("configure"), "gates", s("apply"), "playbook", s("playbooks/configure.yaml"), "target", m("boundCluster", m()))
	cases := []struct {
		name string
		spec api.Value
	}{
		{"manifest escape", base.With("manifestSet", m("manifests", manifests("../manifests/a.yaml")))},
		{"manifest segment", base.With("manifestSet", m("manifests", manifests("payload/a.yaml")))},
		{"manifest suffix", base.With("manifestSet", m("manifests", manifests("manifests/a.json")))},
		{"capability readiness", base.With("provides", api.StringList("customCapability"))},
		{"storage effect kind", base.With("inputs", l(m("name", s("input"), "resourceKind", s("Machine"), "effects", l(m("storageExportAttachment", m())))))},
		{"storage effect capability", base.With("inputs", l(storageInput("export")))},
		{"pull-secret type", base.With("inputs", l(m("name", s("credential"), "secretType", s("opaque"), "effects", l(m("globalPullSecretMerge", m("registry", s("registry.example.test"), "username", s("reader")))))))},
		{"empty step", base.With("steps", l(m("name", s("empty"), "gates", s("apply"))))},
		{"missing target", base.With("steps", l(step.Without("target")))},
		{"manifest-only target", base.With("steps", l(step.Without("playbook").With("manifests", manifests("manifests/a.yaml"))))},
		{"manifest-only outputs", base.With("steps", l(step.Without("playbook").Without("target").With("manifests", manifests("manifests/a.yaml")).With("outputs", l())))},
		{"OLM-only anchor", base.With("steps", l(step.Without("gates").With("follows", s("operatorReady"))))},
		{"Git source", base.With("steps", l(step.With("source", m("git", m("url", s("https://git.example.test/repository"), "ref", s("main"))))))},
		{"secret hash output", base.With("steps", l(step.With("outputs", l(m("name", s("result"), "file", s("result.txt"), "secret", api.BoolValue(true), "format", s("sha256"))))))},
		{"unsupported target input", base.With("inputs", l(m("name", s("input"), "resourceKind", s("NetworkConfig")))).With("steps", l(step.With("target", m("fromInput", m("input", s("input"))))))},
		{"storage target without effect", base.With("inputs", l(storageInput("input").Without("effects"))).With("steps", l(step.With("target", m("fromInput", m("input", s("input"))))))},
		{"empty static target", base.With("steps", l(step.With("target", m("static", m()))))},
		{"connection variables", base.With("steps", l(step.With("extraVars", m("ansible_connection", s("local")))))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o, _ := Normalize(obj(api.ClusterAddon, "example", tc.spec), api.NewCatalog(nil))
			if issues := Validate(o, api.NewCatalog(nil)); len(issues) == 0 {
				t.Fatal("invalid declaration accepted")
			}
		})
	}
}

func TestOLMCustomResourcesAndDefaults(t *testing.T) {
	base := m("olm", m("namespace", m("name", s("operators")), "subscription", m("package", s("example"), "channel", s("stable"), "source", s("catalog")), "customResources", l(m("apiVersion", s("example.test/v1"), "kind", s("Widget"), "metadata", m("name", s("example")), "spec", m("count", api.IntegerValue("184467440737095516160"), "ratio", api.NumberValue("1.25"))))))
	o, _ := Normalize(obj(api.ClusterAddon, "example", base), api.NewCatalog(nil))
	if issues := Validate(o, api.NewCatalog(nil)); len(issues) != 0 {
		t.Fatal(issues)
	}
	resource := o.Spec().Get("olm", "customResources").Items()[0]
	if resource.Get("spec", "count").Text() != "184467440737095516160" || resource.Get("spec", "ratio").Text() != "1.25" {
		t.Fatal("native numeric values changed")
	}
	for _, payload := range []api.Value{
		m("apiVersion", s("v1"), "kind", s("Secret"), "metadata", m("name", s("example")), "data", m()),
		m("apiVersion", s("v1"), "kind", s("Secret"), "metadata", m("name", s("example")), "stringData", m("password", s("synthetic-private-marker"))),
		m("apiVersion", s("v1"), "kind", s("ConfigMap"), "metadata", m("name", api.IntegerValue("1"))),
	} {
		bad := o.WithSpec(o.Spec().WithPath(l(payload), "olm", "customResources"))
		issues := Validate(bad, api.NewCatalog(nil))
		if len(issues) == 0 || strings.Contains(fmt.Sprint(issues), "synthetic-private-marker") {
			t.Fatal("unsafe native resource validation")
		}
	}
	for _, labels := range []api.Value{m("invalid key", s("value")), m("example.test/key", s(strings.Repeat("x", 64)))} {
		if len(Validate(o.WithSpec(o.Spec().WithPath(labels, "olm", "namespace", "labels")), api.NewCatalog(nil))) == 0 {
			t.Fatal("invalid Kubernetes namespace label accepted")
		}
	}
	if issues := Validate(o.WithSpec(o.Spec().WithPath(m("example.test/key", s("")), "olm", "namespace", "labels")), api.NewCatalog(nil)); len(issues) != 0 {
		t.Fatal(issues)
	}
}

func TestStaticMachineChecksAfterNormalization(t *testing.T) {
	step := m("name", s("configure"), "gates", s("apply"), "playbook", s("playbooks/configure.yaml"), "target", m("static", m("machines", api.StringList("node"))))
	o := manifestAddon("example").WithSpec(manifestAddon("example").Spec().With("steps", l(step)))
	machine := obj(api.Machine, "node", m())
	if issues := ValidateAuthored(o, api.NewCatalog([]api.Object{machine})); len(issues) != 0 {
		t.Fatal("authored pass rejected access that can be defaulted", issues)
	}
	if !hasCode(Validate(o, api.NewCatalog([]api.Object{machine})), "api.reference") {
		t.Fatal("non-SSH Machine accepted")
	}
	machine = machine.WithSpec(m("access", m("ssh", m("host", s("node.example.test")))))
	if issues := Validate(o, api.NewCatalog([]api.Object{machine})); len(issues) != 0 {
		t.Fatal(issues)
	}
}

func TestProfileExpansionOrderAndCycles(t *testing.T) {
	a, b, c := manifestAddon("a"), manifestAddon("b"), manifestAddon("c")
	leaf := obj(api.ClusterAddonProfile, "leaf", m("addonRefs", api.StringList("b", "a")))
	left := obj(api.ClusterAddonProfile, "left", m("profileRefs", api.StringList("leaf"), "addonRefs", api.StringList("c")))
	right := obj(api.ClusterAddonProfile, "right", m("profileRefs", api.StringList("leaf"), "addonRefs", api.StringList("a")))
	binding := obj(api.ClusterAddonBinding, "binding", m("clusterRef", s("cluster"), "profileRefs", api.StringList("left", "right"), "addonRefs", api.StringList("c", "a")))
	catalog := []api.Object{a, b, c, leaf, left, right, binding}
	want := []string{"b", "a", "c"}
	for range 3 {
		expanded, issues := ExpandBinding(binding, api.NewCatalog(catalog))
		if len(issues) != 0 || !slices.Equal(names(expanded), want) {
			t.Fatalf("expansion = %v, %v", names(expanded), issues)
		}
		slices.Reverse(catalog)
	}
	cycle := leaf.WithSpec(leaf.Spec().With("profileRefs", api.StringList("left")))
	if _, issues := ExpandBinding(binding, api.NewCatalog([]api.Object{a, b, c, cycle, left, right})); !hasCode(issues, "api.invariant") {
		t.Fatal("profile cycle accepted")
	}
	if _, issues := ExpandBinding(binding, api.NewCatalog([]api.Object{a, b, c, left, right})); !hasCode(issues, "api.reference") {
		t.Fatal("missing profile accepted")
	}
}

func TestIterativeProfileDAG(t *testing.T) {
	const depth = 2500
	objects := []api.Object{manifestAddon("leaf")}
	for i := 0; i < depth; i++ {
		spec := m("addonRefs", api.StringList("leaf"))
		if i > 0 {
			spec = spec.With("profileRefs", api.StringList(fmt.Sprintf("p-%d", i-1), fmt.Sprintf("p-%d", i-1)))
		}
		objects = append(objects, obj(api.ClusterAddonProfile, fmt.Sprintf("p-%d", i), spec))
	}
	binding := obj(api.ClusterAddonBinding, "binding", m("profileRefs", api.StringList(fmt.Sprintf("p-%d", depth-1))))
	expanded, issues := ExpandBinding(binding, api.NewCatalog(objects))
	if len(issues) != 0 || !slices.Equal(names(expanded), []string{"leaf"}) {
		t.Fatal("shared deep profile DAG failed", issues)
	}
}

func TestTypedBindingInputsAndStorageAttachments(t *testing.T) {
	input := storageInput("arbitrary-label")
	addon := manifestAddon("storage").WithSpec(manifestAddon("storage").Spec().With("provides", api.StringList("dataFoundation")).With("inputs", l(input, m("name", s("optional"), "secretType", s("token"), "required", api.BoolValue(false)))))
	export := obj(api.StorageExport, "export", m())
	binding := obj(api.ClusterAddonBinding, "binding", m("clusterRef", s("cluster"), "addonRefs", api.StringList("storage"), "addonConfigs", l(m("addonRef", s("storage"), "inputs", l(m("name", s("arbitrary-label"), "value", s("export")))))))
	catalog := api.NewCatalog([]api.Object{addon, export, binding})
	if issues := Validate(binding, catalog); len(issues) != 0 {
		t.Fatal(issues)
	}
	if got := StorageAttachments(catalog); !reflect.DeepEqual(got, []Attachment{{ClusterRef: "cluster", ExportRef: "export"}}) {
		t.Fatalf("attachments = %v", got)
	}
	for _, altered := range []api.Object{
		addon.WithSpec(addon.Spec().Without("provides")),
		addon.WithSpec(addon.Spec().With("inputs", l(input.Without("effects")))),
		addon.WithSpec(addon.Spec().With("inputs", l(input.With("resourceKind", s("Machine"))))),
		addon.WithSpec(addon.Spec().With("inputs", l(input.With("secretType", s("token"))))),
		addon.WithSpec(addon.Spec().With("inputs", l(input, input))),
	} {
		if got := StorageAttachments(api.NewCatalog([]api.Object{altered, export, binding})); len(got) != 0 {
			t.Fatal("undeclared or invalid storage relationship inferred", got)
		}
	}
	badConfig := m("addonRef", s("storage"), "inputs", l(m("name", s("optional"), "value", s("credential"))))
	badBinding := binding.WithSpec(binding.Spec().With("addonConfigs", l(badConfig)))
	credential := obj(api.Secret, "credential", m("type", s("opaque")))
	issues := Validate(badBinding, api.NewCatalog([]api.Object{addon, export, credential, badBinding}))
	if !hasCode(issues, "api.required") || !hasCode(issues, "api.reference") {
		t.Fatal("required input or Secret type mismatch accepted", issues)
	}
	configOnly := binding.WithSpec(binding.Spec().With("addonRefs", api.StringList("other")))
	if !hasCode(Validate(configOnly, api.NewCatalog([]api.Object{addon, export, manifestAddon("other"), configOnly})), "api.reference") || len(StorageAttachments(api.NewCatalog([]api.Object{addon, export, manifestAddon("other"), configOnly}))) != 0 {
		t.Fatal("configuration selected an unbound add-on")
	}
}

func TestCapabilitiesAndDuplicateBindings(t *testing.T) {
	provider := manifestAddon("provider").WithSpec(m("provides", api.StringList("capability")))
	consumer := manifestAddon("consumer").WithSpec(m("requires", api.StringList("capability")))
	first := obj(api.ClusterAddonBinding, "first", m("clusterRef", s("cluster"), "addonRefs", api.StringList("consumer")))
	second := obj(api.ClusterAddonBinding, "second", m("clusterRef", s("cluster"), "addonRefs", api.StringList("provider")))
	c := api.NewCatalog([]api.Object{provider, consumer, first, second})
	if issues := Validate(first, c); len(issues) != 0 {
		t.Fatal("capability on another binding of same cluster did not resolve", issues)
	}
	if !hasCode(Validate(first, api.NewCatalog([]api.Object{provider, consumer, first, second.WithSpec(second.Spec().With("clusterRef", s("different")))})), "api.reference") {
		t.Fatal("capability from another cluster satisfied requirement")
	}
	duplicate := second.WithSpec(second.Spec().With("addonRefs", api.StringList("consumer", "provider")))
	if !hasCode(Validate(first, api.NewCatalog([]api.Object{provider, consumer, first, duplicate})), "api.duplicate") {
		t.Fatal("duplicate expanded cluster instance accepted")
	}
	provider = provider.WithSpec(provider.Spec().With("requires", api.StringList("reverse")))
	consumer = consumer.WithSpec(consumer.Spec().With("provides", api.StringList("reverse")))
	if !hasCode(Validate(first, api.NewCatalog([]api.Object{provider, consumer, first, second})), "api.invariant") {
		t.Fatal("capability cycle accepted")
	}
	self := provider.WithSpec(m("provides", api.StringList("capability"), "requires", api.StringList("capability")))
	if !hasCode(validateCapabilities([]api.Object{self}), "api.reference") {
		t.Fatal("self-provided capability satisfied requirement")
	}
}

func TestIssuesStayBounded(t *testing.T) {
	refs := make([]string, 2000)
	for i := range refs {
		refs[i] = fmt.Sprintf("missing-%d", i)
	}
	binding := obj(api.ClusterAddonBinding, "binding", m("addonRefs", api.StringList(refs...)))
	_, issues := ExpandBinding(binding, api.NewCatalog(nil))
	if len(issues) != 999 {
		t.Fatalf("unbounded graph diagnostics: %d", len(issues))
	}
}

func TestPartialDefaultsNeedOnlyPresentPrerequisites(t *testing.T) {
	for _, spec := range []api.Value{
		m("olm", m("subscription", m("channel", s("stable")))),
		m("steps", l(m("playbook", s("setup.yaml")))),
		m("steps", l(m("target", m("static", m())))),
		m("steps", l(m("follows", s("operatorReady")))),
		m("manifestSet", m("manifests", l(m()))),
		m("inputs", l(m("name", s("input"), "effects", l(m("storageExportAttachment", m()))))),
	} {
		o := obj(api.ClusterAddon, "", spec)
		if issues := ValidatePartial(o, api.NewCatalog(nil)); len(issues) != 0 {
			t.Fatal("incomplete fragment rejected", issues)
		}
	}
	for _, spec := range []api.Value{
		m("steps", l(m("playbook", s("../escape.yaml")))),
		m("steps", l(m("source", m("git", m())))),
		m("provides", api.StringList("capability"), "readiness", m("checks", l())),
		m("inputs", l(m("name", s("input"), "resourceKind", s("Machine"), "effects", l(m("storageExportAttachment", m()))))),
	} {
		issues := ValidatePartial(obj(api.ClusterAddon, "", spec), api.NewCatalog(nil))
		if len(issues) == 0 {
			t.Fatal("locally contradictory fragment accepted")
		}
		for _, issue := range issues {
			if !strings.HasPrefix(issue.Field, "$.spec") {
				t.Fatal("non-canonical source field", issue.Field)
			}
		}
	}
}

func m(fields ...any) api.Value {
	values := make([]api.FieldValue, 0, len(fields)/2)
	for i := 0; i < len(fields); i += 2 {
		values = append(values, api.FieldValue{Name: fields[i].(string), Value: fields[i+1].(api.Value)})
	}
	return api.MapValue(values...)
}

func s(value string) api.Value { return api.StringValue(value) }

func l(values ...api.Value) api.Value { return api.ListValue(values...) }

func obj(kind api.Kind, name string, spec api.Value) api.Object {
	return api.NewObject(kind, name, api.Value{}, spec)
}

func manifests(value string) api.Value { return l(m("path", s(value))) }

func manifestAddon(name string) api.Object {
	return obj(api.ClusterAddon, name, m("manifestSet", m("manifests", manifests("manifests/example.yaml"))))
}

func storageInput(name string) api.Value {
	return m("name", s(name), "resourceKind", s("StorageExport"), "effects", l(m("storageExportAttachment", m())))
}

func names(objects []api.Object) []string {
	result := make([]string, len(objects))
	for i, object := range objects {
		result[i] = object.Name()
	}
	return result
}

func hasCode(issues []api.Issue, code string) bool {
	return slices.ContainsFunc(issues, func(issue api.Issue) bool { return issue.Code == code })
}
