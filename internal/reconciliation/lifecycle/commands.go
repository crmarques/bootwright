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

// resolutionRemedy is what a resolution asks for once it proves its block's
// effect never performed or, when partial, only partly realized. An apply is
// continued against its whole frozen plan, so it names that continuation with
// every token the plan consumes, and a partial one also the removal that takes
// back what it owns. A removal names no command of its own: the destroy that
// follows replaces or resolves it with the tokens of every block still not
// proved gone once the whole run settles, which one block's resolution cannot
// know while others run, so it defers to the result's last diagnostic, which
// names that destroy exactly.
func resolutionRemedy(contextName string, verb reconciliation.Verb, frozen reconciliation.Plan, partial bool) string {
	if verb != reconciliation.Apply {
		if partial {
			return "run the destroy this result's last diagnostic names, which converges it"
		}
		return "run the destroy this result's last diagnostic names, which performs it"
	}
	continuation := contextCommand(contextName, string(verb), authorizing(requiredTokens(frozen))...)
	if partial {
		return "repeat " + continuation + " to converge it, or take back what it owns with " + contextCommand(contextName, string(reconciliation.Destroy))
	}
	return "repeat " + continuation + " to perform it"
}

// reviewStatus is the remedy of a refusal over records that contradict what
// their operation did: reading what the context holds.
func reviewStatus(contextName string) string {
	return "review its durable state with " + contextCommand(contextName, "status")
}

// authorizing is the --authorize flag of each token, in the order given.
func authorizing(tokens []string) []string {
	flags := make([]string, 0, 2*len(tokens))
	for _, token := range tokens {
		flags = append(flags, "--authorize", token)
	}
	return flags
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
// the operation's own verb and are authorized against the whole frozen plan,
// except a finalization, which runs no block and consumes nothing.
func continuationCommand(contextName string, operation operationstore.Operation, frozen reconciliation.Plan, states map[string]reconciliation.BlockState) string {
	switch action := nextAction(operation, frozen, states); action {
	case "none":
		return ""
	case string(reconciliation.Destroy):
		return contextCommand(contextName, action, authorizing(requiredTokens(reconciliation.RemainingSubset(frozen, states)))...)
	}
	if !pendingRemains(frozen, states) {
		return contextCommand(contextName, string(operation.Verb))
	}
	return contextCommand(contextName, string(operation.Verb), authorizing(requiredTokens(frozen))...)
}
