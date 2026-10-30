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
	if got.Get("olm", "namespace", "management").Text() != "managed" || got.Get("olm", "subscription", "name").Text() != "example-operator" || got.Get("olm", "subscription", "source").Text() != "example-catalog" || got.Get("olm", "subscription", "sourceNamespace").Text() != "openshift-marketplace" || got.Has("olm", "subscription", "installPlanApproval") || got.Get("olm", "catalogSource", "securityContextConfig").Text() != "restricted" {
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
		{"storage effect kind", base.With("inputs", l(m("name", s("input"), "resourceKind", s("Machine"), "effects", l(m("cephConsumerAttachment", m())))))},
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

func TestOLMLabelsAndNormalizedDefaults(t *testing.T) {
	o, _ := Normalize(obj(api.ClusterAddon, "example", executableSpec()), api.NewCatalog(nil))
	if issues := Validate(o, api.NewCatalog(nil)); len(issues) != 0 {
		t.Fatal(issues)
	}
	resource := o.Spec().Get("olm", "customResources").Items()[0].Get("resource")
	if resource.Get("spec", "count").Text() != "184467440737095516160" || resource.Get("spec", "ratio").Text() != "1.25" {
		t.Fatal("native numeric values changed")
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

func executableSpec() api.Value {
	crd := m("resourceExists", m("apiVersion", s("apiextensions.k8s.io/v1"), "kind", s("CustomResourceDefinition"), "name", s("widgets.example.test")))
	resource := m("apiVersion", s("example.test/v1"), "kind", s("Widget"), "metadata", m("name", s("example"), "namespace", s("example-operator"), "labels", m("example.test/key", s("value"))), "spec", m("count", api.IntegerValue("184467440737095516160"), "ratio", api.NumberValue("1.25")))
	return m("olm", m(
		"namespace", m("name", s("example-operator")),
		"catalogSource", m("name", s("example-catalog"), "image", s("registry.example.test/catalog:v1"), "priority", api.IntegerValue("-200")),
		"subscription", m("package", s("example"), "channel", s("stable"), "startingCSV", s("example.v1.0.0")),
		"customResources", l(m("gate", l(crd), "resource", resource)),
	), "readiness", m("timeout", s("45m")))
}

func executableIssues(t *testing.T, edit func(api.Value) api.Value) []api.Issue {
	t.Helper()
	o, _ := Normalize(obj(api.ClusterAddon, "example", edit(executableSpec())), api.NewCatalog(nil))
	return Validate(o, api.NewCatalog(nil))
}

func hasIssue(issues []api.Issue, code, field string) bool {
	return slices.ContainsFunc(issues, func(issue api.Issue) bool { return issue.Code == code && issue.Field == "$."+field })
}

func TestExecutableOLMShapeAdmitsAndRefusesEachRule(t *testing.T) {
	const cr = "spec.olm.customResources[0]"
	const echoed = "do-not-echo"
	resourcePath := func(edit func(api.Value) api.Value) func(api.Value) api.Value {
		return func(spec api.Value) api.Value {
			item := spec.Get("olm", "customResources").Items()[0]
			return spec.WithPath(l(item.With("resource", edit(item.Get("resource")))), "olm", "customResources")
		}
	}
	gatePath := func(checks ...api.Value) func(api.Value) api.Value {
		return func(spec api.Value) api.Value {
			item := spec.Get("olm", "customResources").Items()[0]
			return spec.WithPath(l(item.With("gate", l(checks...))), "olm", "customResources")
		}
	}
	crd := func(name string) api.Value {
		return m("resourceExists", m("apiVersion", s("apiextensions.k8s.io/v1"), "kind", s("CustomResourceDefinition"), "name", s(name)))
	}
	if issues := executableIssues(t, func(spec api.Value) api.Value { return spec }); len(issues) != 0 {
		t.Fatal(issues)
	}
	for name, edit := range map[string]func(api.Value) api.Value{
		"all-namespaces group":      func(v api.Value) api.Value { return v.WithPath(m("targetNamespaces", l()), "olm", "operatorGroup") },
		"annotation":                resourcePath(func(r api.Value) api.Value { return r.WithPath(m("example.test/a", s("b")), "metadata", "annotations") }),
		"timeout at the ceiling":    func(v api.Value) api.Value { return v.WithPath(s("5h50m"), "readiness", "timeout") },
		"gate with a further check": gatePath(crd("widgets.example.test"), m("condition", m("apiVersion", s("apps/v1"), "kind", s("Deployment"), "name", s("x"), "namespace", s("example-operator"), "condition", m("type", s("Available"), "status", s("Unknown"))))),
		"CSV check of its own": func(v api.Value) api.Value {
			return v.WithPath(l(m("csvSucceeded", m("namespace", s("example-operator"), "subscription", s("example")))), "readiness", "checks")
		},
		"group that only looks nested": func(v api.Value) api.Value {
			return gatePath(crd("widgets.example.notk8s.io"))(resourcePath(func(r api.Value) api.Value { return r.With("apiVersion", s("example.notk8s.io/v1")) })(v))
		},
		"x-k8s.io group": func(v api.Value) api.Value {
			return gatePath(crd("clusters.cluster.x-k8s.io"))(resourcePath(func(r api.Value) api.Value { return r.With("apiVersion", s("cluster.x-k8s.io/v1")) })(v))
		},
		"label key that only looks nested": func(v api.Value) api.Value {
			return v.WithPath(m("x-k8s.io/key", s("v"), "notopenshift.io/key", s("v"), "example.test/kubernetes.io", s("v")), "olm", "namespace", "labels")
		},
		"priority just below the default catalogs": func(v api.Value) api.Value {
			return v.WithPath(api.IntegerValue("-101"), "olm", "catalogSource", "priority")
		},
		"smallest 32-bit priority": func(v api.Value) api.Value {
			return v.WithPath(api.IntegerValue("-2147483648"), "olm", "catalogSource", "priority")
		},
		"poll interval of one minute": func(v api.Value) api.Value { return v.WithPath(s("1m"), "olm", "catalogSource", "pollInterval") },
	} {
		t.Run("admits "+name, func(t *testing.T) {
			if issues := executableIssues(t, edit); len(issues) != 0 {
				t.Fatal(issues)
			}
		})
	}
	for _, namespace := range []string{"kubernetes-ops", "openshifty", "defaults"} {
		if issues := executableIssues(t, func(v api.Value) api.Value {
			item := v.Get("olm", "customResources").Items()[0]
			return v.WithPath(s(namespace), "olm", "namespace", "name").WithPath(l(item.WithPath(s(namespace), "resource", "metadata", "namespace")), "olm", "customResources")
		}); len(issues) != 0 {
			t.Fatal("a namespace that only begins like a reserved one was refused", namespace, issues)
		}
	}
	cases := []struct {
		name, code, field string
		edit              func(api.Value) api.Value
	}{
		{"default namespace", "api.invariant", "spec.olm.namespace.name", func(v api.Value) api.Value { return v.WithPath(s("default"), "olm", "namespace", "name") }},
		{"openshift namespace", "api.invariant", "spec.olm.namespace.name", func(v api.Value) api.Value { return v.WithPath(s("openshift"), "olm", "namespace", "name") }},
		{"operators namespace", "api.invariant", "spec.olm.namespace.name", func(v api.Value) api.Value { return v.WithPath(s("openshift-operators"), "olm", "namespace", "name") }},
		{"marketplace namespace", "api.invariant", "spec.olm.namespace.name", func(v api.Value) api.Value { return v.WithPath(s("openshift-marketplace"), "olm", "namespace", "name") }},
		{"kube namespace", "api.invariant", "spec.olm.namespace.name", func(v api.Value) api.Value { return v.WithPath(s("kube-system"), "olm", "namespace", "name") }},
		{"openshift-config namespace", "api.invariant", "spec.olm.namespace.name", func(v api.Value) api.Value { return v.WithPath(s("openshift-config"), "olm", "namespace", "name") }},
		{"openshift-monitoring namespace", "api.invariant", "spec.olm.namespace.name", func(v api.Value) api.Value { return v.WithPath(s("openshift-monitoring"), "olm", "namespace", "name") }},
		{"kube-public namespace", "api.invariant", "spec.olm.namespace.name", func(v api.Value) api.Value { return v.WithPath(s("kube-public"), "olm", "namespace", "name") }},
		{"bootwright namespace label", "api.invariant", "spec.olm.namespace.labels", func(v api.Value) api.Value {
			return v.WithPath(m("bootwright.io/owner", s(echoed)), "olm", "namespace", "labels")
		}},
		{"openshift.io namespace label", "api.invariant", "spec.olm.namespace.labels", func(v api.Value) api.Value {
			return v.WithPath(m("openshift.io/cluster-monitoring", s(echoed)), "olm", "namespace", "labels")
		}},
		{"kubernetes.io namespace label", "api.invariant", "spec.olm.namespace.labels", func(v api.Value) api.Value {
			return v.WithPath(m("kubernetes.io/metadata.name", s(echoed)), "olm", "namespace", "labels")
		}},
		{"k8s.io namespace label", "api.invariant", "spec.olm.namespace.labels", func(v api.Value) api.Value {
			return v.WithPath(m("k8s.io/owner", s(echoed)), "olm", "namespace", "labels")
		}},
		{"subdomain namespace label", "api.invariant", "spec.olm.namespace.labels", func(v api.Value) api.Value {
			return v.WithPath(m("pod-security.kubernetes.io/enforce", s(echoed)), "olm", "namespace", "labels")
		}},
		{"high catalog priority", "api.value", "spec.olm.catalogSource.priority", func(v api.Value) api.Value {
			return v.WithPath(api.IntegerValue("100"), "olm", "catalogSource", "priority")
		}},
		{"zero catalog priority", "api.value", "spec.olm.catalogSource.priority", func(v api.Value) api.Value {
			return v.WithPath(api.IntegerValue("0"), "olm", "catalogSource", "priority")
		}},
		{"catalog priority of redhat-operators", "api.value", "spec.olm.catalogSource.priority", func(v api.Value) api.Value {
			return v.WithPath(api.IntegerValue("-100"), "olm", "catalogSource", "priority")
		}},
		{"catalog priority beyond 32 bits", "api.value", "spec.olm.catalogSource.priority", func(v api.Value) api.Value {
			return v.WithPath(api.IntegerValue("-2147483649"), "olm", "catalogSource", "priority")
		}},
		{"catalog priority beyond 64 bits", "api.value", "spec.olm.catalogSource.priority", func(v api.Value) api.Value {
			return v.WithPath(api.IntegerValue("-92233720368547758080"), "olm", "catalogSource", "priority")
		}},
		{"fast catalog polling", "api.value", "spec.olm.catalogSource.pollInterval", func(v api.Value) api.Value { return v.WithPath(s("59s"), "olm", "catalogSource", "pollInterval") }},
		{"default catalog name", "api.invariant", "spec.olm.catalogSource.name", func(v api.Value) api.Value { return v.WithPath(s("redhat-operators"), "olm", "catalogSource", "name") }},
		{"foreign source namespace", "api.invariant", "spec.olm.subscription.sourceNamespace", func(v api.Value) api.Value {
			return v.WithPath(s("example-operator"), "olm", "subscription", "sourceNamespace")
		}},
		{"fractional timeout", "api.value", "spec.readiness.timeout", func(v api.Value) api.Value { return v.WithPath(s("1500ms"), "readiness", "timeout") }},
		{"timeout over ceiling", "api.value", "spec.readiness.timeout", func(v api.Value) api.Value { return v.WithPath(s("5h50m1s"), "readiness", "timeout") }},
		{"metadata member", "api.invariant", cr + ".resource.metadata.ownerReferences", resourcePath(func(r api.Value) api.Value { return r.WithPath(l(), "metadata", "ownerReferences") })},
		{"metadata finalizers", "api.invariant", cr + ".resource.metadata.finalizers", resourcePath(func(r api.Value) api.Value { return r.WithPath(l(), "metadata", "finalizers") })},
		{"bootwright label", "api.invariant", cr + ".resource.metadata.labels", resourcePath(func(r api.Value) api.Value {
			return r.WithPath(m("bootwright.io/owner", s(echoed)), "metadata", "labels")
		})},
		{"bootwright annotation", "api.invariant", cr + ".resource.metadata.annotations", resourcePath(func(r api.Value) api.Value {
			return r.WithPath(m("bootwright.io/owner", s(echoed)), "metadata", "annotations")
		})},
		{"metadata generateName", "api.invariant", cr + ".resource.metadata.generateName", resourcePath(func(r api.Value) api.Value { return r.WithPath(s("widget-"), "metadata", "generateName") })},
		{"missing namespace", "api.required", cr + ".resource.metadata.namespace", resourcePath(func(r api.Value) api.Value { return r.With("metadata", m("name", s("example"))) })},
		{"other namespace", "api.invariant", cr + ".resource.metadata.namespace", resourcePath(func(r api.Value) api.Value { return r.WithPath(s("other"), "metadata", "namespace") })},
		{"core group", "api.invariant", cr + ".resource.apiVersion", resourcePath(func(r api.Value) api.Value { return r.With("apiVersion", s("v1")) })},
		{"group without dot", "api.invariant", cr + ".resource.apiVersion", resourcePath(func(r api.Value) api.Value { return r.With("apiVersion", s("example/v1")) })},
		{"k8s.io group", "api.invariant", cr + ".resource.apiVersion", resourcePath(func(r api.Value) api.Value { return r.With("apiVersion", s("k8s.io/v1")) })},
		{"subdomain of k8s.io", "api.invariant", cr + ".resource.apiVersion", resourcePath(func(r api.Value) api.Value { return r.With("apiVersion", s("apps.k8s.io/v1")) })},
		{"kubernetes.io group", "api.invariant", cr + ".resource.apiVersion", resourcePath(func(r api.Value) api.Value { return r.With("apiVersion", s("example.kubernetes.io/v1")) })},
		{"openshift.io group", "api.invariant", cr + ".resource.apiVersion", resourcePath(func(r api.Value) api.Value { return r.With("apiVersion", s("config.openshift.io/v1")) })},
		{"coreos.com group", "api.invariant", cr + ".resource.apiVersion", resourcePath(func(r api.Value) api.Value { return r.With("apiVersion", s("operators.coreos.com/v1")) })},
		{"bootwright.io group", "api.invariant", cr + ".resource.apiVersion", resourcePath(func(r api.Value) api.Value { return r.With("apiVersion", s("bootwright.io/v1alpha1")) })},
		{"open-cluster-management.io group", "api.invariant", cr + ".resource.apiVersion", resourcePath(func(r api.Value) api.Value { return r.With("apiVersion", s("cluster.open-cluster-management.io/v1")) })},
		{"cert-manager.io group", "api.invariant", cr + ".resource.apiVersion", resourcePath(func(r api.Value) api.Value { return r.With("apiVersion", s("cert-manager.io/v1")) })},
		{"argoproj.io group", "api.invariant", cr + ".resource.apiVersion", resourcePath(func(r api.Value) api.Value { return r.With("apiVersion", s("argoproj.io/v1alpha1")) })},
		{"mixed-case reserved group", "api.invariant", cr + ".resource.apiVersion", resourcePath(func(r api.Value) api.Value { return r.With("apiVersion", s("Config.OpenShift.IO/v1")) })},
		{"mixed-case reserved subdomain", "api.invariant", cr + ".resource.apiVersion", resourcePath(func(r api.Value) api.Value { return r.With("apiVersion", s("Apps.K8S.io/v1")) })},
		{"secret kind", "api.invariant", cr + ".resource.kind", resourcePath(func(r api.Value) api.Value { return r.With("kind", s("Secret")) })},
		{"lowercase secret kind", "api.invariant", cr + ".resource.kind", resourcePath(func(r api.Value) api.Value { return r.With("kind", s("secret")) })},
		{"uppercase secret kind", "api.invariant", cr + ".resource.kind", resourcePath(func(r api.Value) api.Value { return r.With("kind", s("SECRET")) })},
		{"status in body", "api.invariant", cr + ".resource", resourcePath(func(r api.Value) api.Value { return r.With("status", m("ready", api.BoolValue(true))) })},
		{"template delimiter", "api.invariant", cr + ".resource", resourcePath(func(r api.Value) api.Value { return r.WithPath(s("{{ secret }}"), "spec", "value") })},
		{"gate without CRD", "api.invariant", cr + ".gate", gatePath(m("resourceExists", m("apiVersion", s("example.test/v1"), "kind", s("Gadget"), "name", s("x"))))},
		{"gate CRD of another group", "api.invariant", cr + ".gate", gatePath(crd("widgets.other.test"))},
		{"gate CRD without plural", "api.invariant", cr + ".gate", gatePath(crd(".example.test"))},
		{"gate CRD of a subgroup", "api.invariant", cr + ".gate", gatePath(crd("widgets.sub.example.test"))},
		{"gate with several CRDs of the group", "api.invariant", cr + ".gate", gatePath(crd("foos.example.test"), crd("widgets.example.test"))},
		{"gate CRD wrong version", "api.invariant", cr + ".gate", gatePath(m("resourceExists", m("apiVersion", s("apiextensions.k8s.io/v1beta1"), "kind", s("CustomResourceDefinition"), "name", s("widgets.example.test"))))},
		{"gate CRD plural with several dots", "api.invariant", cr + ".gate", gatePath(crd("a.b.widgets.example.test"))},
		{"gate lowercase Secret", "api.invariant", cr + ".gate[1]", gatePath(crd("widgets.example.test"), m("resourceExists", m("apiVersion", s("v1"), "kind", s("secret"), "name", s("s"), "namespace", s("example-operator"))))},
		{"gate Secret", "api.invariant", cr + ".gate[1]", gatePath(crd("widgets.example.test"), m("resourceExists", m("apiVersion", s("v1"), "kind", s("Secret"), "name", s("s"), "namespace", s("example-operator"))))},
		{"gate foreign CSV", "api.invariant", cr + ".gate[1].csvSucceeded", gatePath(crd("widgets.example.test"), m("csvSucceeded", m("namespace", s("other"), "subscription", s("example"))))},
		{"gate invalid condition status", "api.value", cr + ".gate[1]", gatePath(crd("widgets.example.test"), m("condition", m("apiVersion", s("apps/v1"), "kind", s("Deployment"), "name", s("x"), "namespace", s("example-operator"), "condition", m("type", s("Available"), "status", s("Maybe")))))},
		{"readiness lowercase Secret", "api.invariant", "spec.readiness.checks[0]", func(v api.Value) api.Value {
			return v.WithPath(l(m("condition", m("apiVersion", s("v1"), "kind", s("secret"), "name", s("s"), "namespace", s("example-operator"), "condition", m("type", s("x"), "status", s("True"))))), "readiness", "checks")
		}},
		{"readiness Secret", "api.invariant", "spec.readiness.checks[0]", func(v api.Value) api.Value {
			return v.WithPath(l(m("condition", m("apiVersion", s("v1"), "kind", s("Secret"), "name", s("s"), "namespace", s("example-operator"), "condition", m("type", s("x"), "status", s("True"))))), "readiness", "checks")
		}},
		{"readiness foreign namespace CSV", "api.invariant", "spec.readiness.checks[0].csvSucceeded", func(v api.Value) api.Value {
			return v.WithPath(l(m("csvSucceeded", m("namespace", s("other"), "subscription", s("example")))), "readiness", "checks")
		}},
		{"readiness foreign subscription CSV", "api.invariant", "spec.readiness.checks[0].csvSucceeded", func(v api.Value) api.Value {
			return v.WithPath(l(m("csvSucceeded", m("namespace", s("example-operator"), "subscription", s("other")))), "readiness", "checks")
		}},
		{"readiness invalid status", "api.value", "spec.readiness.checks[0]", func(v api.Value) api.Value {
			return v.WithPath(l(m("condition", m("apiVersion", s("apps/v1"), "kind", s("Deployment"), "name", s("x"), "namespace", s("example-operator"), "condition", m("type", s("Available"), "status", s("yes"))))), "readiness", "checks")
		}},
		{"step requirement Secret", "api.invariant", "spec.steps[0].requires[0]", func(v api.Value) api.Value {
			return v.With("steps", l(m("name", s("s"), "requires", l(m("resourceExists", m("apiVersion", s("v1"), "kind", s("Secret"), "name", s("s")))))))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			issues := executableIssues(t, tc.edit)
			if !hasIssue(issues, tc.code, tc.field) {
				t.Fatalf("want %s at %s, got %v", tc.code, tc.field, issues)
			}
			for _, issue := range issues {
				if strings.Contains(issue.Message+issue.Remediation, echoed) || strings.Contains(issue.Message+issue.Remediation, "registry.example.test") {
					t.Fatalf("diagnostic echoes a value: %+v", issue)
				}
				if issue.Remediation == "" || strings.HasSuffix(issue.Message, ".") || issue.Message != strings.ToLower(issue.Message[:1])+issue.Message[1:] {
					t.Fatalf("issue style: %+v", issue)
				}
			}
		})
	}
	issues := executableIssues(t, func(v api.Value) api.Value {
		return v.WithPath(l(m("gate", l(), "resource", v.Get("olm", "customResources").Items()[0].Get("resource").With("apiVersion", s("bootwright.io/v1")))), "olm", "customResources")
	})
	if len(issues) != 1 {
		t.Fatalf("one fault must raise one diagnostic: %v", issues)
	}
}

func TestExecutableOLMPartialFragments(t *testing.T) {
	for _, spec := range []api.Value{
		m("olm", m("customResources", l(m("resource", m("apiVersion", s("example.test/v1")))))),
		m("olm", m("customResources", l(m("gate", l())))),
		m("readiness", m("timeout", s("30m"), "checks", l(m("csvSucceeded", m("namespace", s("a"), "subscription", s("b")))))),
	} {
		if issues := ValidatePartial(obj(api.ClusterAddon, "", spec), api.NewCatalog(nil)); len(issues) != 0 {
			t.Fatal("incomplete fragment rejected", issues)
		}
	}
	catalog := func(fields ...any) api.Value { return m("olm", m("catalogSource", m(fields...))) }
	if issues := ValidatePartial(obj(api.ClusterAddon, "", catalog("name", s("only-a-name"))), api.NewCatalog(nil)); len(issues) != 0 {
		t.Fatal("a catalog without a priority or interval rejected", issues)
	}
	for field, fragment := range map[string]api.Value{
		"spec.olm.catalogSource.priority":     catalog("priority", api.IntegerValue("5")),
		"spec.olm.catalogSource.pollInterval": catalog("pollInterval", s("10s")),
	} {
		if issues := ValidatePartial(obj(api.ClusterAddon, "", fragment), api.NewCatalog(nil)); !hasIssue(issues, "api.value", field) {
			t.Fatalf("missing %s in %v", field, issues)
		}
	}
	bad := m("olm", m("namespace", m("name", s("default")), "customResources", l(m("resource", m("apiVersion", s("k8s.io/v1"), "kind", s("Secret"))))))
	issues := ValidatePartial(obj(api.ClusterAddon, "", bad), api.NewCatalog(nil))
	for _, field := range []string{"spec.olm.namespace.name", "spec.olm.customResources[0].resource.apiVersion", "spec.olm.customResources[0].resource.kind"} {
		if !hasIssue(issues, "api.invariant", field) {
			t.Fatalf("missing %s in %v", field, issues)
		}
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
	export := obj(api.CephConsumer, "export", m())
	binding := obj(api.ClusterAddonBinding, "binding", m("clusterRef", s("cluster"), "addonRefs", api.StringList("storage"), "addonConfigs", l(m("addonRef", s("storage"), "inputs", l(m("name", s("arbitrary-label"), "value", s("export")))))))
	catalog := api.NewCatalog([]api.Object{addon, export, binding})
	if issues := Validate(binding, catalog); len(issues) != 0 {
		t.Fatal(issues)
	}
	if got := StorageAttachments(catalog, Registry{}); !reflect.DeepEqual(got, []Attachment{{ClusterRef: "cluster", ConsumerRef: "export"}}) {
		t.Fatalf("attachments = %v", got)
	}
	for _, altered := range []api.Object{
		addon.WithSpec(addon.Spec().Without("provides")),
		addon.WithSpec(addon.Spec().With("inputs", l(input.Without("effects")))),
		addon.WithSpec(addon.Spec().With("inputs", l(input.With("resourceKind", s("Machine"))))),
		addon.WithSpec(addon.Spec().With("inputs", l(input.With("secretType", s("token"))))),
		addon.WithSpec(addon.Spec().With("inputs", l(input, input))),
	} {
		if got := StorageAttachments(api.NewCatalog([]api.Object{altered, export, binding}), Registry{}); len(got) != 0 {
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
	if !hasCode(Validate(configOnly, api.NewCatalog([]api.Object{addon, export, manifestAddon("other"), configOnly})), "api.reference") || len(StorageAttachments(api.NewCatalog([]api.Object{addon, export, manifestAddon("other"), configOnly}), Registry{})) != 0 {
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
	if !hasCode(Validate(duplicate, api.NewCatalog([]api.Object{provider, consumer, first, duplicate})), "api.duplicate") {
		t.Fatal("duplicate expanded cluster instance accepted")
	}
	provider = provider.WithSpec(provider.Spec().With("requires", api.StringList("reverse")))
	consumer = consumer.WithSpec(consumer.Spec().With("provides", api.StringList("reverse")))
	if !hasCode(Validate(first, api.NewCatalog([]api.Object{provider, consumer, first, second})), "api.invariant") {
		t.Fatal("capability cycle accepted")
	}
	self := provider.WithSpec(m("provides", api.StringList("capability"), "requires", api.StringList("capability")))
	selfBinding := obj(api.ClusterAddonBinding, "self", m("clusterRef", s("cluster"), "addonRefs", api.StringList(self.Name())))
	if !hasCode(Validate(selfBinding, api.NewCatalog([]api.Object{self, selfBinding})), "api.reference") {
		t.Fatal("self-provided capability satisfied requirement")
	}
}

func TestIssuesStayBounded(t *testing.T) {
	refs := make([]string, 2000)
	for i := range refs {
		refs[i] = fmt.Sprintf("missing-%d", i)
	}
	profile := obj(api.ClusterAddonProfile, "many", m("addonRefs", api.StringList(refs...)))
	binding := obj(api.ClusterAddonBinding, "binding", m("profileRefs", api.StringList("many")))
	_, issues := ExpandBinding(binding, api.NewCatalog([]api.Object{profile}))
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
		m("inputs", l(m("name", s("input"), "effects", l(m("cephConsumerAttachment", m()))))),
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
		m("inputs", l(m("name", s("input"), "resourceKind", s("Machine"), "effects", l(m("cephConsumerAttachment", m()))))),
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
	return m("name", s(name), "resourceKind", s("CephConsumer"), "effects", l(m("cephConsumerAttachment", m())))
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
