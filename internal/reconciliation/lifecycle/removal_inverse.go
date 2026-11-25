package lifecycle

import (
	"context"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

// RemovalOf plans an apply's whole removal beside the leaves-with its phases compute.
func RemovalOf(ctx context.Context, capabilities CapabilityResolver, applied reconciliation.Plan, relaxations map[string]string) (reconciliation.Plan, map[string][]string, error) {
	planner := Service{capabilities: capabilities}
	removal, err := planner.removalOf(ctx, applied, operationstore.Executable{}, removalInputs{relaxations: relaxations})
	if err != nil {
		return reconciliation.Plan{}, nil, err
	}
	return removal, planner.computePhases(removal).leavesWith, nil
}

func inverseOf(retains []string, nothing string, leavesWith, keptHosts []string) *reconciliation.Inverse {
	inverse := &reconciliation.Inverse{Retains: slices.Clone(retains), Nothing: nothing, LeavesWith: slices.Clone(leavesWith), KeptHosts: slices.Clone(keptHosts)}
	if !inverse.Stated() {
		return nil
	}
	return inverse
}

func (s Service) freezeInverse(plan reconciliation.Plan) (reconciliation.Plan, error) {
	phases := s.computePhases(plan)
	definitions := make([]reconciliation.BlockDefinition, 0, len(plan.Blocks))
	var unstated, contradicted []string
	for _, block := range plan.Blocks {
		definition := block.BlockDefinition
		var retains, keptHosts []string
		nothing := ""
		if definition.Inverse != nil {
			retains, nothing, keptHosts = definition.Inverse.Retains, definition.Inverse.Nothing, definition.Inverse.KeptHosts
		}
		definition.Inverse = inverseOf(retains, nothing, phases.leavesWith[block.ID], keptHosts)
		if len(definition.Impacts) == 0 && definition.Inverse == nil {
			unstated = append(unstated, block.ID)
		}
		if nothing != "" && len(phases.leavesWith[block.ID]) != 0 {
			contradicted = append(contradicted, block.ID)
		}
		definitions = append(definitions, definition)
	}
	if len(unstated) != 0 {
		return reconciliation.Plan{}, destroyInverseUnstated.Fail(
			"these removal steps state no effect, nothing they keep, no step they leave with and no reason they have nothing to take back: "+strings.Join(unstated, ", "),
			"report this message as a defect of the build that planned the removal")
	}
	if len(contradicted) != 0 {
		return reconciliation.Plan{}, destroyInverseUnstated.Fail(
			"these removal steps state they have nothing to take back while their effect leaves with the steps that remove their hosts: "+strings.Join(contradicted, ", "),
			"report this message as a defect of the build that planned the removal")
	}
	return reconciliation.NewPlan(reconciliation.Destroy, definitions)
}

func proveInverse(plan reconciliation.Plan, phases removalPhases, operation operationstore.Operation) (removalPhases, error) {
	for _, block := range plan.Blocks {
		var frozen []string
		if block.Inverse != nil {
			frozen = block.Inverse.LeavesWith
		}
		computed := phases.leavesWith[block.ID]
		if slices.Equal(frozen, computed) {
			continue
		}
		remedy := "continue or replace the removal with the build that registered it"
		if operation.ID != "" {
			remedy = "continue or replace the removal " + operation.ID + " with " + registeredWith(operation.Executable)
		}
		return phases, destroyInverseMoved.Fail(
			"the removal plan froze that the step "+block.ID+" leaves with "+leavesWithList(frozen)+", and this executable computes "+leavesWithList(computed),
			remedy)
	}
	phases.retired = frozenRetired(plan)
	return phases, nil
}

func frozenRetired(plan reconciliation.Plan) map[string]bool {
	retired := map[string]bool{}
	for _, block := range plan.Blocks {
		if block.Inverse != nil && len(block.Inverse.LeavesWith) != 0 {
			retired[block.ID] = true
		}
	}
	return retired
}

func leavesWithList(ids []string) string {
	if len(ids) == 0 {
		return "no step"
	}
	return strings.Join(ids, ", ")
}
