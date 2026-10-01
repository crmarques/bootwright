//go:build linux && amd64

package contextfs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/containercluster/agentinstall"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/bundlelocal"
	"github.com/crmarques/bootwright/internal/controller/clients"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// retainedCatalog recovers the closure only from the sources the store
// retained, exactly as the catalog's Select does, so what the lookup finds is
// what the host's own evidence names.
type retainedCatalog struct {
	closure []prerequisites.ToolDefinition
}

func (c retainedCatalog) Select(_ []controller.ToolRequest, retained []prerequisites.DependencySource) ([]prerequisites.ToolDefinition, bool, error) {
	selected := []prerequisites.ToolDefinition{}
	for _, tool := range c.closure {
		if slices.Contains(retained, tool.Source) {
			selected = append(selected, tool)
		}
	}
	return selected, len(selected) == len(c.closure), nil
}

func (c retainedCatalog) Resolve(context.Context, []controller.ToolRequest, prerequisites.SetupEgress) ([]prerequisites.ToolDefinition, error) {
	return slices.Clone(c.closure), nil
}

func (c retainedCatalog) Present(ctx context.Context, area prerequisites.BundleArea, tools []prerequisites.ToolDefinition) (bool, error) {
	entries, err := area.Entries(ctx)
	if err != nil {
		return false, err
	}
	for _, tool := range tools {
		for _, file := range tool.Files {
			if !slices.ContainsFunc(entries, func(entry prerequisites.BundleEntry) bool { return entry.Path == file.Path && !entry.Directory }) {
				return false, nil
			}
		}
	}
	return true, nil
}

// publishedSource is the source a publisher serves for one release of a
// request, under the identity and URL internal/controller/bundlelocal/tools.go
// derives for it, so the catalog's own Select accepts it.
func publishedSource(t *testing.T, request controller.ToolRequest, version, digit string) prerequisites.DependencySource {
	t.Helper()
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	url := "https://get.helm.sh/helm-" + version + "-linux-amd64.tar.gz"
	if request.Kind == "openshift-clients" || request.Kind == "openshift-install" {
		name := strings.Replace(request.Kind, "clients", "client", 1)
		url = "https://mirror.openshift.com/pub/openshift-v4/clients/ocp/" + version + "/" + name + "-linux-" + version + ".tar.gz"
	}
	return prerequisites.DependencySource{
		ID:  "tool-" + request.Kind + "-" + hex.EncodeToString(digest[:16]) + "-" + version,
		URL: url, SHA256: strings.Repeat(digit, 64), Bytes: 4096,
	}
}

// publisherCatalog is the tool catalog's own Select, over a publisher that
// serves exactly the sources it holds, so the closure a stage resolves is the
// one a later Select recovers from what the store retained, and a newer
// release retained beside it is selected as the real catalog selects it.
// Present lists only the published executables, as retainedCatalog does,
// because the installer below writes no source archive.
type publisherCatalog struct {
	*bundlelocal.ToolCatalog
	published []prerequisites.DependencySource
}

func (c publisherCatalog) Resolve(_ context.Context, requests []controller.ToolRequest, _ prerequisites.SetupEgress) ([]prerequisites.ToolDefinition, error) {
	tools, complete, err := c.Select(requests, c.published)
	if err == nil && !complete {
		err = errors.New("the publisher serves no release of a requested client")
	}
	return tools, err
}

func (publisherCatalog) Present(ctx context.Context, area prerequisites.BundleArea, tools []prerequisites.ToolDefinition) (bool, error) {
	return retainedCatalog{}.Present(ctx, area, tools)
}

// publishingInstaller writes each executable the frozen closure names into the
// area it was handed, as the dependency role does.
type publishingInstaller struct{}

