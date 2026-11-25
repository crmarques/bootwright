package lifecycle

import (
	"context"
	"maps"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

type removalInputs struct {
	relaxations  map[string]string
	unregistered map[string]bool
	nothing      func(context.Context, reconciliation.Block) (bool, error)
	keptHosts    map[string][]string
}

func (s Service) selectedRemoval(ctx context.Context, capability Capability, block reconciliation.Block, removal Removal, inputs removalInputs) (Removal, string, error) {
	if recorded, records := capability.(RecordedRemoval); records && inputs.nothing != nil {
		nothing, err := inputs.nothing(ctx, block)
		if err != nil {
			return Removal{}, "", err
		}
		if nothing {
			removal, err := recorded.NothingRemoval(ctx, block)
			return removal, "", err
		}
	}
	removal, relaxation, err := relaxedRemoval(ctx, capability, block, removal, inputs.relaxations)
	if err != nil || relaxation != "" || !inputs.unregistered[block.Object] {
		return removal, relaxation, err
	}
	unregisterer, unregisters := capability.(RemovalUnregisterer)
	if !unregisters {
		return removal, "", nil
	}
	released, ok, err := unregisterer.Unregistered(ctx, block)
	if err != nil || !ok {
		return removal, "", err
	}
	released.Consumes = removal.Consumes
	return released, Unregistered, nil
}

func (s Service) nothingRecorded(records func(context.Context, string) (RemovalRecords, error)) func(context.Context, reconciliation.Block) (bool, error) {
	if records == nil {
		return nil
	}
	return func(ctx context.Context, block reconciliation.Block) (bool, error) {
		capability, _ := s.capabilities.Resolve(block.Kind, block.Implementation)
		recorded, ok := capability.(RecordedRemoval)
		if !ok {
			return false, nil
		}
		read, err := records(ctx, block.ID)
		if err != nil {
			return false, err
		}
		return recorded.NothingRecorded(block, read), nil
	}
}

func recordsFrom(store recordReader, operation operationstore.Operation) func(context.Context, string) (RemovalRecords, error) {
	return func(ctx context.Context, block string) (RemovalRecords, error) {
		return applyRecordsOf(ctx, store, operation, block)
	}
}

func frozenNothing(plan reconciliation.Plan) func(context.Context, reconciliation.Block) (bool, error) {
	return func(_ context.Context, block reconciliation.Block) (bool, error) {
		frozen, found := plan.Block(block.ID)
		return found && TakesNothingBack(frozen), nil
	}
}

func frozenInputs(plan reconciliation.Plan) removalInputs {
	return removalInputs{relaxations: frozenRelaxations(plan), unregistered: frozenUnregistered(plan), nothing: frozenNothing(plan), keptHosts: frozenKept(plan)}
}

func frozenKept(plan reconciliation.Plan) map[string][]string {
	var kept map[string][]string
	for _, block := range plan.Blocks {
		if hosts := frozenKeptHosts(block); len(hosts) != 0 {
			if kept == nil {
				kept = map[string][]string{}
			}
			kept[block.ID] = slices.Clone(hosts)
		}
	}
	return kept
}

func (s Service) recordsNothing(block reconciliation.Block) bool {
	capability, _ := s.capabilities.Resolve(block.Kind, block.Implementation)
	_, records := capability.(RecordedRemoval)
	return records && TakesNothingBack(block)
}

func TakesNothingBack(block reconciliation.Block) bool {
	return block.Inverse != nil && block.Inverse.Nothing != ""
}

func applyRecordsOf(ctx context.Context, store recordReader, operation operationstore.Operation, block string) (RemovalRecords, error) {
	held, err := appliedLineage(ctx, store, operation)
	if err != nil {
		return RemovalRecords{}, err
	}
	if len(held) == 0 {
		return RemovalRecords{}, engineRecordsDamaged.Fail("the removal names no apply whose records decide its inverse", "restore the context store")
	}
	owner, err := held.routed(block)
	if err != nil {
		return RemovalRecords{}, err
	}
	record, err := store.Block(ctx, owner.operation.ID, block)
	if err != nil {
		return RemovalRecords{}, err
	}
	previous, err := previousBlockEvidence(ctx, store, owner.operation.ID, block)
	if err != nil {
		return RemovalRecords{}, err
	}
	dependents, err := originalRemovalDependents(ctx, store, operation, block)
	if err != nil {
		return RemovalRecords{}, err
	}
	preparations, err := applyPreparations(ctx, store, owner.operation.ID, block)
	if err != nil {
		return RemovalRecords{}, err
	}
	return RemovalRecords{State: record.State, Previous: previous, Dependents: dependents, Preparations: preparations}, nil
}

func proveRecorded(ctx context.Context, capabilities CapabilityResolver, store recordReader, plan reconciliation.Plan, operation operationstore.Operation) error {
	for _, block := range plan.Blocks {
		capability, _ := capabilities.Resolve(block.Kind, block.Implementation)
		if err := proveKeptHosts(ctx, capability, store, block, operation); err != nil {
			return err
		}
		recorded, ok := capability.(RecordedRemoval)
		if !ok {
			continue
		}
		read, err := applyRecordsOf(ctx, store, operation, block.ID)
		if err != nil {
			return err
		}
		frozen, nothing := TakesNothingBack(block), recorded.NothingRecorded(block, read)
		if frozen == nothing || !frozen && recorded.DecidedAtRun() {
			continue
		}
		message := "the removal plan froze that the step " + block.ID + " has nothing to take back, and the original apply's records no longer prove it"
		if !frozen {
			message = "the removal plan froze the effect the step " + block.ID + " takes back, and the original apply's records now prove it has nothing to take back"
		}
		return destroyInverseMoved.Fail(message, "continue or replace the removal "+operation.ID+" with "+registeredWith(operation.Executable))
	}
	return nil
}

func proveKeptHosts(ctx context.Context, capability Capability, store recordReader, block reconciliation.Block, operation operationstore.Operation) error {
	if len(frozenKeptHosts(block)) == 0 {
		return nil
	}
	refusal := "this executable cannot remove it on kept Machines"
	if kept, keeps := capability.(KeptHostRemoval); keeps {
		read, err := applyRecordsOf(ctx, store, operation, block.ID)
		if err != nil {
			return err
		}
		refusal = kept.KeptHostsRefusal(block, read)
	}
	if refusal == "" {
		return nil
	}
	return destroyInverseMoved.Fail("the removal plan froze that the step "+block.ID+" is taken back while Machine/"+strings.Join(frozenKeptHosts(block), ", Machine/")+" stay, and now "+refusal,
		"continue or replace the removal "+operation.ID+" with "+registeredWith(operation.Executable))
}

func frozenKeptHosts(block reconciliation.Block) []string {
	if block.Inverse == nil {
		return nil
	}
	return block.Inverse.KeptHosts
}

func frozenUnregistered(plan reconciliation.Plan) map[string]bool {
	var names map[string]bool
	for _, block := range plan.Blocks {
		if block.Relaxation != Unregistered {
			continue
		}
		if names == nil {
			names = map[string]bool{}
		}
		names[block.Object] = true
	}
	return names
}

func unregisteredFlags(plan reconciliation.Plan) []string {
	names := frozenUnregistered(plan)
	if len(names) == 0 {
		return nil
	}
	return []string{"--unregistered", strings.Join(slices.Sorted(maps.Keys(names)), ",")}
}

func (s Service) acknowledgedReleases(contextName string, decided operationPlan) error {
	if len(s.relaxing.unregistered) == 0 || decided.verb != reconciliation.Destroy || decided.noop && !decided.keeps || decided.unclaimed || decided.finalize {
		return nil
	}
	var frozen map[string]bool
	var tokens []string
	if !decided.noop {
		frozen, tokens = frozenUnregistered(decided.plan), authorizing(decided.plan)
	}
	for _, name := range slices.Sorted(maps.Keys(s.relaxing.unregistered)) {
		if frozen[name] {
			continue
		}
		return authorizeUnneeded.Fail(
			"this destroy removes no subscription registration from Machine/"+name+", so --unregistered "+name+" acknowledges nothing",
			"repeat "+contextCommand(contextName, string(reconciliation.Destroy), invocationFlags(decided.floor(), reconciliation.Selection{}, tokens)...)+" without --unregistered "+name)
	}
	return nil
}
