package cli

import (
	"context"
	"io"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// ContextPlanPresenter writes what a context update or deletion changes before
// its confirmation: the plan on standard output and an update's warnings on
// standard error, ahead of the prompt there.
type ContextPlanPresenter struct{ out, errOut io.Writer }

func NewContextPlanPresenter(out, errOut io.Writer) *ContextPlanPresenter {
	return &ContextPlanPresenter{out: out, errOut: errOut}
}

func (p *ContextPlanPresenter) PresentUpdate(ctx context.Context, plan contexts.UpdatePlan) error {
	var text display
	text.headline("", "Context update plan")
	text.section("")
	text.fields(
		field{Label: "Name", Value: plan.Context},
		field{Label: "Input directory", Value: plan.InputDirectory},
		field{Label: "Files to copy", Value: strconv.Itoa(plan.FilesCopied)},
		field{Label: "Files seen", Value: strconv.Itoa(plan.Counts.FilesSeen)},
		field{Label: "Objects decoded", Value: strconv.Itoa(plan.Counts.ObjectsDecoded)},
	)
	if err := p.write(ctx, text); err != nil {
		return err
	}
	if err := writeHumanDiagnostics(p.errOut, displayDiagnostics(plan.Diagnostics)); err != nil {
		return &contextPlanOutputFailure{}
	}
	return ctx.Err()
}

func (p *ContextPlanPresenter) PresentDeletion(ctx context.Context, plan contexts.DeletionPlan) error {
	revisions := "none"
	if plan.Revision != "" {
		revisions = "selected " + plan.Revision
	}
	reservations := "none"
	if len(plan.Reservations) != 0 {
		reservations = strings.Join(plan.Reservations, ", ")
	}
	var text display
	text.headline("", "Context deletion plan")
	text.section("")
	text.fields(
		field{Label: "Name", Value: plan.Context},
		field{Label: "Input revisions", Value: revisions},
		field{Label: "Keyring", Value: "removed with every secret version it holds"},
		field{Label: "Reservations", Value: reservations},
		field{Label: "Owned objects", Value: abandonedObjects(plan)},
	)
	if err := p.write(ctx, text); err != nil {
		return err
	}
	return ctx.Err()
}

// abandonedObjects names what a deletion abandons: nothing, the objects status
// lists, or objects that cannot be listed and why.
func abandonedObjects(plan contexts.DeletionPlan) string {
	switch plan.Abandons {
	case contexts.AbandonsOwned:
		return "abandoned unmanaged; bootwright status --context " + plan.Context + " lists them"
	case contexts.AbandonsUnlisted:
		return "cannot be listed: " + plan.Reason
	}
	return "none"
}

// write refuses a plan that did not reach the operator, so no prompt follows
// it.
func (p *ContextPlanPresenter) write(ctx context.Context, text display) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p == nil || p.out == nil || p.errOut == nil {
		return &contextPlanOutputFailure{}
	}
	if err := text.writeTo(p.out); err != nil {
		return &contextPlanOutputFailure{}
	}
	return nil
}

type contextPlanOutputFailure struct{}

func (*contextPlanOutputFailure) Error() string { return "context plan output failed" }