func (publishingInstaller) Clients(ctx context.Context, installation prerequisites.ClientInstallation) (prerequisites.ActionResult, error) {
	for _, tool := range installation.Definition.Tools {
		for _, file := range tool.Files {
			if err := installation.Target.EnsureDirectory(ctx, path.Dir(file.Path)); err != nil {
				return prerequisites.ActionResult{Outcome: "failed"}, err
			}
			if err := installation.Target.Write(ctx, file.Path, []byte("#!/bin/sh\necho "+file.Member+"\n"), true); err != nil {
				return prerequisites.ActionResult{Outcome: "failed"}, err
			}
		}
	}
	return prerequisites.ActionResult{Outcome: "changed"}, nil
}

type noNative struct{}

func (noNative) Resolve(context.Context, prerequisites.Platform, prerequisites.NativeRequirements, controller.DependencyVersions, prerequisites.SetupEgress) (prerequisites.NativeResolvedPlan, error) {
	return prerequisites.NativeResolvedPlan{}, nil
}

func (noNative) Check(context.Context, prerequisites.NativeResolvedPlan) (prerequisites.NativePresence, error) {
	return prerequisites.NativePresence{}, nil
}

// resolvedSetupFixture completes a setup whose receipt carries the resolved
// definition the controller stage extends.
func resolvedSetupFixture(t *testing.T, store *Store, record contexts.Record) {
	t.Helper()
	ctx := context.Background()
	scope := prerequisites.SetupContext{Name: record.Name, Revision: record.Revision, Machine: "controller"}
	value := syntheticControllerState(t, scope)
	definition := syntheticResolution(t)
	value.Receipt.Definition = &definition
	value.Receipt.CatalogDigest = definition.CatalogDigest
	value.Receipt.Sources = slices.Clone(definition.Sources)
	var err error
	if value.Receipt.PlanDigest, err = prerequisites.SetupPlanDigest(value.Host, value.Receipt); err != nil {
		t.Fatal(err)
	}
	publishControllerState(t, store, scope, value)
	err = store.MutateController(ctx, scope, false, func(tx prerequisites.StorageTransaction) error {
		intended := tx.Snapshot().State
		intended.Receipt.Actions[0].Phase = "intent"
		if _, err := tx.Publish(ctx, intended); err != nil {
			return err
		}
		area, err := tx.Bundle(ctx, value.Receipt.CatalogDigest)
		if err != nil {
			return err
		}
		if err := area.Write(ctx, "manifest.json", []byte("{}\n"), false); err != nil {
			return err
		}
		_, err = tx.Publish(ctx, completeControllerState(tx.Snapshot().State))
		return err
	})
	if err != nil {
		t.Fatalf("setup publication failed: %#v", diagnostics.Of(err))
	}
}

