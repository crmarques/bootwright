package prerequisites

import (
	"context"
	"errors"
	"slices"
	"testing"
)

// cancelingStorage is the fixture's store, but its transactions refuse a
// canceled invocation's publication, as the controller store does.
type cancelingStorage struct{ *memoryStorage }

func (s cancelingStorage) MutateController(ctx context.Context, scope SetupContext, create bool, fn func(StorageTransaction) error) error {
	return s.memoryStorage.MutateController(ctx, scope, create, func(tx StorageTransaction) error {
		return fn(cancelingTransaction{tx.(*memoryStorage)})
	})
}

type cancelingTransaction struct{ *memoryStorage }

func (t cancelingTransaction) Publish(ctx context.Context, state HostState) (Publication, error) {
	if err := ctx.Err(); err != nil {
		return NotCommitted, err
	}
	return t.memoryStorage.Publish(ctx, state)
}

// cancelingRuntime cancels the invocation once its preparation is recorded,
// and returns what the runner returns for a run canceled there: failed, with
// its intent recorded, and the cancellation.
type cancelingRuntime struct {
	*testRuntimeInstaller
	cancel context.CancelFunc
}

func (r cancelingRuntime) Prepare(ctx context.Context, area BundleArea, platform Platform, definition Definition, route SetupEgress, record func(context.Context, NativePreparation) error, progress func(ProgressEvent), output RunOutput) (ActionResult, error) {
	return r.testRuntimeInstaller.Prepare(ctx, area, platform, definition, route, func(call context.Context, preparation NativePreparation) error {
		err := record(call, preparation)
		r.cancel()
		return err
	}, progress, output)
}

// Setup canceled after publishing its preparation but before its native
// record was acknowledged performed nothing, so its receipt records the
// action failed even though the invocation that met it is canceled.
func TestASetupCanceledBeforeNativeAuthorizationRecordsItsActionFailed(t *testing.T) {
	f, r := explicitRuntimeFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.failedAfterPreparation = true
	r.err = context.Canceled
	r.result = ActionResult{Outcome: "failed", Evidence: object(map[string]any{"intentRecorded": true, "postcondition": false})}
	service := New(cancelingStorage{&f.store}, &f.compiler, &f.host, &f.catalog, &f.bundle, cancelingRuntime{r, cancel}, f.service.options)
	if _, err := service.Setup(ctx, SetupRequest{SkipConfirmation: true}); !errors.Is(err, context.Canceled) {
		t.Fatalf("the canceled setup returned %v", err)
	}
	index := slices.IndexFunc(f.store.state.Receipt.Actions, func(a SetupAction) bool { return a.ID == "container-runtime" })
	if index < 0 {
		t.Fatal("the receipt holds no native runtime action")
	}
	action := f.store.state.Receipt.Actions[index]
	if action.Phase != "observed" || action.Outcome != "failed" || f.store.state.Receipt.Status != "failed" || r.entered != 1 {
		t.Fatalf("the native action is %s/%s, the receipt %q, entered %d, want observed failed in a failed receipt",
			action.Phase, action.Outcome, f.store.state.Receipt.Status, r.entered)
	}
}
