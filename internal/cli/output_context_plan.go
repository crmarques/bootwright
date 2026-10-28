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
	keyring := "removed with every secret version it holds"
	if plan.Lost {
		keyring = "already gone with its directory"
	}
	fields := []field{
		{Label: "Name", Value: plan.Context},
		{Label: "Input revisions", Value: revisions},
		{Label: "Keyring", Value: keyring},
		{Label: "Reservations", Value: reservations},
		{Label: "Owned objects", Value: abandonedObjects(plan)},
	}
	if plan.Abandons != contexts.AbandonsNone {
		freed := "none"
		if len(plan.Exclusive) != 0 {
			freed = strings.Join(plan.Exclusive, ", ")
		}
		areas := "none"
		if len(plan.WorkAreas) != 0 {
			areas = "left in place with the material their installations were given: " + contexts.DescribeWorkAreas(plan.WorkAreas)
		}
		fields = append(fields,
			field{Label: "Work areas", Value: areas},
			field{Label: "Freed keys", Value: freed + "; another context may then claim a resource an abandoned object still uses, such as the management controller of a powered-off Machine"},
			field{Label: "Same name", Value: "remove the abandoned objects by hand before bootwright context init --name " + plan.Context +
				" again, because that name reads them as its own, and never reuse " + plan.Context + " on another controller that shares a provider host"},
		)
	}
	var text display
	text.headline("", "Context deletion plan")
	text.section("")
	text.fields(fields...)
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
		return "abandoned unmanaged; bootwright status --context " + plan.Context + " and bootwright diff --context " + plan.Context +
			" list them, diff also what the selected input no longer declares"
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
