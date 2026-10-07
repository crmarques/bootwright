package lifecycle

import (
	"context"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/secrets"
)

// bindingProver is the optional half of a capability whose bound material must
// be proved before registration. It reads only the material the fresh apply
// just bound, so a refusal leaves no operation, reservation or binding.
type bindingProver interface {
	ProveBinding(ctx context.Context, contextName string, block reconciliation.Block, material map[string]secrets.Material) error
}

// proveBindings asks every capability of a fresh apply's blocks, in plan
// order, to prove the material it just bound, and returns the first refusal.
// A continuation proves the version its operation bound in its own attempt.
func (s Service) proveBindings(ctx context.Context, name string, decided transition, material map[string]secrets.Material) error {
	if !decided.fresh || decided.verb != reconciliation.Apply {
		return nil
	}
	for _, block := range decided.plan.Blocks {
		capability, found := s.capabilities.Resolve(block.Kind, block.Implementation)
		if !found {
			continue
		}
		if prover, ok := capability.(bindingProver); ok {
			if err := prover.ProveBinding(ctx, name, block, material); err != nil {
				return err
			}
		}
	}
	return nil
}
