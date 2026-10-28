package clusterpreflight

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/substrate/capacity"
)

type Part func(context.Context) (*Report, error)

var errSiblingStopped = errors.New("the other checks stopped")

func RunTogether(ctx context.Context, parts ...Part) ([]*Report, error) {
	reports := make([]*Report, len(parts))
	failures := make([]error, len(parts))
	checks, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	var group sync.WaitGroup
	for index, part := range parts {
		if part == nil {
			continue
		}
		group.Go(func() {
			reports[index], failures[index] = part(checks)
			if failures[index] != nil && reports[index] == nil {
				cancel(errSiblingStopped)
			}
		})
	}
	group.Wait()
	for index := range parts {
		if failures[index] != nil && reports[index] == nil && !(errors.Is(failures[index], context.Canceled) && context.Cause(checks) == errSiblingStopped && ctx.Err() == nil) {
			return nil, failures[index]
		}
	}
	return reports, nil
}

func Assemble(contextName, target, subject, command string, reports ...*Report) (*Report, error) {
	result := &Report{Context: contextName, Target: target, Checks: []capacity.Check{}}
	for _, part := range reports {
		if part != nil {
			result.Checks = MergeChecks(result.Checks, part.Checks)
		}
	}
	slices.SortFunc(result.Checks, func(a, b capacity.Check) int { return strings.Compare(a.Target+"/"+a.ID, b.Target+"/"+b.ID) })
	for _, check := range result.Checks {
		switch check.Status {
		case capacity.Passed:
			result.Summary.Passed++
		case capacity.Failed:
			result.Summary.Failed++
		case capacity.Pending:
			result.Summary.Pending++
		default:
			result.Summary.Unknown++
		}
	}
	if result.Summary.Failed+result.Summary.Unknown+result.Summary.Pending > 0 {
		code := "preflight.unknown"
		if result.Summary.Failed > 0 {
			code = "preflight.failed"
		}
		return result, diagnostics.NewFailureWithRemediation(code, subject+" readiness is not proved; inspect the failed, unknown and pending checks", "", "resolve the reported prerequisites and repeat preflight "+command)
	}
	return result, nil
}

func MergeChecks(merged, added []capacity.Check) []capacity.Check {
	for _, check := range added {
		index := slices.IndexFunc(merged, func(held capacity.Check) bool { return held.Target == check.Target && held.ID == check.ID })
		switch {
		case index < 0:
			merged = append(merged, check)
		case severity(check.Status) > severity(merged[index].Status):
			merged[index] = check
		}
	}
	return merged
}

func severity(status string) int {
	switch status {
	case capacity.Passed:
		return 0
	case capacity.Pending:
		return 1
	case capacity.Failed:
		return 3
	default:
		return 2
	}
}
