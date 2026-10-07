package prerequisites

import (
	"context"
	"encoding/hex"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

// A foundation that qualifies differently after the operator confirmed the
// plan, as when a second errata lands before setup re-inspects, refuses as a
// changed state: setup never records a foundation the operator did not see.
func TestAFoundationRequalifiedAfterConfirmationRefuses(t *testing.T) {
	f, foundation, runtime, _ := errataFixture(t)
	foundation.qualified = errataQualified()
	before, writes, prepares := f.store.state.Receipt, f.store.writes, f.bundle.prepares
	fired := false
	f.beforeMutation = func() {
		f.beforeMutation, fired = nil, true
		later := errataQualified()
		later.Execution.Files[1].SHA256 = hex.EncodeToString(slices.Repeat([]byte{2}, 32))
		later.Packages[1].Build = "11.5.0-16.el9"
		foundation.qualified = later
	}
	_, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
	found := diagnostics.Of(err)
	if !fired || len(found) != 1 || found[0].Code != "controller.conflict" || found[0].Message != "controller state changed after plan confirmation" {
		t.Fatalf("a foundation requalified after confirmation: fired=%v %v", fired, err)
	}
	if f.store.state.Receipt.ID != before.ID || f.store.state.Receipt.Foundation != nil || f.store.writes != writes || f.bundle.prepares != prepares || runtime.calls != 0 {
		t.Fatalf("the refused setup changed state: receipt %+v, %d writes, %d prepares, %d native effects", f.store.state.Receipt, f.store.writes-writes, f.bundle.prepares-prepares, runtime.calls)
	}
}
