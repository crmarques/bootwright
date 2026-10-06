package prerequisites

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// Setup prepares only what it resolved, so a composition lacking any of its
// resolution ports offers no setup and no preflight: both fail closed before
// any stored evidence is read.
func TestSetupRequiresItsResolutionPorts(t *testing.T) {
	for name, drop := range map[string]func(*Options){
		"bootstrap":        func(options *Options) { options.Bootstrap = nil },
		"native":           func(options *Options) { options.Native = nil },
		"native inspector": func(options *Options) { options.NativeInspector = nil },
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			options := f.service.options
			drop(&options)
			service := New(&f.store, &f.compiler, &f.host, &f.catalog, &f.bundle, nil, options)
			for _, request := range []SetupRequest{{}, {DryRun: true}, {SkipConfirmation: true}} {
				if report, err := service.Setup(context.Background(), request); !errors.Is(err, availability.ErrNotImplemented) || report != nil {
					t.Fatalf("setup %+v without its %s port: %#v %v", request, name, report, err)
				}
			}
			for _, request := range []CheckRequest{{}, {ContextName: "example"}} {
				if report, err := service.Check(context.Background(), request); !errors.Is(err, availability.ErrNotImplemented) || report != nil {
					t.Fatalf("preflight %+v without its %s port: %#v %v", request, name, report, err)
				}
			}
			if f.store.reads != 0 || f.store.writes != 0 || len(f.events) != 0 {
				t.Fatalf("an unavailable setup reached the host: reads=%d writes=%d events=%v", f.store.reads, f.store.writes, f.events)
			}
		})
	}
}

// Bindings are published by a context's first apply, never by setup: a setup
// over a host that already holds them plans and records only its own
// prerequisites and leaves them as they were. Even a context-bound inspection
// plans no binding, and a pending receipt that carries one is not setup's.
func TestSetupNeverPlansAControllerBinding(t *testing.T) {
	f := newFixture(t)
	f.store.scope = SetupContext{Name: "example", Revision: "rev-" + strings.Repeat("2", 32)}
	digest, err := f.host.identity.PrivateDigest()
	if err != nil {
		t.Fatal(err)
	}
	bound := []ControllerBinding{{Context: "example", Machine: "controller", HostDigest: digest}}
	f.store.state.Bindings = slices.Clone(bound)
	report, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
	if err != nil || report.Outcome != "changed" {
		t.Fatalf("setup over a bound host: %#v %v", report, err)
	}
	ids := func(actions []SetupAction) []string {
		var found []string
		for _, action := range actions {
			found = append(found, action.ID)
		}
		return found
	}
	if got := ids(f.store.state.Receipt.Actions); !slices.Equal(got, []string{"execution-bundle", "container-runtime"}) {
		t.Fatalf("setup recorded actions %v", got)
	}
	if !slices.Equal(f.store.state.Bindings, bound) {
		t.Fatalf("setup changed the bindings to %+v", f.store.state.Bindings)
	}
	err = f.store.ReadController(context.Background(), "example", func(view StorageView) error {
		current, err := f.service.inspect(context.Background(), view, false, "")
		if err != nil {
			return err
		}
		receipt, err := newReceipt(current)
		if err != nil {
			return err
		}
		if got := ids(receipt.Actions); slices.Contains(got, "controller-binding") {
			t.Fatalf("a context-bound inspection planned %v", got)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	pending := newFixture(t)
	pending.bundle.err = errors.New("failed")
	if _, err := pending.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true}); err == nil || !pending.store.state.Receipt.Incomplete() {
		t.Fatalf("setup left no pending receipt: %v", err)
	}
	pending.bundle.err = nil
	pending.store.state.Receipt.Actions = append(pending.store.state.Receipt.Actions, SetupAction{ID: "controller-binding", Request: object(map[string]any{"boundBefore": false, "machine": "controller"}), Phase: "planned", Evidence: object(map[string]any{})})
	writes := pending.store.writes
	_, err = pending.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
	want := "restore the executable that recorded it and the HTTPS_PROXY, HTTP_PROXY and NO_PROXY values it ran with, then run bootwright setup"
	if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Code != "controller.unknown" || reported[0].Remediation != want || pending.store.writes != writes {
		t.Fatalf("a pending receipt with a binding action was resumed: %+v", reported)
	}
}
