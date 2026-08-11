package workflow

import (
	"fmt"

	"github.com/crmarques/bootwright/internal/converge/remedy"
)

type LegacyConvergenceEvidenceError struct {
	ResourceID string
	Cause      error
}

type CurrentConvergenceEvidenceError struct {
	ResourceID string
	Cause      error
}

type UntrustedConvergenceEvidenceError struct {
	ResourceID string
	RecordPath string
	Cause      error
}

func (e *UntrustedConvergenceEvidenceError) Error() string {
	return fmt.Sprintf("cannot trust convergence safety evidence for %s at %s: %v; no apply mode or authorization adopts or overwrites evidence with an unverified API, target identity, manager, or context. Restore the exact record from a trusted backup, or remove only that file after independently proving it stale", e.ResourceID, e.RecordPath, e.Cause)
}

func (e *UntrustedConvergenceEvidenceError) Unwrap() error {
	return e.Cause
}

func (e *UntrustedConvergenceEvidenceError) Remedy() remedy.Request {
	return remedy.Request{Action: remedy.ActionRetrySameInvocation}
}

func (e *CurrentConvergenceEvidenceError) Error() string {
	return fmt.Sprintf("cannot verify current safety evidence payload for %s: %v; the record authority and selected target identity match, but the payload cannot prove prior convergence. Restore a valid payload or deliberately rebuild exactly the same resolved selection", e.ResourceID, e.Cause)
}

func (e *CurrentConvergenceEvidenceError) Unwrap() error {
	return e.Cause
}

func (e *CurrentConvergenceEvidenceError) Remedy() remedy.Request {
	return remedy.Request{Action: remedy.ActionRebuildSameSelection}
}

func (e *LegacyConvergenceEvidenceError) Error() string {
	return fmt.Sprintf("cannot verify legacy safety evidence for %s: %v; restore the immutable run evidence or deliberately rebuild exactly the same resolved selection", e.ResourceID, e.Cause)
}

func (e *LegacyConvergenceEvidenceError) Unwrap() error {
	return e.Cause
}

func (e *LegacyConvergenceEvidenceError) Remedy() remedy.Request {
	return remedy.Request{Action: remedy.ActionRebuildSameSelection}
}
