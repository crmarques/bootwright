package lifecycle

import (
	"context"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

func (s Service) Apply(ctx context.Context, request ApplyRequest) (*OperationResult, error) {
	s, err := s.withParallelism(request.Parallelism)
	if err != nil {
		return nil, err
	}
	selection, err := reconciliation.ParseSelection(request.Stages, request.Until)
	if err != nil {
		return nil, err
	}
	if err := s.available(ctx); err != nil {
		return nil, err
	}
	request.ContextName, err = s.resolve(ctx, request.ContextName)
	if err != nil {
		return nil, err
	}
	return s.applyPrepared(ctx, request, selection)
}

// Destroy removes what an apply recorded as owned. It accepts no stage
// selection: a removal covers exactly the effects that exist.
func (s Service) Destroy(ctx context.Context, request DestroyRequest) (*OperationResult, error) {
	s, err := s.withParallelism(request.Parallelism)
	if err != nil {
		return nil, err
	}
	return s.mutate(ctx, reconciliation.Destroy, request.ContextName, reconciliation.Selection{}, request.Authorizations, request.Unregistered, request.SkipConfirmation, request.Unattended, request.SSH.Borrowed())
}

func (s Service) mutate(ctx context.Context, verb reconciliation.Verb, contextName string, selection reconciliation.Selection, authorizations, unregistered []string, skipConfirmation, unattended, borrowed bool) (*OperationResult, error) {
	if err := s.available(ctx); err != nil {
		return nil, err
	}
	if borrowed {
		return nil, failure("lifecycle.state",
			"borrowed SSH credentials are unsupported for lifecycle operations",
			"remove --ssh-user, --ssh-id-file and --ssh-ask-sudo-password and author the Machine's own access")
	}
	name, err := s.resolve(ctx, contextName)
	if err != nil {
		return nil, err
	}
	// A destroy lists the context's bindings before it decides, so one that
	// settles releases only what existed before it read pristine evidence.
	var stranded []string
	if verb == reconciliation.Destroy {
		stranded = s.held(ctx, name)
	}
	decided, identity, err := s.decideShared(ctx, name, verb, selection)
	if err != nil {
		return nil, err
	}
	// An operation its block records already prove complete is finalized
	// before anything is authorized, presented or confirmed, because doing so
	// performs no effect: only the record, releases and projection those
	// records prove. The verb then decides again from what that leaves.
	finalized := decided.finalize
	if decided.finalize {
		if decided, identity, err = s.finalizeFirst(ctx, name, verb, selection, decided); err != nil {
			return nil, err
		}
	}
	// What an interrupted registration left with no operation to own it is
	// released the same way, for the same reason: it performs no effect, only
	// the releases and the pristine evidence that no operation's records need.
	if decided.unclaimed {
		if err := s.releaseUnclaimed(ctx, name, decided); err != nil {
			return nil, err
		}
		return settled(identity, operationPlan{verb: verb}, RecoveredRelease), nil
	}
	if decided.noop {
		return s.finishSettled(ctx, name, decided, identity, finalized, stranded)
	}

	if err := s.refuseLostBinding(ctx, name, decided, decided.basis); err != nil {
		return nil, err
	}
	if err := s.proveShared(ctx, name, decided); err != nil {
		return nil, err
	}
	if err := s.admitApply(ctx, name, decided); err != nil {
		return nil, err
	}
	// The plan is presented before authorization refuses, so a refusal names
	// steps the operator has just read; both precede the prompt and
	// registration.
	if err := s.present(ctx, name, decided); err != nil {
		return nil, err
	}
	tokens, rebinds, err := splitRebinds(name, verb, authorizations)
	if err != nil {
		return nil, err
	}
	if prompts, reports := s.options.Confirmer.(InteractiveConfirmer); reports && !prompts.Interactive() {
		unattended = true
	}
	if err := s.authorized(name, decided, tokens, unregistered, unattended); err != nil {
		return nil, err
	}
	if decided, err = s.admitRebinds(ctx, name, decided, rebinds); err != nil {
		return nil, err
	}
	if !skipConfirmation {
		if s.options.Confirmer == nil {
			remedy := "review the plan and repeat with --yes"
			if len(requiredTokens(decided.plan)) != 0 {
				remedy = "review the plan, then repeat " + contextCommand(name, string(verb), invocationFlags(decided.selection, authorizing(decided.plan), unregistered)...) + " with --yes"
			}
			return nil, failure("lifecycle.state", "this operation requires confirmation", remedy)
		}
		if err := s.options.Confirmer.Confirm(ctx, string(verb), name); err != nil {
			return nil, err
		}
	}
	return s.execute(releasing(ctx, unregistered), name, decided)
}

func (s Service) admitApply(ctx context.Context, name string, decided operationPlan) error {
	if decided.verb != reconciliation.Apply || (s.options.ApplyAdmission == nil && s.options.ListenerAdmission == nil) {
		return nil
	}
	return s.workspace.ReadLifecycle(ctx, name, func(view View) error {
		return s.admitApplyView(ctx, view, decided)
	})
}

func (s Service) admitApplyView(ctx context.Context, view View, decided operationPlan) error {
	if decided.verb != reconciliation.Apply || (s.options.ApplyAdmission == nil && s.options.ListenerAdmission == nil) {
		return nil
	}
	state, err := s.compile(ctx, view)
	if err != nil {
		return err
	}
	owned := map[string]ObjectOwnership{}
	for _, block := range decided.plan.Blocks {
		current := decided.states[block.ID]
		if current != "" && current != reconciliation.BlockPending {
			recordOwnership(owned, block, reconciliation.Apply, current)
		}
	}
	starting := decided.starting()
	if s.options.ListenerAdmission != nil {
		if err := s.options.ListenerAdmission(ctx, view.Identity().Name, state.Effective(), decided.plan, starting); err != nil {
			return err
		}
	}
	if s.options.ApplyAdmission != nil {
		return s.options.ApplyAdmission(ctx, view.Identity().Name, state.Effective(), owned, starting)
	}
	return nil
}

// withParallelism changes only this invocation's service copy.
func (s Service) withParallelism(value int) (Service, error) {
	if value < 0 {
		return s, failure("lifecycle.state", "parallelism must not be negative", "use zero for the service default or a positive running-step limit")
	}
	if value > 0 {
		s.options.Concurrency = value
	}
	return s, nil
}
