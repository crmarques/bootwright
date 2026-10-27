package main

import (
	"context"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	effectiveencoding "github.com/crmarques/bootwright/internal/desiredstate/encoding"
)

func TestNFSExportEffectiveGolden(t *testing.T) {
	state, _ := compileAcceptance(t, admissibleExampleSources(t, false))
	export := requireObject(t, state.Effective(), api.CephNFSExport, "acceptance-nfs")
	data, err := effectiveencoding.YAML(context.Background(), api.NewCatalog([]api.Object{export}))
	if err != nil {
		t.Fatal(err)
	}
	matchesTextGolden(t, "admission/nfs-export-effective", data)
}
