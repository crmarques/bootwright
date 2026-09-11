package prerequisites

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller"
)

type testTools struct {
	owner         *fixture
	resolves      int
	fail          error
	beforeResolve func()
}

func (c *testTools) Select(requests []controller.ToolRequest, retained []DependencySource) ([]ToolDefinition, bool, error) {
	tools, complete := []ToolDefinition{}, true
	for _, request := range requests {
		index := slices.IndexFunc(retained, func(source DependencySource) bool { return source.ID == "tool-"+request.Kind })
		if index == -1 {
			complete = false
			continue
		}
		tools = append(tools, ToolDefinition{Kind: request.Kind, Version: "1.2.3", Compatibility: request.Compatibility, Archive: "binary", Source: retained[index], Files: []ToolFile{{Member: request.Kind, Path: "tools/" + request.Kind + "/1.2.3/" + request.Kind}}})
	}
	return tools, complete, nil
}

func (c *testTools) Resolve(_ context.Context, requests []controller.ToolRequest, _ SetupEgress) ([]ToolDefinition, error) {
	c.resolves++
	c.owner.events = append(c.owner.events, "resolve")
	if c.beforeResolve != nil {
		c.beforeResolve()
	}
	if c.fail != nil {
		return nil, c.fail
	}
	sources := []DependencySource{}
	for _, request := range requests {
		sources = append(sources, DependencySource{ID: "tool-" + request.Kind, URL: "https://downloads.example.test/" + request.Kind, SHA256: strings.Repeat("e", 64), Bytes: 64})
	}
	tools, _, err := c.Select(requests, sources)
	return tools, err
}

func toolsFixture(t *testing.T) (*fixture, *testRuntimeInstaller, *testTools) {
	t.Helper()
	f, installer := explicitRuntimeFixture(t)
	f.host.runtime = RuntimeInspection{Present: true, Ready: true}
	f.compiler.extra = []api.Object{api.NewObject(api.ContainerCluster, "cluster", api.Value{}, api.MapValue().WithPath(api.StringValue("openshift"), "distribution", "type").WithPath(api.StringValue("4.21.15"), "distribution", "release", "version"))}
	catalog := &testTools{owner: f}
	f.service.options.Tools = catalog
	wireResolution(t, f)
	return f, installer, catalog
}

func TestTargetToolsResolveBeforeConfirmationAndInstallEvenWithReadyRuntime(t *testing.T) {
	f, installer, catalog := toolsFixture(t)
	report, err := f.service.Setup(context.Background(), SetupRequest{ContextName: "example"})
	if err != nil || report.Outcome != "changed" || installer.calls != 1 || catalog.resolves != 3 || !f.bundle.toolsReady {
		t.Fatalf("report=%#v err=%v installs=%d resolves=%d", report, err, installer.calls, catalog.resolves)
	}
	confirm := slices.Index(f.events, "confirm")
	for index, event := range f.events {
		if event == "resolve" && index >= confirm {
			t.Fatal("metadata resolution happened after confirmation")
		}
	}
	// The frozen closure is the complete dependency set: bootstrap, native and
	// every selected target client.
	if f.store.state.Receipt.CatalogDigest == f.catalog.definition.CatalogDigest || len(f.store.state.RetainedSources) != 9 {
		t.Fatalf("target source closure was not frozen: %#v", f.store.state.RetainedSources)
	}
	for _, id := range []string{"tool-helm", "tool-openshift-clients", "tool-openshift-install"} {
		if !slices.ContainsFunc(f.store.state.RetainedSources, func(source DependencySource) bool { return source.ID == id }) {
			t.Fatal("target tool source was not retained", id)
		}
	}
	writes := f.store.writes
	report, err = f.service.Setup(context.Background(), SetupRequest{ContextName: "example"})
	// Helm has no exact desired-state version, so a fresh setup resolves latest
	// again; the releases are unchanged, so it still settles as a verified no-op.
	if err != nil || report.Outcome != "unchanged" || installer.calls != 1 || catalog.resolves != 4 || f.store.writes != writes {
		t.Fatalf("no-op changed frozen tools: %#v %v resolves=%d", report, err, catalog.resolves)
	}
}

