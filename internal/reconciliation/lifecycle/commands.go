package lifecycle

import (
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

// contextCommand is a context-backed command as a remedy or next step names
// it: bound to the context the invocation resolved, so a copied remedy never
// acts on another context's object of the same name.
func contextCommand(contextName, verb string, flags ...string) string {
	return strings.Join(append([]string{"bootwright", verb, "--context", contextName}, flags...), " ")
}

func planCommand(contextName string, plan reconciliation.Plan, flags ...string) string {
	if plan.Preparation == nil || plan.Preparation.Command == string(reconciliation.Apply) {
		return contextCommand(contextName, string(plan.Verb), flags...)
	}
	return preparationScopeCommand(contextName, *plan.Preparation, flags...)
}

func preparationScopeCommand(contextName string, scope reconciliation.PreparationScope, flags ...string) string {
	scoped := []string{"--name", strings.Join(scope.Targets, ",")}
	if scope.PreparePrerequisites {
		scoped = append(scoped, "--stage", "controller")
	}
	return contextCommand(contextName, scope.Command, append(scoped, flags...)...)
}

// resolutionRemedy is what a resolution asks for once it proves its block's
// effect never performed or, when partial, only partly realized. An apply is
// continued against its whole frozen plan, so it names that continuation with
// every token the plan consumes, and a partial one also the removal that takes
// back what it owns. A removal names no command of its own: the destroy that
// follows replaces or resolves it with the tokens of every block still not
// proved gone once the whole run settles, which one block's resolution cannot
// know while others run, so it defers to the result's last diagnostic, which
// names that destroy exactly.
func (s Service) resolutionRemedy(contextName string, verb reconciliation.Verb, frozen reconciliation.Plan, selection reconciliation.Selection, partial bool) string {
	if verb != reconciliation.Apply {
		if partial {
			return "run the destroy this result's last diagnostic names, which converges it"
		}
		return "run the destroy this result's last diagnostic names, which performs it"
	}
	continuation := s.preparationRetry(contextName, planCommand(contextName, frozen, authorizing(frozen)...))
	if !selection.Empty() {
		continuation = contextCommand(contextName, "apply --dry-run", invocationFlags("", selection, nil)...)
		if partial {
			return "review the continuation with " + continuation + ", or take back what it owns with " + contextCommand(contextName, string(reconciliation.Destroy))
		}
		return "review the continuation with " + continuation
	}
	if partial {
		return "repeat " + continuation + " to converge it, or take back what it owns with " + contextCommand(contextName, string(reconciliation.Destroy))
	}
	return "repeat " + continuation + " to perform it"
}

// ReopenBindings is the remedy of bound Secret material an attempt could not
// read: the exact command that repeats the operation, which reopens its Secret
// bindings, or, when the attempt carries none, status's next command.
func ReopenBindings(contextName, repeat string) string {
	if repeat != "" {
		return "repeat " + repeat + ", which reopens this operation's Secret bindings"
	}
	return "run " + contextCommand(contextName, "status") + " and repeat the command it names, which reopens this operation's Secret bindings"
}

// reviewStatus is the remedy of a refusal over records that contradict what
// their operation did: reading what the context holds.
func reviewStatus(contextName string) string {
	return "review its durable state with " + contextCommand(contextName, "status")
}

// DigestPlaceholder stands for a digest only a --dry-run preview prints.
const DigestPlaceholder = "<digest apply|destroy --dry-run prints>"

// authorizing binds each token the plan consumes to its steps' digest (Q117),
// for a command that authorizes against exactly this plan.
func authorizing(plan reconciliation.Plan) []string {
	return boundFlags(plan, func(token string) string { return AuthorizationDigest(plan, token) })
}

// authorizingUnfixed binds the tokens to the placeholder, for a removal whose
// plan is not fixed until it runs, so no stale digest is named.
func authorizingUnfixed(plan reconciliation.Plan) []string {
	return boundFlags(plan, func(string) string { return DigestPlaceholder })
}

func boundFlags(plan reconciliation.Plan, digest func(string) string) []string {
	tokens := requiredTokens(plan)
	flags := make([]string, 0, 2*len(tokens))
	for _, token := range tokens {
		flags = append(flags, "--authorize", token+"="+digest(token))
	}
	return slices.Concat(flags, relaxationFlags(frozenRelaxations(plan)), unregisteredFlags(plan))
}

// requiredTokens are the authorizations a plan's blocks consume, in the order
// they are first consumed.
func requiredTokens(plan reconciliation.Plan) []string {
	required := []string{}
	for _, block := range plan.Blocks {
		for _, token := range block.Consumes {
			if !slices.Contains(required, token) {
				required = append(required, token)
			}
		}
	}
	return required
}

// continuationCommand is the command an operator runs for what an incomplete
// operation's records call for, with every token that command's decision
// requires. A replacement is a fresh removal of the blocks the failed one has
// not proved gone, so it needs theirs. A continuation and a resolution repeat
// the operation's own verb. An unfiltered apply continuation reviews its
// reachable attempts. Finalization consumes nothing.
func continuationCommand(contextName string, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState) string {
	switch action := nextAction(operation, frozen, states); action {
	case "none":
		return ""
	case string(reconciliation.Destroy):
		return contextCommand(contextName, action, invocationFlags(frozenFloor(operation), reconciliation.Selection{}, authorizingUnfixed(reconciliation.RemainingSubset(frozen, states)))...)
	}
	if !pendingRemains(frozen, states) {
		return planCommand(contextName, frozen, invocationFlags(frozenFloor(operation), reconciliation.Selection{}, slices.Concat(relaxationFlags(frozenRelaxations(frozen)), unregisteredFlags(frozen)))...)
	}
	return planCommand(contextName, frozen, invocationFlags(frozenFloor(operation), reconciliation.Selection{}, authorizing(authorizationPlan(frozen, states, reconciliation.Selection{})))...)
}
