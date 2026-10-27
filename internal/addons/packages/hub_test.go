package packages

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
)

func packageTestConfig(name string) api.Value {
	v := config().Without("pools")
	if name == "fdf" {
		v = v.With("cephConsumerRef", api.StringValue("export")).WithPath(api.StringValue("redhat-operators"), "subscription", "source")
	}
	if name == "acm" {
		v = v.With("mce", api.MapValue(api.FieldValue{Name: "channel", Value: api.StringValue("stable")}, api.FieldValue{Name: "startingCSV", Value: api.StringValue("engine.v1")}))
	}
	binding := api.NewObject(api.ClusterAddonBinding, "binding", api.MapValue(), api.MapValue(api.FieldValue{Name: "clusterRef", Value: api.StringValue("cluster")}, api.FieldValue{Name: "packages", Value: api.MapValue(api.FieldValue{Name: name, Value: v})}))
	binding, _ = addons.NormalizePackages(binding, Builtin())
	return binding.Spec().Get("packages", name)
}

func bootstrapConfig() api.Value {
	return api.MapValue(api.FieldValue{Name: "repoURL", Value: api.StringValue("https://example.test/repository.git")}, api.FieldValue{Name: "targetRevision", Value: api.StringValue("main")}, api.FieldValue{Name: "path", Value: api.StringValue(".")}, api.FieldValue{Name: "destinationNamespace", Value: api.StringValue("application")})
}

func TestHubPackageRenderGoldens(t *testing.T) {
	for _, tc := range []struct {
		name, pkg string
		change    func(api.Value) api.Value
	}{
		{"acm-single-node", "acm", func(v api.Value) api.Value { return v.WithPath(api.StringValue("Basic"), "hub", "availabilityConfig") }},
		{"acm-placement", "acm", func(v api.Value) api.Value {
			return v.WithPath(api.MapValue(api.FieldValue{Name: "node-role.kubernetes.io/infra", Value: api.StringValue("")}), "hub", "nodeSelector").WithPath(api.ListValue(api.MapValue(api.FieldValue{Name: "operator", Value: api.StringValue("Exists")}, api.FieldValue{Name: "effect", Value: api.StringValue("NoSchedule")})), "hub", "tolerations")
		}},
		{"acm-owned-catalog", "acm", func(v api.Value) api.Value {
			return v.WithPath(api.MapValue(api.FieldValue{Name: "name", Value: api.StringValue("hub-catalog")}, api.FieldValue{Name: "image", Value: api.StringValue("registry.example.test/catalog@sha256:" + strings.Repeat("a", 64))}, api.FieldValue{Name: "priority", Value: api.IntegerValue("1")}, api.FieldValue{Name: "securityContextConfig", Value: api.StringValue("restricted")}), "subscription", "catalogSource").WithPath(api.StringValue("hub-catalog"), "subscription", "source").WithPath(api.StringValue("hub-catalog"), "mce", "source")
		}},
		{"gitops-public", "gitops", func(v api.Value) api.Value { return v.With("bootstrap", bootstrapConfig()) }},
		{"gitops-private", "gitops", func(v api.Value) api.Value {
			return v.With("bootstrap", bootstrapConfig().With("credentialsRef", api.StringValue("repository-login")))
		}},
		{"gitops-access", "gitops", func(v api.Value) api.Value {
			return v.With("bootstrap", bootstrapConfig().With("access", api.MapValue(api.FieldValue{Name: "clusterRole", Value: api.StringValue("bootstrap-writer")})))
		}},
		{"gitops-manual", "gitops", func(v api.Value) api.Value {
			return v.With("bootstrap", bootstrapConfig().With("sync", api.MapValue(api.FieldValue{Name: "automated", Value: api.BoolValue(false)})))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, _ := Builtin().Find(tc.pkg)
			v := p.Normalize(tc.change(packageTestConfig(tc.pkg)))
			rendered, err := p.Render(addons.Instance{Cluster: "cluster", Binding: "binding", Config: v})
			program := co.Program{Version: co.Version, Steps: []co.Step{}, Checks: rendered.Checks}
			for _, phase := range rendered.After {
				steps, e := co.CompilePhase(phase)
				if e != nil {
					t.Fatal(e)
				}
				program.Steps = append(program.Steps, steps...)
			}
			data, err := co.Freeze(program, co.Options{Namespace: p.OLM.Namespace, PlacementNamespaces: p.PlacementNamespaces})
			if err != nil {
				t.Fatal(err)
			}
			matchesGolden(t, tc.name, data, false)
			if _, err := co.ThawVersion(data, co.Options{Namespace: p.OLM.Namespace, PlacementNamespaces: p.PlacementNamespaces}); err != nil {
				t.Fatal(err)
			}
			program = co.Program{}
			if err := json.Unmarshal(data, &program); err != nil {
				t.Fatal(err)
			}
			for _, s := range program.Steps {
				if s.Create != nil && s.Create.Kind == "OperatorGroup" && tc.pkg == "gitops" && strings.Contains(string(s.Create.Body), `"spec"`) {
					t.Fatal("all-namespaces group has spec")
				}
			}
		})
	}
}

