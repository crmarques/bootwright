package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

type sameBlocksProof struct {
	proved  bool
	refusal error
}

func (s Service) otherBuild(operation operationstore.Operation) bool {
	current := operationstore.Executable{Version: s.options.Executable.Version, Commit: s.options.Executable.Commit}
	return operation.AutomationDigest != s.automation.CatalogDigest() || operation.Executable != current || s.options.Executable.Modified
}

func (s Service) proveSameBlocks(ctx context.Context, view View, operation operationstore.Operation, frozen reconciliation.Plan) sameBlocksProof {
	if !s.otherBuild(operation) {
		return sameBlocksProof{proved: true}
	}
	fresh, err := s.replanned(ctx, view, operation, frozen)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return sameBlocksProof{refusal: ctxErr}
	}
	remediation := otherBuildRemediation(view, operation)
	if err != nil {
		detail := ""
		if reported := diagnostics.Of(err); len(reported) != 0 {
			detail = ": " + reported[0].Message
		}
		return sameBlocksProof{refusal: failure("lifecycle.state",
			"this executable cannot plan this operation again to prove it freezes the same blocks"+detail, remediation)}
	}
	if block, differs := differingBlock(frozen, fresh); differs {
		return sameBlocksProof{refusal: failure("lifecycle.state",
			"this executable would freeze the block "+block+" differently from the plan this operation froze", remediation)}
	}
	return sameBlocksProof{proved: true}
}

func (s Service) replanned(ctx context.Context, view View, operation operationstore.Operation, frozen reconciliation.Plan) (reconciliation.Plan, error) {
	if operation.Verb == reconciliation.Destroy {
		return s.removalOf(ctx, frozen, operation.Executable)
	}
	state, err := s.compile(ctx, view)
	if err != nil {
		return reconciliation.Plan{}, err
	}
	if err := s.refuseUnsupported(state); err != nil {
		return reconciliation.Plan{}, err
	}
	plan, _, err := s.planFrom(ctx, view, state, reconciliation.Apply)
	return plan, err
}

func otherBuildRemediation(view View, operation operationstore.Operation) string {
	registered := registeredWith(operation.Executable)
	if supersedable(operation) {
		return takeBackUnder(view.Identity().Name, "this executable") + ", or install " + registered + ", which registered it"
	}
	return "install " + registered + ", which registered this operation, and run bootwright setup"
}

func differingBlock(frozen, fresh reconciliation.Plan) (string, bool) {
	planned := make(map[string][]byte, len(fresh.Blocks))
	for _, block := range fresh.Blocks {
		planned[block.ID] = effectsOf(block)
	}
	for _, block := range frozen.Blocks {
		theirs, found := planned[block.ID]
		ours := effectsOf(block)
		if !found || ours == nil || theirs == nil || !bytes.Equal(ours, theirs) {
			return block.ID, true
		}
		delete(planned, block.ID)
	}
	for _, block := range fresh.Blocks {
		if _, extra := planned[block.ID]; extra {
			return block.ID, true
		}
	}
	return "", false
}

func effectsOf(block reconciliation.Block) []byte {
	block.Description, block.Impacts = "", nil
	groups := make([]reconciliation.Group, 0, len(block.Groups))
	for _, group := range block.Groups {
		group.Description = ""
		groups = append(groups, group)
	}
	block.Groups = groups
	encoded, err := json.Marshal(block)
	if err != nil {
		return nil
	}
	return encoded
}
