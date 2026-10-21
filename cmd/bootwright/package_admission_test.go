package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	"github.com/crmarques/bootwright/internal/addons/packages"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func packageSources(t *testing.T, config map[string]any) desiredstate.Sources {
	t.Helper()
	sources := exampleDirectory(t, "lab-sno")
	binding := map[string]any{"apiVersion": "bootwright.io/v1alpha1", "kind": "ClusterAddonBinding", "metadata": map[string]any{"name": "operators"}, "spec": map[string]any{"clusterRef": "sno", "packages": config}}
	data, err := yaml.Marshal(binding)
	if err != nil {
		t.Fatal(err)
	}
	sources.Files = append(sources.Files, desiredstate.NewSourceFile(filepath.Join(sources.Roots[0], "operators.yaml"), data))
	return sources
}
func TestPackageBindingCompilerRefusals(t *testing.T) {
	for _, test := range []struct {
		name         string
		member       string
		subscription map[string]any
		timeout      string
		field        string
	}{
		{"unknown member", "unknown", map[string]any{"channel": "stable", "startingCSV": "operator.v1"}, "", "$.spec.packages.unknown"},
		{"missing channel", "nmstate", map[string]any{"startingCSV": "operator.v1"}, "", "$.spec.packages.nmstate.subscription.channel"},
		{"missing CSV", "nmstate", map[string]any{"channel": "stable"}, "", "$.spec.packages.nmstate.subscription.startingCSV"},
		{"catalog priority", "nmstate", map[string]any{"channel": "stable", "startingCSV": "operator.v1", "catalogSource": map[string]any{"name": "private", "image": "registry.example.test/catalog:v1"}}, "", "$.spec.packages.nmstate.subscription.catalogSource.priority"},
		{"source mismatch", "nmstate", map[string]any{"channel": "stable", "startingCSV": "operator.v1", "source": "other", "catalogSource": map[string]any{"name": "private", "image": "registry.example.test/catalog:v1", "priority": -100}}, "", "$.spec.packages.nmstate.subscription.source"},
		{"default catalog ownership", "nmstate", map[string]any{"channel": "stable", "startingCSV": "operator.v1", "catalogSource": map[string]any{"name": "redhat-operators", "image": "registry.example.test/catalog:v1", "priority": -100}}, "", "$.spec.packages.nmstate.subscription.catalogSource.name"},
		{"timeout bound", "nmstate", map[string]any{"channel": "stable", "startingCSV": "operator.v1"}, "6h", "$.spec.packages.nmstate.timeout"},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := map[string]any{"subscription": test.subscription}
			if test.timeout != "" {
				config["timeout"] = test.timeout
			}
			_, _, err := wireCompiler().Compile(context.Background(), packageSources(t, map[string]any{test.member: config}))
			if err == nil {
				t.Fatal("invalid binding admitted")
			}
			for _, issue := range diagnostics.Of(err) {
				if issue.Field == test.field {
					return
				}
			}
			t.Fatalf("expected %s: %v", test.field, diagnostics.Of(err))
		})
	}
}
func TestFixturePackageUsesCompiledProductionBinding(t *testing.T) {
	entries := packages.Builtin().Packages()
	for i := range entries {
		if entries[i].Name != "nmstate" {
			continue
		}
		entries[i].Admit = func(instance addons.Instance, facts addons.ClusterFacts) []api.Issue {
			if facts.Name != "sno" || len(facts.Nodes) != 1 {
				return []api.Issue{{Code: "api.invariant", Field: ".subscription.channel", Message: "fixture did not receive cluster facts", Remediation: "supply cluster facts"}}
			}
			return []api.Issue{{Code: "api.invariant", Field: ".subscription.channel", Message: "fixture channel is refused", Remediation: "choose another channel"}}
		}
		entries[i].RequiresFor = func(addons.Instance) []string { return []string{"fixture-required"} }
	}
	registry, err := addons.NewRegistry(entries)
	if err != nil {
		t.Fatal(err)
	}
	config := map[string]any{"nmstate": map[string]any{"subscription": map[string]any{"channel": "stable", "startingCSV": "operator.v1"}}}
	_, _, err = wireCompilerWithRegistry(registry).Compile(context.Background(), packageSources(t, config))
	if err == nil {
		t.Fatal("fixture was not admitted")
	}
	hook, requirement := false, false
	for _, issue := range diagnostics.Of(err) {
		hook = hook || issue.Field == "$.spec.packages.nmstate.subscription.channel" && issue.Message == "fixture channel is refused"
		requirement = requirement || issue.Field == "$.spec.packages.nmstate" && strings.Contains(issue.Message, "fixture-required")
	}
	if !hook || !requirement {
		t.Fatal(diagnostics.Of(err))
	}
}
func TestCompiledPackageConfigurationDecodesDefaults(t *testing.T) {
	state, _, err := wireCompiler().Compile(context.Background(), packageSources(t, map[string]any{"nmstate": map[string]any{"subscription": map[string]any{"channel": "stable", "startingCSV": "operator.v1"}}}))
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	binding, ok := state.Effective().Find(api.ClusterAddonBinding, "operators")
	if !ok {
		t.Fatal("binding absent")
	}
	config, err := addons.Decode[api.NMStateConfig](binding.Spec().Get("packages", "nmstate"))
	if err != nil {
		t.Fatal(err)
	}
	if config.Subscription.Source != "redhat-operators" || config.Subscription.SourceNamespace != "openshift-marketplace" || config.Timeout == "" {
		t.Fatal(config)
	}
}

func TestMetalLBPoolNamesAreDNSLabels(t *testing.T) {
	config := map[string]any{"metallb": map[string]any{"subscription": map[string]any{"channel": "stable", "startingCSV": "operator.v1"}, "pools": []any{map[string]any{"name": "dotted.name", "addresses": []string{"192.0.2.80-192.0.2.90"}}}}}
	_, _, err := wireCompiler().Compile(context.Background(), packageSources(t, config))
	if err == nil {
		t.Fatal("dotted pool name admitted")
	}
	for _, issue := range diagnostics.Of(err) {
		if issue.Field == "$.spec.packages.metallb.pools[0].name" {
			return
		}
	}
	t.Fatal(diagnostics.Of(err))
}
