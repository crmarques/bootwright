package cli

import (
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/host/shellquote"
	"github.com/crmarques/bootwright/internal/preflight"
	"github.com/crmarques/bootwright/internal/status"
)

func nextStepHints(stateLoaded bool, state v1alpha1.State, renderedDir string, clustersDir string, contextName string, secretsDir string, runsDir string) []string {
	var secretHints []status.NextStepHint
	needsHostTrust := false
	if stateLoaded {
		secretHints = secretNextStepHints(state, contextName, secretsDir)
		needsHostTrust = preflight.NeedsHostTrust(state, secretsDir)
	}
	applied := workflow.HasConvergeSafetyRecords(runsDir)
	return renderNextStepHints(status.NextStepHints(stateLoaded, state, renderedDir, clustersDir, contextName, secretHints, needsHostTrust, applied))
}

func secretNextStepHints(state v1alpha1.State, contextName, secretsDir string) []status.NextStepHint {
	entries, err := declaredSecretEntriesForContext(contextName, secretsDir, state)
	return status.SecretNextStepHints(state, entries, err, contextName)
}

func renderNextStepHints(hints []status.NextStepHint) []string {
	result := make([]string, 0, len(hints))
	for _, hint := range hints {
		switch {
		case hint.Action != "":
			result = append(result, renderStatusNextStepAction(hint))
		case len(hint.Args) > 0:
			result = append(result, shellquote.QuoteWords(hint.Args))
		case strings.TrimSpace(hint.Guidance) != "":
			result = append(result, hint.Guidance)
		}
	}
	return result
}

func renderStatusNextStepAction(hint status.NextStepHint) string {
	if hint.Action != status.NextStepActionApply || strings.TrimSpace(hint.ContextName) == "" {
		return "review the status evidence and choose the next command explicitly; no runnable command is suggested for the unresolved next-step action"
	}
	invocation, err := newResolvedInvocation(invocationApply, hint.ContextName, invocationFlags{
		mode:            workflow.ApplyModeReconcile,
		askBecomePass:   askBecomePassDefault(),
		trustOnFirstUse: true,
	})
	if err != nil {
		return "review the status evidence and choose the next command explicitly; no runnable command is suggested because the apply invocation could not be resolved: " + err.Error()
	}
	command, err := invocation.retry(retryIntent{})
	if err != nil {
		return "review the status evidence and choose the next command explicitly; no runnable command is suggested because the apply invocation could not be formatted: " + err.Error()
	}
	return command.String()
}
