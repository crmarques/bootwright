package lifecycle

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

type invocationTestKey struct{}

type scopedCapability struct {
	*testCapability
	t     *testing.T
	watch *overlap
	seen  chan chan struct{}
}

func (c scopedCapability) enter(ctx context.Context, block string) func() {
	scope, ok := ctx.Value(invocationTestKey{}).(chan struct{})
	if !ok {
		c.t.Error("adapter received no execution scope")
	}
	c.seen <- scope
	c.watch.enter(block)
	return func() {
		select {
		case <-scope:
			c.t.Error("execution scope closed before its adapter settled")
		default:
		}
		c.watch.leave(block)
	}
}

func (c scopedCapability) Apply(ctx context.Context, execution Execution) (Result, error) {
	defer c.enter(ctx, execution.Block.ID)()
	return c.testCapability.Apply(ctx, execution)
}

func (c scopedCapability) Destroy(ctx context.Context, execution Execution) (Result, error) {
	defer c.enter(ctx, execution.Block.ID)()
	return c.testCapability.Destroy(ctx, execution)
}

func TestInvocationScopeClosesOnlyAfterEveryCanceledAdapterSettles(t *testing.T) {
	for _, verb := range []reconciliation.Verb{reconciliation.Apply, reconciliation.Destroy} {
		t.Run(string(verb), func(t *testing.T) {
			h := scheduled(t, 2, definition("alpha"), definition("bravo"))
			if verb == reconciliation.Destroy {
				if err := applyOnce(h); err != nil {
					t.Fatal(err)
				}
			}
			watch := newOverlap(2)
			defer watch.open()
			seen := make(chan chan struct{}, 2)
			h.service.capabilities = testResolver{capability: scopedCapability{h.capability, t, watch, seen}}
			var opened, closed atomic.Int32
			h.service.options.BeginInvocation = func(ctx context.Context) (context.Context, func()) {
				opened.Add(1)
				scope := make(chan struct{})
				return context.WithValue(ctx, invocationTestKey{}, scope), func() {
					closed.Add(1)
					close(scope)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				var err error
				if verb == reconciliation.Apply {
					_, err = h.service.Apply(ctx, ApplyRequest{ContextName: "lab", SkipConfirmation: true})
				} else {
					_, err = h.service.Destroy(ctx, DestroyRequest{ContextName: "lab", SkipConfirmation: true})
				}
				done <- err
			}()
			watch.gather(t, 2)
			first, second := <-seen, <-seen
			if first == nil || first != second || opened.Load() != 1 {
				t.Fatal("siblings did not share exactly one execution scope")
			}
			cancel()
			if closed.Load() != 0 {
				t.Fatal("cancellation closed the scope with adapters still in flight")
			}
			watch.open()
			<-done
			if closed.Load() != 1 {
				t.Fatal("settled operation did not close its scope exactly once")
			}
		})
	}
}
