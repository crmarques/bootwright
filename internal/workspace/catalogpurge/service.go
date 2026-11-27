package catalogpurge

import (
	"context"
	"fmt"

	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

type Service struct {
	Store      Store
	References References
	Confirmer  Confirmer
	Present    func(context.Context, Plan) error
}

func (s Service) Purge(ctx context.Context, request Request) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if s.Store == nil || s.References == nil {
		return Result{}, availability.ErrNotImplemented
	}
	if !request.Kind.Valid() {
		return Result{}, Failure(request.Kind, "only artifact and media catalogs can be purged", "select catalog artifact or catalog media")
	}
	plan, err := s.Store.PlanCatalogPurge(ctx, request.Kind, request.Force, s.References)
	if err != nil {
		return Result{}, err
	}
	result := Result{Plan: plan, DryRun: request.DryRun}
	count := selectedCount(plan)
	if request.DryRun || count == 0 {
		return result, nil
	}
	if !request.Yes {
		if s.Present == nil || s.Confirmer == nil {
			return Result{}, Failure(request.Kind, "catalog purge requires confirmation", "inspect the purge with --dry-run, then repeat with --yes")
		}
		if err := s.Present(ctx, plan); err != nil {
			return Result{}, err
		}
		scope := fmt.Sprintf("%d entries", count)
		if count == 1 {
			scope = "1 entry"
		}
		if err := s.Confirmer.Confirm(ctx, "purge catalog "+string(request.Kind), scope); err != nil {
			return Result{}, err
		}
	}
	removed, err := s.Store.CommitCatalogPurge(ctx, plan, s.References)
	if err != nil && removed > 0 {
		summary := Failure(request.Kind, fmt.Sprintf("the purge stopped after removing %d entries", removed),
			"review what remains with --dry-run before retrying")
		err = &diagnostics.Failure{Diagnostics: append(diagnostics.Of(err), diagnostics.Of(summary)...)}
	}
	return result, err
}

func selectedCount(plan Plan) int {
	count := 0
	for _, entry := range plan.Entries {
		if entry.Selected(plan.Force) {
			count++
		}
	}
	return count
}

func Failure(kind Kind, reason, remedy string) error {
	code := "artifact.catalog"
	if kind == Media {
		code = "media.store"
	}
	return diagnostics.NewFailureWithRemediation(code, reason, "", remedy)
}
