package installation

import (
	"context"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/machine/diskselection"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate"
)

type DiscoveryRefinement func(api.Object, api.Object, reconciliation.Plan) bool

type DiscoverRequest struct {
	ContextName          string
	Names                []string
	PreparePrerequisites bool
	SkipConfirmation     bool
}

func (c DiscoveryCapability) AutomaticPreparation(ctx context.Context, input lifecycle.PlanInput, selection reconciliation.Selection) ([]string, error) {
	requests, _, err := c.requests(ctx, input)
	if err != nil {
		return nil, err
	}
	var pending, targets []string
	for _, r := range requests {
		if input.AppliedEvidence == nil {
			return nil, refusal("lifecycle.state", "automatic preparation has no ownership reader", "")
		}
		owned, err := input.AppliedEvidence(ctx, Kind, r.Identity.Object)
		if err != nil {
			return nil, err
		}
		if slices.ContainsFunc(owned, func(record lifecycle.BlockEvidence) bool {
			return record.Implementation == Implementation || record.Implementation == EraseImplementation
		}) {
			continue
		}
		targets = append(targets, r.Identity.Object)
		_, found, err := c.completedInventory(ctx, input, r)
		if err != nil {
			return nil, err
		}
		if !found {
			pending = append(pending, r.Identity.Object)
		}
	}
	if len(pending) == 0 && !selection.Stages.Selects(reconciliation.StageMachines) {
		return targets, nil
	}
	return pending, nil
}

func (c DiscoveryCapability) ValidatePreparation(ctx context.Context, input lifecycle.PlanInput, scope reconciliation.PreparationScope) error {
	if input.State == nil || input.AppliedEvidence == nil {
		return refusal("lifecycle.state", "disk discovery needs compiled state and durable ownership", "")
	}
	for _, name := range scope.Targets {
		m, found := input.State.Effective().Find(api.Machine, name)
		if !found || !discoveryMachine(m) || name == input.Controller {
			return refusal("api.invariant", "discovery selects only non-controller physical Machines declaring rootDevice.discover", "select a powered-off physical RHEL installation target")
		}
		owned, err := input.AppliedEvidence(ctx, Kind, name)
		if err != nil {
			return err
		}
		for _, record := range owned {
			if record.Implementation == Implementation || record.Implementation == EraseImplementation {
				return refusal("lifecycle.state", "Machine/"+name+" already has installation ownership and cannot be booted for discovery", "retain the existing installation or remove it through its recorded lifecycle first")
			}
		}
	}
	if scope.PreparePrerequisites {
		return nil
	}
	requests, requirements, err := c.requests(ctx, input)
	if err != nil {
		return err
	}
	for index, r := range requests {
		for _, ref := range requires(requirements[index]) {
			if ref.Kind == Kind {
				continue
			}
			owned, err := input.AppliedEvidence(ctx, ref.Kind, ref.Object)
			if err != nil {
				return err
			}
			if !slices.ContainsFunc(owned, func(record lifecycle.BlockEvidence) bool {
				return record.Verb == reconciliation.Apply && record.State == reconciliation.BlockDone
			}) {
				return refusal("lifecycle.state", "discovery prerequisites are not applied for Machine/"+r.Identity.Object, "repeat bootwright machine discover with --prepare-prerequisites to review and confirm controller and infrastructure setup")
			}
		}
	}
	return nil
}

func (c DiscoveryCapability) AdvancePreparation(ctx context.Context, input lifecycle.PreparationAdvance) error {
	if input.Before != nil {
		if err := c.refinement(input.Before.Effective(), input.State.Effective(), input.Owned, c.refinementInventory(ctx, input)); err != nil {
			return err
		}
	}
	for _, block := range input.Fresh.Blocks {
		if block.Implementation != Implementation {
			continue
		}
		r, err := DecodeRequest(block.Request)
		if err != nil {
			return err
		}
		m, found := input.State.Effective().Find(api.Machine, r.Identity.Object)
		if found && discoveryMachine(m) && r.DiscoveryProof == nil {
			return refusal("lifecycle.state", "installation addition has no frozen discovery proof", "")
		}
	}
	return ctx.Err()
}

func (c DiscoveryCapability) refinement(before, after api.Catalog, owned reconciliation.Plan, inventory func(string) ([]diskselection.Disk, error)) error {
	refused := func() error {
		return refusal("lifecycle.state", "only root-discovery policy and unowned storage-device refinements may follow preparation", "keep prepared ownership unchanged and update only the disk-selection fields")
	}
	if len(before.Objects()) != len(after.Objects()) {
		return refused()
	}
	installationOwned := slices.ContainsFunc(owned.Blocks, func(block reconciliation.Block) bool {
		m, found := before.Find(api.Machine, block.Object)
		return found && discoveryMachine(m) && (block.Implementation == Implementation || block.Implementation == EraseImplementation)
	})
	for _, current := range after.Objects() {
		old, found := before.Find(current.Kind(), current.Name())
		if !found {
			return refused()
		}
		if current.Value().Equal(old.Value()) {
			continue
		}
		if installationOwned {
			return refused()
		}
		allowed := false
		if current.Kind() == api.Machine && discoveryMachine(current) && discoveryMachine(old) {
			left := substrate.WithMachineDiscoveryPolicy(old, api.MapValue())
			right := substrate.WithMachineDiscoveryPolicy(current, api.MapValue())
			allowed = left.Value().Equal(right.Value())
			if !allowed && inventory != nil {
				oldClaims, oldErr := c.protectedClaims(before, old)
				newClaims, newErr := c.protectedClaims(after, current)
				facts, err := inventory(current.Name())
				if oldErr != nil || newErr != nil || err != nil || len(facts) == 0 {
					return refused()
				}
				allowed = machine.RefinesUnownedDataTargets(left, right, oldClaims, newClaims, facts)
			}
		}
		for _, owner := range c.refinements {
			allowed = allowed || owner(old, current, owned)
		}
		if !allowed {
			return refused()
		}
	}
	return nil
}

func (c DiscoveryCapability) protectedClaims(catalog api.Catalog, target api.Object) ([]diskselection.DiskIdentity, error) {
	var claims []diskselection.DiskIdentity
	for _, owner := range c.protections {
		found, err := owner(catalog, target)
		if err != nil {
			return nil, err
		}
		claims = append(claims, found...)
	}
	return claims, nil
}

func (c DiscoveryCapability) refinementInventory(ctx context.Context, input lifecycle.PreparationAdvance) func(string) ([]diskselection.Disk, error) {
	return func(name string) ([]diskselection.Disk, error) {
		controller, err := lifecycle.ControllerMachine(input.State.Effective())
		if err != nil || input.AppliedEvidence == nil {
			return nil, refusal("lifecycle.state", "disk refinement has no original inventory reader", "")
		}
		if _, err := c.Roots(ctx, lifecycle.PlanInput{Context: input.Context, State: input.State, Controller: controller, AppliedEvidence: input.AppliedEvidence}); err != nil {
			return nil, err
		}
		records, err := input.AppliedEvidence(ctx, Kind, name)
		if err != nil {
			return nil, err
		}
		inventory, found, err := RecordedInventory(name, records)
		if err != nil || !found {
			return nil, refusal("lifecycle.state", "disk refinement has no matching original inventory", "")
		}
		return inventory.Disks, nil
	}
}