func TestGitOpsBootstrapRefusals(t *testing.T) {
	for _, tc := range []struct{ field, value, code string }{
		{"repoURL", "http://example.test/repo", "api.field"}, {"repoURL", "https://secret:password@example.test/repo", "api.field"}, {"repoURL", "https://example.test/repo?token=secret", "api.field"}, {"repoURL", "https://example.test/repo#fragment", "api.field"}, {"path", "../repo", "api.field"}, {"path", "/repo", "api.field"}, {"targetRevision", "bad revision", "api.field"},
	} {
		t.Run(tc.value, func(t *testing.T) {
			v := normalizeGitOps(packageTestConfig("gitops").With("bootstrap", bootstrapConfig().With(tc.field, api.StringValue(tc.value))))
			issues := admitGitOps(addons.Instance{Config: v}, addons.ClusterFacts{})
			if len(issues) != 1 || issues[0].Code != tc.code || issues[0].Field != ".bootstrap."+tc.field || issues[0].Remediation == "" {
				t.Fatal(issues)
			}
			if tc.field == "repoURL" && strings.Contains(issues[0].Message, tc.value) {
				t.Fatal("URL leaked")
			}
		})
	}
	valid := normalizeGitOps(packageTestConfig("gitops").With("bootstrap", bootstrapConfig()))
	if issues := admitGitOps(addons.Instance{Config: valid}, addons.ClusterFacts{}); len(issues) != 0 {
		t.Fatal(issues)
	}
	invalid := valid.WithPath(api.BoolValue(false), "bootstrap", "sync", "automated").WithPath(api.BoolValue(true), "bootstrap", "sync", "prune")
	if issues := admitGitOps(addons.Instance{Config: invalid}, addons.ClusterFacts{}); len(issues) != 1 || issues[0].Code != "api.invariant" {
		t.Fatal(issues)
	}
}

func TestACMSingleNodeAndDependentApproval(t *testing.T) {
	v := packageTestConfig("acm")
	i := addons.Instance{Cluster: "cluster", Config: v}
	f := addons.ClusterFacts{Nodes: []string{"node"}}
	if len(admitACM(i, f)) != 1 {
		t.Fatal("single node accepted without Basic")
	}
	i.Config = v.WithPath(api.StringValue("Basic"), "hub", "availabilityConfig")
	if len(admitACM(i, f)) != 0 {
		t.Fatal("Basic refused")
	}
	r, err := renderACM(i)
	if err != nil {
		t.Fatal(err)
	}
	a := r.After[0].Approvals[0]
	if a.CSV != "engine.v1" || a.Source != "redhat-operators" || a.Creator.Kind != "MultiClusterHub" {
		t.Fatal(a)
	}
}

func TestHubSelectionPlanGolden(t *testing.T) {
	fixture := addons.Package{Name: "fixture", Budget: time.Minute, Render: func(addons.Instance) (addons.Rendered, error) { return addons.Rendered{}, nil }}
	entries := Builtin().Packages()
	entries = append(entries, fixture)
	registry, err := addons.NewRegistry(entries)
	if err != nil {
		t.Fatal(err)
	}
	configs := api.MapValue(api.FieldValue{Name: "gitops", Value: packageTestConfig("gitops")}, api.FieldValue{Name: "acm", Value: packageTestConfig("acm")}, api.FieldValue{Name: "fixture", Value: api.MapValue()})
	binding := api.NewObject(api.ClusterAddonBinding, "hub", api.MapValue(), api.MapValue(api.FieldValue{Name: "clusterRef", Value: api.StringValue("cluster")}, api.FieldValue{Name: "packages", Value: configs}))
	selected, issues := addons.Instances(api.NewCatalog([]api.Object{binding}), registry)
	if len(issues) > 0 {
		t.Fatal(issues)
	}
	type entry struct {
		Package   string            `json:"package"`
		Providers []addons.Provider `json:"providers"`
	}
	plan := []entry{}
	for _, s := range selected {
		plan = append(plan, entry{s.Package.Name, s.Providers})
	}
	if len(plan) != 3 || plan[2].Package != "gitops" || len(plan[2].Providers) != 2 {
		t.Fatal(plan)
	}
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "hub-selection-plan", data, false)
}

func TestHubNormalizationIsIdempotentAndKeepsExplicitValues(t *testing.T) {
	for _, name := range []string{"acm", "gitops"} {
		p, _ := Builtin().Find(name)
		v := packageTestConfig(name)
		if name == "acm" {
			v = v.WithPath(api.StringValue("custom"), "mce", "source").WithPath(api.StringValue("custom-marketplace"), "mce", "sourceNamespace")
		} else {
			v = v.With("bootstrap", bootstrapConfig().With("sync", api.MapValue(api.FieldValue{Name: "automated", Value: api.BoolValue(false)})))
		}
		normalized := p.Normalize(v)
		if !p.Normalize(normalized).Equal(normalized) {
			t.Fatalf("%s not idempotent", name)
		}
		if name == "acm" && normalized.Get("mce", "source").Text() != "custom" {
			t.Fatal("explicit source lost")
		}
		if name == "gitops" && normalized.Get("bootstrap", "sync", "automated").Bool() {
			t.Fatal("explicit manual sync lost")
		}
	}
}
