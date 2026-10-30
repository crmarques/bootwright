//go:build linux && amd64

package contextfs

import (
	"context"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// An apply or a controller stage that needs the setup this host lacks names
// setup and then the command itself as its remedy, never setup's exact retry
// and never inside its message; setup's own refusals keep that retry.
func TestControllerRefusalsNameTheirOwnRemedies(t *testing.T) {
	ctx := context.Background()
	store, record := lifecycleFixture(t)
	host := syntheticControllerState(t).Host
	for name, call := range map[string]func(lifecycle.Transaction) error{
		"binding":               func(tx lifecycle.Transaction) error { return tx.Bind(ctx, "controller", host) },
		"retained dependencies": func(tx lifecycle.Transaction) error { return tx.RetainDependencies(ctx, nil) },
		"client area": func(tx lifecycle.Transaction) error {
			_, err := tx.ClientArea(ctx, clientClosure)
			return err
		},
		"host reservation": func(tx lifecycle.Transaction) error {
			return tx.Reserve(ctx, []prerequisites.HostReservation{{Context: "example", Kind: "listener", Service: "lab-dns", Keys: []string{"udp/53"}}})
		},
	} {
		err := store.MutateLifecycle(ctx, "example", call)
		reported := diagnostics.Of(err)
		message := "this host has no completed controller setup"
		if name == "host reservation" {
			message = "locally hosted services require a completed controller setup on this host"
		}
		if len(reported) != 1 || reported[0].Code != "controller.identity" || reported[0].Message != message ||
			reported[0].Remediation != "run bootwright controller install, then repeat the command" || reported[0].Source != nil {
			t.Errorf("%s: refusal = %+v", name, reported)
		}
	}
	sealedBundleFixture(t, store, record)
	other, err := controller.NewInstalledHostIdentity(controller.LinuxInstalledIdentityV1, strings.Repeat("9", 32), "99999999-2222-4333-8444-555555555555", "99999999-3333-4444-8555-666666666666")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error { return tx.Bind(ctx, "controller", host) }); err != nil {
		t.Fatalf("the first binding failed: %+v", diagnostics.Of(err))
	}
	for _, test := range []struct {
		name, machine, remedy string
		host                  controller.InstalledHostIdentity
	}{
		{"another host", "controller", "run on the host this controller state belongs to, or restore that host's machine-id, DMI product UUID and root filesystem UUID on this one; if this host keeps the same state store under the changed values, rebind its record with bootwright controller rebind; otherwise bootwright context delete --name example --purge --allow-orphans, which abandons every object the context realized", other},
		{"another Machine", "replacement", "restore the bound controller Machine input, or create a context on this host", host},
	} {
		err := store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error { return tx.Bind(ctx, test.machine, test.host) })
		reported := diagnostics.Of(err)
		if len(reported) != 1 || reported[0].Code != "controller.identity" || reported[0].Remediation != test.remedy || strings.Contains(reported[0].Message, ";") {
			t.Errorf("%s: refusal = %+v", test.name, reported)
		}
	}
}

// A tool area no attribution names is settled by setup, which every command
// reading it is sent to.
func TestAToolAreaSetupMustSettleNamesSetup(t *testing.T) {
	stored := controllerStored{bundles: []controllerBundleReservation{{ID: clientClosure, Mode: "sealed"}}}
	_, err := openControllerBundle(context.Background(), nil, nil, contexts.Registry{}, stored, clientClosure, func() bool { return true }, false, nil)
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Message != "required controller bundle is not attributable" || reported[0].Remediation != "run bootwright controller install" {
		t.Errorf("refusal = %+v", reported)
	}
}
