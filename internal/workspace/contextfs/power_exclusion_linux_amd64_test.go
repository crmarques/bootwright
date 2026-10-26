//go:build linux && amd64

package contextfs

import (
	"context"
	"os"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func TestMachinePowerAndLifecycleNetworkRestartExcludeOneAnother(t *testing.T) {
	for _, powerFirst := range []bool{false, true} {
		name := "network-restart-holds-the-context"
		if powerFirst {
			name = "machine-power-holds-the-context"
		}
		t.Run(name, func(t *testing.T) {
			store, _ := lifecycleFixture(t)
			other := New(Options{Root: store.options.Root, Owner: &Ownership{UID: uint32(os.Geteuid()), GID: uint32(os.Getegid())}})
			ctx := context.Background()
			entered := false
			compete := func() error {
				var err error
				if powerFirst {
					err = other.MutateLifecycle(ctx, "example", func(lifecycle.Transaction) error { entered = true; return nil })
				} else {
					err = other.RunLifecycle(ctx, "example", func(lifecycle.RunView) error { entered = true; return nil })
				}
				reported := diagnostics.Of(err)
				if entered || len(reported) != 1 || reported[0].Code != "lifecycle.lease" {
					t.Fatalf("a competing host effect entered=%t, diagnostics=%+v, error=%v", entered, reported, err)
				}
				return nil
			}
			var err error
			if powerFirst {
				err = store.RunLifecycle(ctx, "example", func(lifecycle.RunView) error { return compete() })
			} else {
				err = store.MutateLifecycle(ctx, "example", func(lifecycle.Transaction) error { return compete() })
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := other.RunLifecycle(ctx, "example", func(lifecycle.RunView) error { entered = true; return nil }); err != nil || !entered {
				t.Fatalf("power stayed excluded after the hold ended: %v", err)
			}
		})
	}
}