// The controller stage retains the sources it acquired and seals the area its
// closure names, and the store refuses any retained resolution that carries
// clients. The install, in a later operation, locates the installer and the
// client that stage installed from its proof alone, even once another context
// on this host retained a newer helm under the same latest intent, whose
// closure this context never published.
func TestTheInstallLocatesTheClientsTheControllerStageInstalled(t *testing.T) {
	ctx := context.Background()
	store, record := lifecycleFixture(t)
	resolvedSetupFixture(t, store, record)
	request := clients.Request{
		Egress: prerequisites.SetupEgress{NoProxy: []string{}}, Machine: "controller", Version: clients.Version,
		Tools: []clients.ToolRequest{
			{Kind: "helm", Version: "latest"},
			{Compatibility: "openshift", Kind: "openshift-clients", Version: "4.19.3"},
			{Compatibility: "openshift", Kind: "openshift-install", Version: "4.19.3"},
		},
	}
	requests := request.ToolRequests()
	catalog := publisherCatalog{ToolCatalog: bundlelocal.NewToolCatalog()}
	for _, tool := range requests {
		version := tool.Version
		if tool.Kind == "helm" {
			version = "v3.19.0"
		}
		catalog.published = append(catalog.published, publishedSource(t, tool, version, "a"))
	}
	stage := clients.New(catalog, noNative{}, noNative{}, publishingInstaller{})
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	block := reconciliation.Block{
		BlockDefinition: reconciliation.BlockDefinition{
			ID: clients.BlockID, Stage: reconciliation.StageController, Kind: clients.Kind,
			Implementation: clients.Implementation, Request: canonical,
		},
		RequestDigest: strings.Repeat("f", 64),
	}
	var proved lifecycle.BlockEvidence
	err = store.MutateLifecycle(ctx, record.Name, func(tx lifecycle.Transaction) error {
		result, err := stage.Apply(ctx, lifecycle.Execution{Block: block, Stage: &lifecycle.ControllerStage{
			Setup: tx.Controller(), ClientArea: tx.ClientArea, SealClientArea: tx.SealClientArea,
			RetainDependencies: tx.RetainDependencies,
			Prepare:            func(context.Context, prerequisites.NativePreparation) error { return nil },
			ReleaseFoundation:  func() error { return nil },
		}})
		if err == nil && result.Outcome != reconciliation.OutcomeChanged {
			t.Fatalf("the controller stage reported %q", result.Outcome)
		}
		proved = lifecycle.BlockEvidence{
			Kind: block.Kind, Object: block.Object, Implementation: block.Implementation,
			Verb: reconciliation.Apply, State: reconciliation.BlockDone, Evidence: result.Evidence,
		}
		return err
	})
	if err != nil {
		t.Fatalf("the controller stage failed: %#v", diagnostics.Of(err))
	}
	published, _, err := catalog.Select(requests, catalog.published)
	if err != nil {
		t.Fatal(err)
	}
	area := prerequisites.ToolsDigest(published)
	if reservation := clientReservation(t, store, area); reservation.Mode != "sealed" {
		t.Fatalf("the stage left its client area %+v", reservation)
	}
	newer := []prerequisites.DependencySource{}
	for _, tool := range requests {
		version := "v3.20.0"
		if tool.Kind != "helm" {
			tool.Version, version = "4.22.3", "4.22.3"
		}
		newer = append(newer, publishedSource(t, tool, version, "b"))
	}
	err = store.MutateLifecycle(ctx, record.Name, func(tx lifecycle.Transaction) error {
		return tx.RetainDependencies(ctx, nil, newer, nil)
	})
	if err != nil {
		t.Fatalf("another context's stage could not retain its sources: %#v", diagnostics.Of(err))
	}
	err = store.MutateLifecycle(ctx, record.Name, func(tx lifecycle.Transaction) error {
		view := tx.Controller()
		if slices.ContainsFunc(view.State.RetainedDefinitions, func(definition prerequisites.Definition) bool { return len(definition.Tools) != 0 }) {
			t.Fatal("the store retained a resolution that carries clients")
		}
		if selected, complete, err := catalog.Select(requests, view.State.RetainedSources); err != nil || !complete || prerequisites.ToolsDigest(selected) == area {
			t.Fatalf("the newer helm another context retained is not what the host's sources now select (%v)", err)
		}
		execution := lifecycle.Execution{LocateTool: func(inner context.Context, tool controller.InstalledTool) (string, error) {
			return stage.LocateTool(inner, view, block, proved, tool)
		}}
		for executable, kind := range map[string]string{"openshift-install": "openshift-install", "oc": "openshift-clients"} {
			located, err := agentinstall.ToolPath(ctx, execution, agentinstall.Tool{Compatibility: "openshift", Kind: kind, Version: "4.19.3"}, executable)
			if err != nil {
				t.Fatalf("the install could not locate its %s: %#v", executable, diagnostics.Of(err))
			}
			want := filepath.Join(store.options.Root, "controller", "bundles", area, "tools", kind, "openshift", "4.19.3", executable)
			if located != want {
				t.Fatalf("the %s was located at %q, want %q", executable, located, want)
			}
			if data, err := os.ReadFile(located); err != nil || string(data) != "#!/bin/sh\necho "+executable+"\n" {
				t.Fatalf("the located %s holds %q (%v)", executable, data, err)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("the lookup failed: %#v", diagnostics.Of(err))
	}
}
