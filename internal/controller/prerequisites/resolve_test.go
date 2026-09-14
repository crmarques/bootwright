package prerequisites

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/diagnostics"
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

func (c *testTools) Present(ctx context.Context, area BundleArea, tools []ToolDefinition) (bool, error) {
	if len(tools) == 0 {
		return true, nil
	}
	if area == nil {
		return false, nil
	}
	entries, err := area.Entries(ctx)
	if err != nil {
		return false, err
	}
	for _, tool := range tools {
		for _, file := range tool.Files {
			if !slices.ContainsFunc(entries, func(entry BundleEntry) bool { return entry.Path == file.Path }) {
				return false, nil
			}
		}
	}
	return true, nil
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

// Setup prepares what every context on this host shares. A context that
// selects target clients adds nothing to it: no publisher is asked for their
// metadata, no source is retained for them, and the frozen closure stays the
// context-independent one. Their installation belongs to the controller stage.
func TestSetupIgnoresTheTargetToolsAContextSelects(t *testing.T) {
	f, installer, catalog := toolsFixture(t)
	catalog.fail = errors.New("tool publisher must not be contacted")
	report, err := f.service.Setup(context.Background(), SetupRequest{})
	if err != nil || report.Outcome != "changed" || installer.calls != 0 || catalog.resolves != 0 {
		t.Fatalf("report=%#v err=%v installs=%d resolves=%d", report, err, installer.calls, catalog.resolves)
	}
	if len(f.store.state.RetainedSources) != 6 {
		t.Fatalf("setup retained a source it does not own: %#v", f.store.state.RetainedSources)
	}
	for _, source := range f.store.state.RetainedSources {
		if strings.HasPrefix(source.ID, "tool-") {
			t.Fatal("setup retained a target tool source", source.ID)
		}
	}
	for _, check := range report.Checks {
		if check.Scope != HostScope {
			t.Fatalf("setup reported a %s check: %#v", check.Scope, check)
		}
	}
	writes := f.store.writes
	f.resolution.bootstrapError = errors.New("bootstrap publisher must not be contacted")
	f.resolution.nativeError = errors.New("repository must not be refreshed")
	report, err = f.service.Setup(context.Background(), SetupRequest{})
	if err != nil || report.Outcome != "unchanged" || installer.calls != 0 || f.store.writes != writes {
		t.Fatalf("no-op changed the frozen closure: %#v %v", report, err)
	}
}

// One retained resolution serves every context, because none of them can move
// the versions setup owns.
func TestRetainedResolutionServesAContextThatSelectsTools(t *testing.T) {
	f, installer, catalog := toolsFixture(t)
	if _, err := f.service.Setup(context.Background(), SetupRequest{}); err != nil {
		t.Fatal(err)
	}
	f.resolution.bootstrapError = errors.New("bootstrap publisher must not be contacted")
	f.resolution.nativeError = errors.New("repository must not be refreshed")
	catalog.fail = errors.New("tool publisher must not be contacted")
	report, err := f.service.Check(context.Background(), CheckRequest{ContextName: "example"})
	if code(err) != "preflight.failed" || report.Outcome != "not-ready" || installer.calls != 0 {
		t.Fatalf("a ready host did not serve the context from its retained resolution: %#v %v", report, err)
	}
	// The host itself is ready, so every unmet check belongs to the context and
	// the offered command is its own controller stage.
	if PendingScope(*report) != ContextScope {
		t.Fatalf("pending scope = %q: %#v", PendingScope(*report), report.Checks)
	}
	if !strings.Contains(diagnostics.Of(err)[0].Remediation, "apply --stage controller --context example") {
		t.Fatalf("remediation = %q", diagnostics.Of(err)[0].Remediation)
	}
}

func TestToolPlanningAndPreflightNeverResolveMetadata(t *testing.T) {
	f, installer, catalog := toolsFixture(t)
	_, err := f.service.Setup(context.Background(), SetupRequest{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	report, err := f.service.Check(context.Background(), CheckRequest{ContextName: "example"})
	if code(err) != "preflight.failed" || report.Outcome != "not-ready" || catalog.resolves != 0 || installer.calls != 0 || f.store.writes != 0 {
		t.Fatalf("inspection crossed effect boundary: %#v %v", report, err)
	}
	// Preflight reports the context's own prerequisites without planning them.
	scopes := map[string]string{}
	for _, check := range report.Checks {
		scopes[check.ID] = check.Scope
	}
	for id, want := range map[string]string{"execution-bundle": HostScope, "container-runtime": HostScope, "target-tools": ContextScope, "controller-binding": ContextScope} {
		if scopes[id] != want {
			t.Fatalf("check %s scope = %q, want %q", id, scopes[id], want)
		}
	}
	for _, action := range report.Actions {
		if strings.Contains(action, "client") || strings.Contains(action, "Bind") {
			t.Fatal("preflight planned a context prerequisite", action)
		}
	}
}

func TestDefiniteRefusalRetriesFromRetainedClosure(t *testing.T) {
	for _, missing := range []string{"none", "native"} {
		t.Run(missing, func(t *testing.T) {
			f, installer, _ := toolsFixture(t)
			f.host.runtime = RuntimeInspection{}
			installer.result = ActionResult{Outcome: "failed", Evidence: object(map[string]any{"installationEntered": false})}
			installer.err = failure("controller.unsupported", "native transaction refused before effects", "prepare the required foundation")
			_, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
			if err == nil || f.store.state.Receipt.Status != "failed" || f.bundle.sealed {
				t.Fatal("failure was not retained as an unsealed terminal attempt")
			}
			installer.err = nil
			installer.result = ActionResult{Outcome: "changed", Evidence: object(map[string]any{"nativePostcondition": "verified"})}
			// A fresh attempt installs the retained closure as frozen: no bootstrap
			// publisher is contacted. Only a missing native root solves the native
			// transaction again, because the frozen one is bound to a
			// before-inventory the host no longer has.
			f.resolution.bootstrapError = errors.New("bootstrap publisher must not be contacted")
			nativeCalls, installs := 2, 2
			if missing == "none" {
				f.host.runtime = RuntimeInspection{Present: true, Ready: true}
				f.resolution.nativeError = errors.New("repository must not be refreshed")
				nativeCalls, installs = 1, 1
			}
			report, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
			if err != nil || report.Outcome != "changed" || f.resolution.bootstrapCalls != 1 || f.resolution.nativeCalls != nativeCalls || installer.calls != installs || !f.bundle.sealed {
				t.Fatalf("retry from the retained closure failed: %#v %v bootstrap=%d native=%d installs=%d", report, err, f.resolution.bootstrapCalls, f.resolution.nativeCalls, installer.calls)
			}
		})
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
	_, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
	if err != nil {
		t.Fatal(err)
	}
	f.store.state.Receipt.CatalogDigest = strings.Repeat("f", 64)
	f.bundle.ready = false
	writes := f.store.writes
	_, err = f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
	if code(err) != "controller.unknown" || f.store.writes != writes {
		t.Fatalf("old sealed bundle repair was authorized: %v", err)
	}
}

func TestLaterCompletedReceiptCannotQualifyUnsealedBundle(t *testing.T) {
	f, _, _ := toolsFixture(t)
	request := SetupRequest{SkipConfirmation: true}
	if _, err := f.service.Setup(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	// Another native profile completed after this unsealed attempt. Its shared
	// receipt cannot supply this bundle's missing completion evidence.
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
