package lifecycle

import (
	"context"
	"slices"

	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

func projectionOf(operation operationstore.Operation) ([]byte, error) {
	if trimmed(operation) {
		return reconciliation.Evidence{Operation: reconciliation.MutationNone, Ownership: reconciliation.OwnershipRetained}.Bytes()
	}
	return projection(operation.Verb, operation.State)
}

func (s Service) projectRecord(ctx context.Context, tx Transaction, operation operationstore.Operation) error {
	data, err := projectionOf(operation)
	if err != nil {
		return err
	}
	return tx.PublishEvidence(ctx, data)
}

func (s Service) completeStaged(ctx context.Context, tx Transaction, store recordReader, operation operationstore.Operation) error {
	owned, err := stagedLineage(ctx, store, operation)
	if err != nil {
		return err
	}
	err = tx.Secrets(ctx, func(selected secretstore.Context, area secretstore.Area) error {
		_, err := s.binder.WithdrawBlocks(ctx, selected, area, operation.Stage.TakenBack)
		return err
	})
	if err != nil {
		return err
	}
	if err := s.reserveKept(ctx, tx, owned, operation.Stage.TakenBack); err != nil {
		return err
	}
	if err := s.projectRecord(ctx, tx, operation); err != nil {
		return err
	}
	reclaim(ctx, tx, s.store(tx))
	return nil
}

func stagedLineage(ctx context.Context, store recordReader, operation operationstore.Operation) (reconciliation.Plan, error) {
	held, err := sourceLineage(ctx, store, operation.Source)
	if err != nil {
		return reconciliation.Plan{}, err
	}
	return held.plan()
}

type claimIdentity struct{ kind, service string }

// reserveKept keeps the shared claim keys kept steps plan, and marks exactly the gone objects' claims.
func (s Service) reserveKept(ctx context.Context, tx Transaction, owned reconciliation.Plan, taken []string) error {
	dropped, planned := map[claimIdentity][]string{}, map[claimIdentity][]string{}
	for _, block := range owned.Blocks {
		target := planned
		if _, scoped := slices.BinarySearch(taken, block.ID); scoped {
			target = dropped
		}
		claimer, claims := s.capabilityOf(block).(SharedClaimer)
		if !claims {
			continue
		}
		shared, err := claimer.SharedClaims(block)
		if err != nil {
			return err
		}
		for _, claim := range shared {
			identity := claimIdentity{claim.Kind, claim.Service}
			target[identity] = append(target[identity], claim.Keys...)
		}
	}
	gone := goneObjects(owned, taken)
	changed := false
	var kept []infrastructureservices.HostReservation
	for _, reservation := range tx.Controller().State.Reservations {
		if reservation.Context != tx.Identity().Name {
			continue
		}
		reservation.Keys = slices.Clone(reservation.Keys)
		identity := claimIdentity{reservation.Kind, reservation.Service}
		if removed := dropped[identity]; reservation.Shared && len(removed) != 0 {
			still := planned[identity]
			keys := slices.DeleteFunc(slices.Clone(reservation.Keys), func(key string) bool {
				return slices.Contains(removed, key) && !slices.Contains(still, key)
			})
			if len(still) == 0 || len(keys) == 0 {
				changed = true
				continue
			}
			changed = changed || len(keys) != len(reservation.Keys)
			reservation.Keys = keys
		}
		if releasing := namesGone(reservation, gone); reservation.Releasing != releasing {
			reservation.Releasing, changed = releasing, true
		}
		kept = append(kept, reservation)
	}
	if !changed {
		return nil
	}
	return tx.Reserve(ctx, kept)
}

func (s Service) capabilityOf(block reconciliation.Block) Capability {
	capability, _ := s.capabilities.Resolve(block.Kind, block.Implementation)
	return capability
}

func goneObjects(owned reconciliation.Plan, taken []string) []reconciliation.ObjectRef {
	var gone, kept []reconciliation.ObjectRef
	for _, block := range owned.Blocks {
		object := reconciliation.ObjectRef{Kind: block.Kind, Object: block.Object}
		if _, scoped := slices.BinarySearch(taken, block.ID); scoped {
			gone = append(gone, object)
		} else {
			kept = append(kept, object)
		}
	}
	return slices.DeleteFunc(gone, func(object reconciliation.ObjectRef) bool { return slices.Contains(kept, object) })
}

func namesGone(reservation infrastructureservices.HostReservation, gone []reconciliation.ObjectRef) bool {
	return slices.ContainsFunc(gone, func(object reconciliation.ObjectRef) bool {
		return infrastructureservices.NamesObject(reservation, object.Kind, object.Object)
	})
}

func stagedFinalized(ctx context.Context, view View, store recordReader, operation operationstore.Operation) (bool, error) {
	owned, err := stagedLineage(ctx, store, operation)
	if err != nil {
		return false, err
	}
	gone := goneObjects(owned, operation.Stage.TakenBack)
	name := view.Identity().Name
	return !slices.ContainsFunc(view.Controller().State.Reservations, func(reservation infrastructureservices.HostReservation) bool {
		return reservation.Context == name && reservation.Releasing && !namesGone(reservation, gone)
	}), nil
}

func trimmedReopen(ctx context.Context, store OperationStore, operation operationstore.Operation, plan reconciliation.Plan, states map[string]reconciliation.BlockState) (frozenReopen, error) {
	folded, err := removalLineage(ctx, store, lineageMember{operation: operation, plan: plan, states: states})
	if err != nil {
		return frozenReopen{}, err
	}
	presents := folded.presenting()
	keeps := reconciliation.Plan{}
	kept := folded.kept()
	for _, holding := range folded.ownership().Holdings() {
		if kept[holding.Block.ID] {
			keeps.Blocks = append(keeps.Blocks, holding.Block)
		}
	}
	if presents.lineage == "" && len(presents.steps) == 0 {
		return frozenReopen{}, nil
	}
	return frozenReopen{operation: operation, reopens: reopensOf(presents.narrowed(keeps.Blocks), keeps)}, nil
}
