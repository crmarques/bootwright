package catalog_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/addons/catalog"
	"github.com/crmarques/bootwright/internal/addons/packages"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func describe(t *testing.T, name string) catalog.Description {
	t.Helper()
	result, err := catalog.New(packages.Builtin()).Describe(context.Background(), catalog.DescribeRequest{Name: name})
	if err != nil {
		t.Fatal(err)
	}
	return result.Package
}

func field(t *testing.T, description catalog.Description, path string) catalog.Field {
	t.Helper()
	index := slices.IndexFunc(description.Configuration, func(f catalog.Field) bool { return f.Path == path })
	if index < 0 {
		t.Fatalf("%s has no field %s", description.Name, path)
	}
	return description.Configuration[index]
}

func TestDescribeDerivesEveryLeafFromTheSchema(t *testing.T) {
	metallb := describe(t, "metallb")
	for path, want := range map[string]catalog.Field{
		"subscription.channel":                {Path: "subscription.channel", Type: "string", Required: true},
		"subscription.sourceNamespace":        {Path: "subscription.sourceNamespace", Type: "name", Default: "openshift-marketplace"},
		"subscription.source":                 {Path: "subscription.source", Type: "DNS name", Default: "redhat-operators"},
		"subscription.catalogSource.priority": {Path: "subscription.catalogSource.priority", Type: "integer", RequiredWithin: "subscription.catalogSource"},
		"timeout":                             {Path: "timeout", Type: "duration", Default: "30m"},
		"pools[].addresses":                   {Path: "pools[].addresses", Type: "list of string", RequiredWithin: "pools[]"},
		"pools[].autoAssign":                  {Path: "pools[].autoAssign", Type: "boolean", Default: "true"},
	} {
		if got := field(t, metallb, path); !slices.Equal(got.Values, want.Values) || got.Path != want.Path || got.Type != want.Type || got.Required != want.Required || got.RequiredWithin != want.RequiredWithin || got.Default != want.Default {
			t.Errorf("%s = %+v, want %+v", path, got, want)
		}
	}
	if values := field(t, metallb, "subscription.catalogSource.securityContextConfig").Values; !slices.Equal(values, []string{"legacy", "restricted"}) {
		t.Errorf("securityContextConfig values = %v", values)
	}
	fdf := describe(t, "fdf")
	if got := field(t, fdf, "cephConsumerRef"); !got.Required || got.Type != "name of CephConsumer" {
		t.Errorf("cephConsumerRef = %+v", got)
	}
	if got := field(t, fdf, "subscription.source"); !got.Required || got.Default != "" {
		t.Errorf("fdf subscription.source = %+v", got)
	}
	if got := field(t, describe(t, "acm"), "mce.channel"); !got.Required {
		t.Errorf("mce.channel = %+v", got)
	}
}

func TestDescribeFactsAndSkeletonsPerPackage(t *testing.T) {
	for _, entry := range mustList(t) {
		t.Run(entry.Name, func(t *testing.T) {
			description := describe(t, entry.Name)
			if description.Summary == "" || description.OLMPackage != entry.OLMPackage || description.DefaultSource != entry.DefaultSource || !slices.Equal(description.Provides, entry.Provides) || !slices.Equal(description.Requires, entry.Requires) {
				t.Fatalf("%+v differs from the listing %+v", description, entry)
			}
			if want := "oc get packagemanifest " + entry.OLMPackage + " -n openshift-marketplace -o json"; description.Lookup != want {
				t.Fatalf("lookup %q, want %q", description.Lookup, want)
			}
			if !strings.HasPrefix(description.Binding, "apiVersion: bootwright.io/v1alpha1\nkind: ClusterAddonBinding\nmetadata:\n  name: "+entry.Name+"\nspec:\n  clusterRef: <clusterRef>\n  packages:\n    "+entry.Name+":\n") {
				t.Fatalf("skeleton:\n%s", description.Binding)
			}
			for _, f := range description.Configuration {
				if f.Required && !strings.Contains(description.Binding, "<"+f.Path[strings.LastIndex(f.Path, ".")+1:]+">") {
					t.Errorf("required %s has no placeholder in\n%s", f.Path, description.Binding)
				}
			}
			again := describe(t, entry.Name)
			again.Provides = append(again.Provides, "changed")
			if len(describe(t, entry.Name).Provides) != len(entry.Provides) {
				t.Fatal("description aliases the registry")
			}
		})
	}
	if binding := describe(t, "fdf").Binding; !strings.Contains(binding, "source: <source>") || !strings.Contains(binding, "cephConsumerRef: <cephConsumerRef>") {
		t.Errorf("fdf skeleton:\n%s", binding)
	}
	if binding := describe(t, "nmstate").Binding; !strings.Contains(binding, "source: redhat-operators") {
		t.Errorf("nmstate skeleton:\n%s", binding)
	}
}

func mustList(t *testing.T) []catalog.Entry {
	t.Helper()
	result, err := catalog.New(packages.Builtin()).List(context.Background(), catalog.ListRequest{})
	if err != nil {
		t.Fatal(err)
	}
	return result.AddOns
}

func TestDescribeRefusesAnUnknownNameWithARemedy(t *testing.T) {
	_, err := catalog.New(packages.Builtin()).Describe(context.Background(), catalog.DescribeRequest{Name: "metalb"})
	found := diagnostics.Of(err)
	if len(found) != 1 || found[0].Code != "access.target" || !strings.Contains(found[0].Remediation, "bootwright add-ons list") {
		t.Fatalf("diagnostics %+v", found)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := catalog.New(packages.Builtin()).Describe(ctx, catalog.DescribeRequest{Name: "metallb"}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
