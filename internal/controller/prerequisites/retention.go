package prerequisites

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

const retentionCode = "controller.runtime-retention"

func (s Service) retain(ctx context.Context, tx SetupTransaction, current *inspection) {
	if err := s.retainRuntimes(ctx, tx, current); err != nil && ctx.Err() == nil {
		current.report.Warnings = append(current.report.Warnings, diagnostics.Diagnostic{Severity: "warning", Code: retentionCode,
			Message: "older controller runtimes and wheels could not all be removed (" + err.Error() + "); the next bootwright controller install retries"})
	}
}

func (s Service) retainRuntimes(ctx context.Context, tx SetupTransaction, current *inspection) error {
	runtimes := tx.View().State.Runtimes
	kept := []string{runtimes.Current, runtimes.Previous}
	published, err := tx.Runtimes(ctx)
	if err != nil {
		return err
	}
	remaining, held := []string{}, 0
	for _, digest := range published {
		if digest != "" && slices.Contains(kept, digest) {
			remaining = append(remaining, digest)
			continue
		}
		retired, err := tx.RetireRuntime(ctx, digest)
		if err != nil {
			return err
		}
		if !retired {
			remaining, held = append(remaining, digest), held+1
		}
	}
	if held != 0 {
		current.report.Warnings = append(current.report.Warnings, diagnostics.Diagnostic{Severity: "warning", Code: retentionCode,
			Message: fmt.Sprintf("kept: %d runtimes (%d in use)", len(remaining), held)})
	}
	keep := []string{}
	for _, file := range s.ports.Lock.Files {
		if file.ABI == "" || file.ABI == current.interpreter.ABI {
			keep = append(keep, file.SHA256)
		}
	}
	for _, digest := range remaining {
		wheels, err := s.ports.Python.RuntimeWheels(ctx, digest)
		if errors.Is(err, ErrRuntimeUnavailable) {
			continue
		}
		if err != nil {
			return err
		}
		keep = append(keep, wheels...)
	}
	slices.Sort(keep)
	_, err = tx.PruneWheels(ctx, slices.Compact(keep))
	return err
}
