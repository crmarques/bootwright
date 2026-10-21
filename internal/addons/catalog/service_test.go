package catalog_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/addons/catalog"
	"github.com/crmarques/bootwright/internal/addons/packages"
)

func TestListUsesOnlyImmutableRegistry(t *testing.T) {
	service := catalog.New(packages.Builtin())
	result, err := service.List(context.Background(), catalog.ListRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.AddOns) != 2 || result.AddOns[0].Name != "metallb" || result.AddOns[1].Name != "nmstate" {
		t.Fatalf("listing: %+v", result)
	}
	if !slices.Contains(result.AddOns[0].Configuration, "pools") || result.AddOns[1].OLMPackage != "kubernetes-nmstate-operator" {
		t.Fatal(result)
	}
	result.AddOns[0].Name = "changed"
	result.AddOns[1].Provides[0] = "changed"
	again, err := service.List(context.Background(), catalog.ListRequest{})
	if err != nil || again.AddOns[0].Name != "metallb" || again.AddOns[1].Provides[0] != "nmstate" {
		t.Fatal("listing mutated registry")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.List(ctx, catalog.ListRequest{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
