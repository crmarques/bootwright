package main

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	"github.com/crmarques/bootwright/internal/addons/packages"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

const nmstateKindDefault = "  defaults:\n    ClusterAddonBinding:\n      packages:\n        nmstate:\n          subscription: {channel: stable, startingCSV: kubernetes-nmstate-operator.v4.0.0}\n          timeout: 31m\n"

const metallbMember = "    metallb:\n      subscription: {channel: stable, startingCSV: metallb-operator.v4.0.0}\n      pools: [{name: services, addresses: ['198.51.100.128-198.51.100.135']}]\n"

func bindingYAML(name, members string) string {
	return "---\napiVersion: bootwright.io/v1alpha1\nkind: ClusterAddonBinding\nmetadata: {name: " + name + "}\nspec:\n  clusterRef: sno\n" + members
}

func kindDefaultSources(t *testing.T, documents ...string) desiredstate.Sources {
	t.Helper()
	sources := exampleDirectory(t, "lab-sno")
	environment := filepath.Join(sources.Roots[0], "environment.yaml")
	for i, file := range sources.Files {
		if file.Path() == environment {
			sources.Files[i] = desiredstate.NewSourceFile(environment, append(slices.Clone(file.Bytes()), nmstateKindDefault...))
		}
	}
	for i, document := range documents {
		sources.Files = append(sources.Files, desiredstate.NewSourceFile(filepath.Join(sources.Roots[0], "binding-"+string(rune('a'+i))+".yaml"), []byte(document)))
	}
	return sources
}

func compileKindDefaults(t *testing.T, documents ...string) (*compilation.State, error) {
	t.Helper()
	state, _, err := wireCompiler().Compile(context.Background(), kindDefaultSources(t, documents...))
	return state, err
}

func selectedPackages(t *testing.T, state *compilation.State) []string {
	t.Helper()
	instances, issues := addons.Instances(state.Effective(), packages.Builtin())
	if len(issues) != 0 {
		t.Fatalf("selection refused: %v", issues)
	}
	names := []string{}
	for _, instance := range instances {
		names = append(names, instance.Package.Name)
	}
	slices.Sort(names)
	return names
}

// Environment kind defaults for packages fill the members a binding declares
// and never select one (specs/api/environment.md, Kind defaults).
func TestKindDefaultsNeverSelectAPackage(t *testing.T) {
	state, err := compileKindDefaults(t, bindingYAML("operators", "  packages:\n"+metallbMember))
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if got := selectedPackages(t, state); !slices.Equal(got, []string{"metallb"}) {
		t.Fatalf("a MetalLB-only binding selected %v, want only metallb", got)
	}
	binding, _ := state.Effective().Find(api.ClusterAddonBinding, "operators")
	if binding.Spec().Get("packages").Has("nmstate") {
		t.Fatalf("the binding inherited the nmstate member: %v", binding.Spec().Get("packages"))
	}
}

func TestKindDefaultsFillADeclaredMember(t *testing.T) {
	for _, test := range []struct{ name, member, timeout, timeoutOrigin string }{
		{"member with another field", "    nmstate: {timeout: 20m}\n", "20m", "authored"},
		{"explicit empty member", "    nmstate: {}\n", "31m", "defaults.ClusterAddonBinding"},
	} {
		t.Run(test.name, func(t *testing.T) {
			state, err := compileKindDefaults(t, bindingYAML("operators", "  packages:\n"+test.member))
			if err != nil {
				t.Fatal(diagnostics.Of(err))
			}
			binding, _ := state.Effective().Find(api.ClusterAddonBinding, "operators")
			nmstate := binding.Spec().Get("packages", "nmstate")
			if got := nmstate.Get("subscription", "channel").Text(); got != "stable" {
				t.Fatalf("the declared member did not inherit the subscription: %v", nmstate)
			}
			if got := nmstate.Get("timeout").Text(); got != test.timeout {
				t.Fatalf("timeout %q, want %q", got, test.timeout)
			}
			origins := map[string]string{}
			for _, origin := range state.FieldOrigins(binding.Identity()) {
				origins[origin.Field] = origin.Origin
			}
			if origins["$.spec.packages.nmstate.subscription.channel"] != "defaults.ClusterAddonBinding" || origins["$.spec.packages.nmstate.timeout"] != test.timeoutOrigin {
				t.Fatalf("origins do not name the inherited fields: %v", origins)
			}
		})
	}
}

func TestKindDefaultsInheritNoMemberWhenNoneIsDeclared(t *testing.T) {
	for name, members := range map[string]string{"absent": "", "empty": "  packages: {}\n"} {
		t.Run(name, func(t *testing.T) {
			_, err := compileKindDefaults(t, bindingYAML("operators", members))
			if err == nil {
				t.Fatal("a binding with no declared member was admitted")
			}
			found := false
			for _, issue := range diagnostics.Of(err) {
				found = found || strings.Contains(issue.Message, "selects no profile, add-on or package")
			}
			if !found {
				t.Fatalf("expected the selects-nothing refusal: %v", diagnostics.Of(err))
			}
		})
	}
}

func TestKindDefaultsRaiseNoDuplicateAcrossBindings(t *testing.T) {
	state, err := compileKindDefaults(t,
		bindingYAML("a-network", "  packages:\n    nmstate: {}\n"),
		bindingYAML("b-balancer", "  packages:\n"+metallbMember))
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if got := selectedPackages(t, state); !slices.Equal(got, []string{"metallb", "nmstate"}) {
		t.Fatalf("selected %v, want metallb and nmstate once each", got)
	}
}
