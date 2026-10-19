package main

import (
	"context"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	effectiveencoding "github.com/crmarques/bootwright/internal/desiredstate/encoding"
)

func TestGatewayEffectiveGolden(t *testing.T) {
	state, _ := compileAcceptance(t, admissibleExampleSources(t, false))
	gateway := requireObject(t, state.Effective(), api.StorageObjectGateway, "objects")
	data, err := effectiveencoding.YAML(context.Background(), api.NewCatalog([]api.Object{gateway}))
	if err != nil {
		t.Fatal(err)
	}
	matchesTextGolden(t, "admission/gateway-effective", data)
}
