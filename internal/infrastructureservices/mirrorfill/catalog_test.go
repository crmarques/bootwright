package mirrorfill

import (
	"context"
	"encoding/json"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactcatalog"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type catalogFixture struct {
	entry artifactcatalog.Entry
	path  string
}

func (f catalogFixture) ResolveArtifact(_ context.Context, _ string) (artifactcatalog.Entry, string, error) {
	return f.entry, f.path, nil
}

func TestCatalogSourcePinsTheHelmArchiveWithoutChangingItsServedPath(t *testing.T) {
	const path = "helm/v4.3.1/helm-v4.3.1-linux-amd64.tar.gz"
	entry := artifactcatalog.Entry{Name: "helm-archive", SHA256: helmDigest, Size: 5}
	selectArtifact := func(api.Catalog) ([]Artifact, error) {
		return []Artifact{{Bytes: 5, Location: mirrorURL + "/" + path, SHA256: helmDigest, CatalogArtifact: entry.Name}}, nil
	}
	for _, origin := range []string{"", "https://example.invalid/helm.tar.gz"} {
		entry.OriginURL = origin
		catalog := catalogFixture{entry: entry, path: "/private/store/payload"}
		capability := NewFiles(nil, selectArtifact, catalog)
		request, found, err := capability.Request(mirrorCatalog("mirror"), "controller", "lab")
		if err != nil || !found || len(request.Files) != 1 {
			t.Fatalf("request = %v, found %v, error %v", request, found, err)
		}
		file := request.Files[0]
		if file.Path != path || file.Source != origin || file.CatalogIdentity != entry.Identity() {
			t.Fatalf("frozen file = %#v", file)
		}
		claims := catalogClaims("lab", request)
		if len(claims) != 1 || !claims[0].Shared || len(claims[0].Keys) != 1 {
			t.Fatalf("catalog claims = %#v", claims)
		}
		values, err := capability.localSources(t.Context(), request)
		if err != nil || origin == "" && values["local_"+helmDigest] != catalog.path || origin != "" && len(values) != 0 {
			t.Fatalf("local transfer values = %v, error %v", values, err)
		}

		catalog.entry.Size++
		if _, _, err := NewFiles(nil, selectArtifact, catalog).Request(mirrorCatalog("mirror"), "controller", "lab"); err == nil {
			t.Fatal("accepted size other than the tool pin")
		}
	}
}

func TestLocalCatalogInputIsLentOnlyForTheUnchangedFrozenRegistration(t *testing.T) {
	const path = "helm/v4.3.1/helm-v4.3.1-linux-amd64.tar.gz"
	entry := artifactcatalog.Entry{Name: "helm-archive", SHA256: helmDigest, Size: 5}
	catalog := &catalogFixture{entry: entry, path: "/private/catalog/payload"}
	selectArtifact := func(api.Catalog) ([]Artifact, error) {
		return []Artifact{{Bytes: 5, Location: mirrorURL + "/" + path, SHA256: helmDigest, CatalogArtifact: entry.Name}}, nil
	}
	runner := &scriptedRunner{}
	capability := NewFiles(runner, selectArtifact, catalog)
	request, found, err := capability.Request(mirrorCatalog("mirror"), "controller", "lab")
	if err != nil || !found {
		t.Fatalf("request = %v, found %v, error %v", request, found, err)
	}
	data, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	definition := filesDefinition(reconciliation.Apply, request, data)
	digest, err := reconciliation.RequestDigest(definition)
	if err != nil {
		t.Fatal(err)
	}
	block := reconciliation.Block{BlockDefinition: definition, RequestDigest: digest}
	claims, err := capability.SharedClaims(block)
	if err != nil || len(claims) != 1 {
		t.Fatalf("unexpected claims: %v, %v", claims, err)
	}
	invalid := block
	invalid.Request = []byte("{}")
	if _, err := capability.SharedClaims(invalid); err == nil {
		t.Fatal("a malformed request could claim an artifact")
	}
	evidence, err := json.Marshal(FilesEvidence{
		Files: []FileEvidence{{Path: path, Verified: true}}, Postcondition: true, Request: digest, Version: FilesEvidenceVersion,
	})
	if err != nil {
		t.Fatal(err)
	}
	runner.result = lifecycle.RunResult{Outcome: reconciliation.OutcomeUnchanged, Evidence: evidence}
	execution := lifecycle.Execution{Attempt: 1, Context: "lab", Block: block}
	if _, err := capability.Apply(t.Context(), execution); err != nil {
		t.Fatal(err)
	}
	if runner.request.PublicFiles["local_"+helmDigest] != catalog.path || len(runner.request.MaterialValues) != 0 {
		t.Fatal("the runner did not receive only the public payload")
	}
	catalog.path = ""
	if _, err := capability.Apply(t.Context(), execution); err == nil {
		t.Fatal("an unavailable payload reached the adapter")
	}
	catalog.path = "/private/catalog/payload"
	catalog.entry.OriginURL = "https://example.invalid/replaced"
	if _, err := capability.Apply(t.Context(), execution); err == nil {
		t.Fatal("a changed origin was usable under the earlier request")
	}
}
