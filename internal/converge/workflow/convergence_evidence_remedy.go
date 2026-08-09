package workflow

import (
	"fmt"

	"github.com/crmarques/bootwright/internal/converge/remedy"
)

type LegacyConvergenceEvidenceError struct {
	ResourceID string
	Cause      error
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
