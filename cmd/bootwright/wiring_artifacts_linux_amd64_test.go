//go:build linux && amd64

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/clients"
	"github.com/crmarques/bootwright/internal/controller/tooltable"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactcatalog"
	"github.com/crmarques/bootwright/internal/infrastructureservices/mirrorfill"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func TestCatalogHelmSelectionReachesTheProductionMirrorPlan(t *testing.T) {
	const example = "lab-sno-mirror-host"
	const archive = "approved-helm"
	sources := exampleDirectory(t, example)
	for index, file := range sources.Files {
		if filepath.Base(file.Path()) == "environment.yaml" {
			before := "      serverRef: mirror-artifacts"
			after := "      toolSources:\n        helm:\n          catalogArtifact: " + archive + "\n\n" + before
			sources.Files[index] = desiredstate.NewSourceFile(file.Path(), []byte(strings.Replace(string(file.Bytes()), before, after, 1)))
		}
		if filepath.Base(file.Path()) == "mirror-artifacts.yaml" {
			sources.Files[index] = desiredstate.NewSourceFile(file.Path(), []byte(strings.Replace(string(file.Bytes()), "port: 443", "port: 8443", 1)))
		}
	}
	state, _ := compileAcceptance(t, sources)
	var helmMirror string
	tools, err := controller.SelectTools(state.Effective())
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools {
		if tool.Kind == "helm" {
			helmMirror = tool.Mirror
		}
	}
	if !strings.Contains(helmMirror, ":8443/mirror/") {
		t.Fatalf("Helm did not use the configured port: %s", helmMirror)
	}
	selected, err := controller.Select(state.Effective())
	if err != nil {
		t.Fatal(err)
	}
	trust := clients.NewRequest(selected, tools).WithMirrorTrust(state.Effective()).MirrorTrust
	if trust == nil || !strings.HasSuffix(trust.Host, ":8443") {
		t.Fatalf("Helm did not scope its trust anchor to the configured port: %+v", trust)
	}
	capability := mirrorfill.NewFiles(nil, acquisitionTools().MirrorFiles)
	args := lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: example}, Controller: "controller", State: state}
	if _, err := capability.Plan(t.Context(), args); len(diagnostics.Of(err)) != 1 || diagnostics.Of(err)[0].Code != "artifact.catalog" {
		t.Fatalf("explicit catalog requirement produced %v without a catalog", err)
	}

	_, store := productionSourcesContext(t, example, example, sources)
	pin, found, err := tooltable.PinOf("helm")
	if err != nil || !found {
		t.Fatalf("missing build Helm pin: %v", err)
	}
	entry := artifactcatalog.Entry{Name: archive, OriginURL: "https://downloads.example.test/helm.tar.gz", Size: pin.Default.Bytes, SHA256: pin.Default.SHA256}
	if _, _, err := store.AddArtifact(t.Context(), entry, nil); err != nil {
		t.Fatal(err)
	}
	capability = mirrorfill.NewFiles(nil, acquisitionTools().MirrorFiles, store)
	plan, err := capability.Plan(t.Context(), args)
	if err != nil || len(plan.Definitions) != 1 || len(plan.Reservations) != 1 {
		t.Fatalf("plan = %#v, error %+v", plan, diagnostics.Of(err))
	}
	request, err := mirrorfill.DecodeFilesRequest(plan.Definitions[0].Request)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range request.Files {
		if file.CatalogArtifact == archive && file.Source == entry.OriginURL && file.SHA256 == entry.SHA256 && request.Placement.Machine == "mirror" {
			return
		}
	}
	t.Fatalf("the selected file did not reach the mirror's frozen request: %#v", request)
}

func TestCatalogCLIImportsARelativeFileAndKeepsItsOwnCopy(t *testing.T) {
	files := newVirtualOperatorFiles(t)
	absolute := files.write(t, "helm.tar.gz", "fixture archive")
	current, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(current, absolute)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "state")
	repository := testRepository(root)
	deps := testContextWiring(t, root)
	deps.Repository, deps.Workspace, deps.Artifacts, deps.Files = repository, repository, repository, files
	deps.Confirmer = policyConfirmer(func(_ context.Context, action, subject string) error {
		if action != "delete catalog artifact" || !strings.HasPrefix(subject, "helm-archive (sha256:") {
			t.Fatalf("unexpected deletion confirmation %q, %q", action, subject)
		}
		return nil
	})
	services := assembleServices(deps)
	input := t.TempDir()
	addSecretInput(t, input, "environment.yaml", syntheticEnvironment)
	addSecretInput(t, input, "controller.yaml", serviceHost)
	services.Contexts = testServices(t, repository, root).Contexts
	contextRun(t, services, 0, "context", "init", "--name", "demo", "--input-dir", input)
	args := []string{"catalog", "artifact", "add", "--name", "helm-archive", "--from-file", relative}
	out, _ := contextRun(t, services, 0, args...)
	if !strings.Contains(out, "Artifact registered") || strings.Join(files.take(), "|") != absolute {
		t.Fatalf("relative import output %q or unexpected invoking-account path", out)
	}
	out, _ = contextRun(t, services, 0, args...)
	if !strings.Contains(out, "Artifact unchanged") {
		t.Fatalf("repeated import output %q", out)
	}
	contextRun(t, services, 0, "catalog", "artifact", "show", "--name", "helm-archive", "--output", "json")
	contextRun(t, services, 0, "catalog", "artifact", "delete", "--name", "helm-archive")
	contextRun(t, services, 1, "catalog", "artifact", "show", "--name", "helm-archive")
}