func TestToolPlanningAndPreflightNeverResolveMetadata(t *testing.T) {
	f, installer, catalog := toolsFixture(t)
	_, err := f.service.Setup(context.Background(), SetupRequest{ContextName: "example", DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	report, err := f.service.Check(context.Background(), CheckRequest{ContextName: "example"})
	if code(err) != "preflight.failed" || report.Outcome != "not-ready" || catalog.resolves != 0 || installer.calls != 0 || f.store.writes != 0 {
		t.Fatalf("inspection crossed effect boundary: %#v %v", report, err)
	}
}

func TestToolMetadataFailureOrChangedInputCannotStartInstallation(t *testing.T) {
	for _, mode := range []string{"metadata", "input"} {
		t.Run(mode, func(t *testing.T) {
			f, installer, catalog := toolsFixture(t)
			if mode == "metadata" {
				catalog.fail = errors.New("metadata unavailable")
			} else {
				catalog.beforeResolve = func() { f.store.scope.Revision = "rev-" + strings.Repeat("3", 32) }
			}
			_, err := f.service.Setup(context.Background(), SetupRequest{ContextName: "example", SkipConfirmation: true})
			if err == nil || installer.calls != 0 || f.store.writes != 0 || slices.Contains(f.events, "present") {
				t.Fatalf("unapproved metadata/input caused effects: %v", err)
			}
		})
	}
}

func TestUncertainTargetInstallationUsesFrozenMetadataOnRetry(t *testing.T) {
	f, installer, catalog := toolsFixture(t)
	installer.err = errors.New("result lost")
	installer.result = ActionResult{Outcome: "unknown"}
	_, err := f.service.Setup(context.Background(), SetupRequest{ContextName: "example", SkipConfirmation: true})
	if err == nil || catalog.resolves != 3 || !f.store.state.Receipt.Incomplete() {
		t.Fatal("missing pending receipt")
	}
	installer.recoveryError = failure("controller.unknown", "native inventory is not attributable", "restore exact native evidence")
	_, err = f.service.Setup(context.Background(), SetupRequest{ContextName: "example", SkipConfirmation: true})
	if code(err) != "controller.unknown" || catalog.resolves != 3 || installer.calls != 1 {
		t.Fatalf("uncertain tool work repeated: %v", err)
	}
	installer.recoveryError = nil
	_, err = f.service.Setup(context.Background(), SetupRequest{ContextName: "example", SkipConfirmation: true})
	if err != nil || catalog.resolves != 3 || installer.calls != 1 || installer.recovers != 2 || !f.bundle.toolsReady {
		t.Fatalf("frozen recovery failed: %v", err)
	}
}

func TestUnattributablePartialToolsCannotResume(t *testing.T) {
	f, installer, catalog := toolsFixture(t)
	installer.err = errors.New("result lost")
	installer.result = ActionResult{Outcome: "unknown"}
	_, _ = f.service.Setup(context.Background(), SetupRequest{ContextName: "example", SkipConfirmation: true})
	f.bundle.recoverable = false
	_, err := f.service.Setup(context.Background(), SetupRequest{ContextName: "example", SkipConfirmation: true})
	if code(err) != "controller.unknown" || installer.calls != 1 || installer.recovers != 0 || catalog.resolves != 3 {
		t.Fatalf("unattributable files resumed: %v", err)
	}
}

func TestDefiniteToolRefusalAllowsExactFreshAttempt(t *testing.T) {
	f, installer, catalog := toolsFixture(t)
	installer.result = ActionResult{Outcome: "failed", Evidence: object(map[string]any{"installationEntered": false})}
	installer.err = failure("controller.unsupported", "native transaction refused before effects", "prepare the required foundation")
	_, err := f.service.Setup(context.Background(), SetupRequest{ContextName: "example", SkipConfirmation: true})
	if err == nil || f.store.state.Receipt.Status != "failed" || f.bundle.sealed {
		t.Fatal("failure was not retained as an unsealed terminal attempt")
	}
	installer.err = nil
	installer.result = ActionResult{Outcome: "changed", Evidence: object(map[string]any{"nativePostcondition": "verified"})}
	report, err := f.service.Setup(context.Background(), SetupRequest{ContextName: "example", SkipConfirmation: true})
	// A fresh attempt after a terminal failure resolves latest again; the exact
	// releases stay frozen, so only the one latest client is re-resolved.
	if err != nil || report.Outcome != "changed" || catalog.resolves != 4 || installer.calls != 2 || !f.bundle.sealed {
		t.Fatalf("fresh tool attempt did not complete: %#v %v resolves=%d", report, err, catalog.resolves)
	}
}

func TestFailedReceiptDoesNotClaimReadyAfterManualPreparation(t *testing.T) {
	f, installer := explicitRuntimeFixture(t)
	f.store.scope = SetupContext{}
	installer.result = ActionResult{Outcome: "failed", Evidence: object(map[string]any{"installationEntered": false})}
	installer.err = failure("controller.unsupported", "native transaction refused before effects", "prepare the required foundation")
	_, _ = f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
	f.host.runtime = RuntimeInspection{Present: true, Ready: true}
	report, err := f.service.Check(context.Background(), CheckRequest{})
	if code(err) != "preflight.failed" || report.Outcome != "not-ready" || f.bundle.sealed {
		t.Fatalf("failed receipt claimed readiness: %#v %v", report, err)
	}
	report, err = f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
	if err != nil || report.Outcome != "changed" || !f.bundle.sealed || f.store.state.Receipt.Status != "complete" {
		t.Fatalf("manual preparation could not be finalized: %#v %v", report, err)
	}
}

func TestOldSealedBundleCannotBeRepairedThroughLaterReceipt(t *testing.T) {
	f, _, _ := toolsFixture(t)
	_, err := f.service.Setup(context.Background(), SetupRequest{ContextName: "example", SkipConfirmation: true})
	if err != nil {
		t.Fatal(err)
	}
	f.store.state.Receipt.CatalogDigest = strings.Repeat("f", 64)
	f.bundle.toolsReady = false
	writes := f.store.writes
	_, err = f.service.Setup(context.Background(), SetupRequest{ContextName: "example", SkipConfirmation: true})
	if code(err) != "controller.unknown" || f.store.writes != writes {
		t.Fatalf("old sealed bundle repair was authorized: %v", err)
	}
}

func TestLaterCompletedReceiptCannotQualifyUnsealedBundle(t *testing.T) {
	f, _, _ := toolsFixture(t)
	request := SetupRequest{ContextName: "example", SkipConfirmation: true}
	if _, err := f.service.Setup(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	// Another native/tool profile completed after this unsealed attempt. Its
	// shared receipt cannot supply this bundle's missing completion evidence.
	f.store.state.Receipt.CatalogDigest = strings.Repeat("f", 64)
	f.bundle.sealed = false
	report, err := f.service.Check(context.Background(), CheckRequest{ContextName: "example"})
	if code(err) != "preflight.failed" || report.Outcome != "not-ready" || len(report.Actions) == 0 {
		t.Fatalf("unsealed bundle claimed readiness: %#v %v", report, err)
	}
	writes := f.store.writes
	report, err = f.service.Setup(context.Background(), request)
	if err != nil || report.Outcome != "changed" || !f.bundle.sealed || f.store.writes <= writes {
		t.Fatalf("selected bundle completion was not established: %#v %v", report, err)
	}
}
