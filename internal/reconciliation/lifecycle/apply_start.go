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

type applyStartCheck struct {
	probe       Probe
	checker     applyStartChecker
	description string
}

func (s Service) checkApplyStart(ctx context.Context, run invocation, states map[string]reconciliation.BlockState) error {
	var checks []applyStartCheck
	for _, block := range run.plan.Blocks {
		if !run.effects[block.ID] && !unproved(states[block.ID]) {
			continue
		}
		capability, _ := s.capabilities.Resolve(block.Kind, block.Implementation)
		prompter, prompts := capability.(applyStartPrompter)
		checker, checked := capability.(applyStartChecker)
		if !prompts && !checked {
			continue
		}
		owner, err := ownerID(ctx, run.store, run.operation, block.ID)
		if err != nil {
			return err
		}
		probe := Probe{Context: run.tx.Identity().Name, Block: block, ApplyOperation: owner, Material: run.material}
		if prompts {
			if err := prompter.PromptApplyStart(ctx, probe); err != nil {
				return err
			}
		}
		if checked {
			check := applyStartCheck{probe: probe, checker: checker}
			if describer, described := capability.(applyStartDescriber); described {
				check.description = describer.ApplyStartCheck(block)
			}
			checks = append(checks, check)
		}
	}
	for _, check := range checks {
		if err := s.runApplyStartCheck(ctx, check); err != nil {
			return err
		}
	}
	return nil
}

func (s Service) runApplyStartCheck(ctx context.Context, check applyStartCheck) error {
	if check.description == "" {
		return check.checker.CheckApplyStart(ctx, check.probe)
	}
	event := ProgressEvent{Phase: CheckPhase, Block: "apply-start-" + check.probe.Block.ID, Description: check.description, Status: "running"}
	s.report(ctx, event)
	err := check.checker.CheckApplyStart(ctx, check.probe)
	event.Status = "ok"
	if err != nil {
		event.Status = "failed"
	}
	s.report(ctx, event)
	return err
}
