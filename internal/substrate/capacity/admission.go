package capacity

import (
	"context"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func PendingWrites(ctx context.Context, writes WriteProjector, ownership Ownership, catalog api.Catalog, contextName string, allocations []Allocation) ([]Write, error) {
	if writes == nil {
		return nil, nil
	}
	return writes(ctx, catalog, contextName, allocations, func(kind, implementation, object string) (bool, error) {
		if ownership == nil {
			return true, nil
		}
		done, err := ownership.Completed(ctx, contextName, kind, implementation, object)
		return !done, err
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
