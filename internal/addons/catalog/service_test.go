package catalog_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons/catalog"
	"github.com/crmarques/bootwright/internal/addons/packages"
)

func TestListUsesOnlyImmutableRegistry(t *testing.T) {
	service := catalog.New(packages.Builtin())
	result, err := service.List(context.Background(), catalog.ListRequest{})
	if err != nil {
		t.Fatal(err)
	}
	entries := packages.Builtin().Packages()
	if len(result.AddOns) != len(entries) {
		t.Fatalf("listing: %+v", result)
	}
	for i, p := range entries {
		got := result.AddOns[i]
		olmPackage, source := "", ""
		if p.OLM != nil {
			olmPackage, source = p.OLM.Package, p.OLM.DefaultSource
		}
		if got.Name != p.Name || !slices.Equal(got.Provides, p.Provides) || !slices.Equal(got.Requires, p.Requires) || got.OLMPackage != olmPackage || got.DefaultSource != source {
			t.Fatalf("listing row %d = %+v, registry package %+v", i, got, p)
		}
		member, ok := api.Schema(api.ClusterAddonBinding).Field("packages")
		if !ok {
			t.Fatal("the binding schema has no spec.packages")
		}
		shape, ok := member.Shape.Field(p.Name)
		if !ok {
			t.Fatalf("package %s has no spec.packages member", p.Name)
		}
		want := []string{}
		for _, field := range shape.Shape.Fields {
			want = append(want, field.Name)
		}
		slices.Sort(want)
		if !slices.Equal(got.Configuration, want) {
			t.Fatalf("package %s configuration = %v, schema %v", p.Name, got.Configuration, want)
		}
	}
	first, withProvides := result.AddOns[0].Name, slices.IndexFunc(result.AddOns, func(e catalog.Entry) bool { return len(e.Provides) > 0 })
	if withProvides < 0 {
		t.Fatal("no built-in package provides a capability to prove the clone")
	}
	provided := result.AddOns[withProvides].Provides[0]
	result.AddOns[0].Name = "changed"
	result.AddOns[withProvides].Provides[0] = "changed"
	again, err := service.List(context.Background(), catalog.ListRequest{})
	if err != nil || again.AddOns[0].Name != first || again.AddOns[withProvides].Provides[0] != provided {
		t.Fatal("listing mutated registry")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.List(ctx, catalog.ListRequest{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
