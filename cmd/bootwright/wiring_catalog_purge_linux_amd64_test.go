//go:build linux && amd64

package main

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactcatalog"
	"github.com/crmarques/bootwright/internal/workspace/catalogpurge"
)

func TestPurgeUsesSavedArtifactAndMediaReferences(t *testing.T) {
	ctx := t.Context()
	const contextName = "lab-sno-mirror-host"
	const declared = "saved-helm"
	const unused = "spare-helm"
	sources := exampleDirectory(t, contextName)
	for i, file := range sources.Files {
		if filepath.Base(file.Path()) == "environment.yaml" {
			before := "      serverRef: mirror-artifacts"
			after := "      toolSources:\n        helm:\n          catalogArtifact: " + declared + "\n\n" + before
			if !strings.Contains(string(file.Bytes()), before) {
				t.Fatal("missing the test artifact mirror")
			}
			original := strings.Replace(string(file.Bytes()), before, after, 1)
			defaults := "spec:\n  defaults:\n    MachineImage:\n      bootMedia: local-media:dormant-default.iso\n\n" +
				"    Environment:\n      mirror:\n        artifacts:\n          toolSources:\n            helm:\n              catalogArtifact: dormant-archive\n\n"
			sources.Files[i] = desiredstate.NewSourceFile(file.Path(), []byte(strings.Replace(original, "spec:\n", defaults, 1)))
		}
	}
	refs := catalogPurgeReferences(wireCompiler())
	if names, err := refs(ctx, catalogpurge.Media, sources); err != nil || !slices.Contains(names, "dormant-default.iso") {
		t.Fatalf("media refs %v: %v", names, err)
	}
	if names, err := refs(ctx, catalogpurge.Artifact, sources); err != nil || !slices.Equal(names, []string{"dormant-archive", declared}) {
		t.Fatalf("artifact refs %v: %v", names, err)
	}
	services, store := productionSourcesContext(t, contextName, contextName, sources)
	services.CatalogPurge = catalogpurge.Service{Store: store, References: refs}
	mediaPreview, _ := contextRun(t, services, 0, "catalog", "media", "purge", "--dry-run", "--output", "json")
	if !strings.Contains(mediaPreview, `"catalog":"media"`) || !strings.Contains(mediaPreview, `"entries":[]`) {
		t.Fatalf("media purge did not return an empty preview: %s", mediaPreview)
	}

	for _, name := range []string{declared, unused} {
		if _, _, err := store.AddArtifact(ctx, artifactcatalog.Entry{Name: name}, strings.NewReader("synthetic Helm archive")); err != nil {
			t.Fatal(err)
		}
	}
	contextRun(t, services, 2, "catalog", "artifact", "purge", "--force", "--output", "json")
	preview, _ := contextRun(t, services, 0, "catalog", "artifact", "purge", "--force", "--dry-run", "--output", "json")
	assertPurgePreview(t, preview, declared, unused, contextName)
	if rows, err := store.ListArtifacts(ctx); err != nil || len(rows) != 2 {
		t.Fatalf("dry run changed registrations: %v, %v", rows, err)
	}
	contextRun(t, services, 0, "catalog", "artifact", "purge", "--yes")
	if rows, err := store.ListArtifacts(ctx); err != nil || len(rows) != 1 || rows[0].Name != declared {
		t.Fatalf("default purge left %v: %v", rows, err)
	}
	contextRun(t, services, 0, "catalog", "artifact", "purge", "--force", "--yes")
	if rows, err := store.ListArtifacts(ctx); err != nil || len(rows) != 0 {
		t.Fatalf("force left registrations %v: %v", rows, err)
	}
}

func assertPurgePreview(t *testing.T, encoded, declared, unused, holder string) {
	t.Helper()
	if strings.Contains(encoded, "identity") || strings.Contains(encoded, "origin") {
		t.Fatalf("purge output leaked internal fields: %s", encoded)
	}
	var document struct {
		Result struct {
			Kind    string `json:"catalog"`
			DryRun  bool   `json:"dryRun"`
			Force   bool   `json:"force"`
			Entries []struct {
				Name         string   `json:"name"`
				Action       string   `json:"action"`
				ReferencedBy []string `json:"referencedBy"`
			} `json:"entries"`
		} `json:"result"`
	}
	err := json.Unmarshal([]byte(encoded), &document)
	result := document.Result
	if err != nil || result.Kind != "artifact" || !result.DryRun || !result.Force || len(result.Entries) != 2 ||
		result.Entries[0].Name != declared || result.Entries[0].Action != "would delete" ||
		!slices.Equal(result.Entries[0].ReferencedBy, []string{holder}) || result.Entries[1].Name != unused {
		t.Fatalf("unexpected purge preview: %s (%v)", encoded, err)
	}
}
