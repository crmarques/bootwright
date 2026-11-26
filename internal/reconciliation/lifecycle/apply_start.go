package lifecycle

import (
	"context"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

type applyStartChecker interface {
	CheckApplyStart(context.Context, Probe) error
}

type applyStartPrompter interface {
	PromptApplyStart(context.Context, Probe) error
}

type applyStartDescriber interface {
	ApplyStartCheck(reconciliation.Block) string
}

func applyStartBlocks(plan reconciliation.Plan, states map[string]reconciliation.BlockState, effects map[string]bool) []reconciliation.Block {
	var blocks []reconciliation.Block
	for _, block := range plan.Blocks {
		if effects[block.ID] || unproved(states[block.ID]) {
			blocks = append(blocks, block)
		}
	}
	return blocks
}

func (s Service) promptApplyStart(ctx context.Context, name string, decided operationPlan) error {
	effects, _ := reachableEffects(decided.plan, decided.states, decided.selection)
	for _, block := range applyStartBlocks(decided.plan, decided.states, effects) {
		capability, _ := s.capabilities.Resolve(block.Kind, block.Implementation)
		if prompter, ok := capability.(applyStartPrompter); ok {
			if err := prompter.PromptApplyStart(ctx, Probe{Context: name, Block: block}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s Service) checkApplyStart(ctx context.Context, run invocation, states map[string]reconciliation.BlockState) error {
	for _, block := range applyStartBlocks(run.plan, states, run.effects) {
		capability, _ := s.capabilities.Resolve(block.Kind, block.Implementation)
		checker, ok := capability.(applyStartChecker)
		if !ok {
			continue
		}
		owner, err := ownerID(ctx, run.store, run.operation, block.ID)
		if err != nil {
			return err
		}
		probe := Probe{Context: run.tx.Identity().Name, Block: block, ApplyOperation: owner, Material: run.material.of(block.ID)}
		description := ""
		if describer, described := capability.(applyStartDescriber); described {
			description = describer.ApplyStartCheck(block)
		}
		if err := s.runApplyStartCheck(ctx, checker, probe, description); err != nil {
			return err
		}
	}
	return nil
}

func (s Service) runApplyStartCheck(ctx context.Context, checker applyStartChecker, probe Probe, description string) error {
	if description == "" {
		return checker.CheckApplyStart(ctx, probe)
	}
	event := ProgressEvent{Phase: CheckPhase, Block: "apply-start-" + probe.Block.ID, Description: description, Status: "running"}
	s.report(ctx, event)
	err := checker.CheckApplyStart(ctx, probe)
	event.Status = "ok"
	if err != nil {
		event.Status = "failed"
	}
	s.report(ctx, event)
	return err
}
