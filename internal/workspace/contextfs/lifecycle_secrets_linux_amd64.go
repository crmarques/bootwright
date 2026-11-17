//go:build linux && amd64

package contextfs

import (
	"context"

	"github.com/crmarques/bootwright/internal/secrets/secretstore"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// Secrets lends the context's secret area under the root lock and the lease
// this transaction already holds, with the checks MutateSecrets makes before it
// lends its own. The lease already collected the context's stages, so none are
// collected here. Blocks of one operation share the transaction, so callers are
// served one at a time, each with a fresh area that closes when its callback
// returns.
func (t *lifecycleTransaction) Secrets(ctx context.Context, callback func(secretstore.Context, secretstore.Area) error) error {
	if callback == nil {
		return state("lifecycle secret callback is missing")
	}
	t.lending.Lock()
	defer t.lending.Unlock()
	if t.active == nil || !t.active() || t.base == nil {
		return state("lifecycle secret capability has closed")
	}
	if err := t.base.available(ctx); err != nil {
		return err
	}
	record, err := t.base.record(t.identity.Name)
	if err != nil {
		return err
	}
	if record.Mode != contexts.Ready || record != t.record {
		return secretstore.Failure("store.conflict", "context identity changed before secret access")
	}
	if err := t.context.Verify(); err != nil {
		return state("context directory changed during the operation")
	}
	if err := verifySecretContextLayout(ctx, t.context); err != nil {
		return safeError(err)
	}
	selected := secretContext(record)
	area := &secretArea{store: t.base.store, root: t.base.root, context: t.context, expected: t.base.expected, token: selected, active: true, mutable: make(map[string]secretExpectation)}
	return lendSecretArea(ctx, area, t.context, func() error { return callback(selected, area) })
}
