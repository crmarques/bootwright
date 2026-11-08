package capacity

import (
	"context"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
)

func PendingWrites(ctx context.Context, writes WriteProjector, catalog api.Catalog, contextName string, allocations []Allocation, owned map[string]machine.OwnershipState) ([]Write, error) {
	if writes == nil {
		return nil, nil
	}
	return writes(ctx, catalog, contextName, allocations, func(kind, object string) bool {
		state, found := owned[kind+"/"+object]
		return !found || state.Verb != machine.VerbApply || state.State != machine.BlockDone
	})
}

func Admit(ctx context.Context, checker CapacityChecker, allocations []Allocation, writes []Write) error {
	checks, err := checker.Check(ctx, allocations, writes, false)
	if err != nil {
		return err
	}
	var refused []diagnostics.Diagnostic
	for _, check := range checks {
		if check.Status == Passed {
			continue
		}
		code := "preflight.unknown"
		if check.Status == Failed {
			code = "preflight.failed"
		}
		refused = append(refused, diagnostics.Diagnostic{Severity: "error", Code: code, Message: check.Target + " " + check.ID + ": " + check.Reason, Remediation: check.Next})
	}
	if len(refused) > 0 {
		return &diagnostics.Failure{Diagnostics: refused}
	}
	return nil
}
