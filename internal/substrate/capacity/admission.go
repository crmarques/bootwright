package capacity

import (
	"context"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

func Admit(ctx context.Context, checker CapacityChecker, allocations []Allocation) error {
	checks, err := checker.Check(ctx, allocations, false)
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
