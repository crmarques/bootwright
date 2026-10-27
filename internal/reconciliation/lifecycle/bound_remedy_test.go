package lifecycle

import (
	"context"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// unfixed is what a remedy writes for the digest of a plan that is not fixed
// yet; only bootwright plan prints the digest itself.
const unfixed = "data-loss=" + DigestPlaceholder

// shownBinding is the bound data-loss authorization the context's plan prints
// now, which is what an operator reads before repeating a remedy.
func shownBinding(t *testing.T, h *harness, contextName string) string {
	t.Helper()
	preview, err := h.service.Plan(context.Background(), PlanRequest{ContextName: contextName})
	if err != nil || len(preview.Authorizations) != 1 || !strings.HasPrefix(preview.Authorizations[0], "data-loss=") {
		t.Fatalf("the plan of %s = %+v (%v), want one bound data-loss authorization", contextName, preview, err)
	}
	return preview.Authorizations[0]
}

// authorizeTokens reads the --authorize values a command names.
func authorizeTokens(command string) []string {
	tokens := []string{}
	for _, part := range strings.Split(command, " --authorize ")[1:] {
		token := part
		if end := strings.Index(part, ">"); strings.Contains(part, "=<") && end >= 0 {
			token = part[:end+1]
		} else if end := strings.IndexAny(part, " ,"); end >= 0 {
			token = part[:end]
		}
		tokens = append(tokens, token)
	}
	return tokens
}

// reviewed is tokens as an operator runs them after reading the plan: a
// placeholder becomes the digest the plan prints.
func reviewed(tokens []string, shown string) []string {
	out := make([]string, len(tokens))
	for index, token := range tokens {
		out[index] = strings.Replace(token, unfixed, shown, 1)
	}
	return out
}

// noBareAuthorization fails when a text tells the operator to pass a token
// without the digest of its steps, which an unattended invocation refuses.
func noBareAuthorization(t *testing.T, text string) {
	t.Helper()
	for _, token := range authorizeTokens(text) {
		if !strings.Contains(token, "=") {
			t.Fatalf("%q names a bare --authorize %s, which --yes refuses", text, token)
		}
	}
}

// An operation with no confirmer to ask tells the operator to repeat with
// --yes, which refuses a bare data-loss: the repeat it names binds the token to
// the digest the plan printed, with the selection and acknowledgements it
// carries, and that command run as named completes (Q117).
func TestAMissingConfirmerNamesTheBoundRepeat(t *testing.T) {
	ctx := context.Background()
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{destructive("artifacts")})
	shown := shownBinding(t, h, "lab")
	h.service.options.Confirmer = nil
	_, err := h.service.Apply(ctx, ApplyRequest{ContextName: "lab", Authorizations: []string{"data-loss"}})
	reported := diagnostics.Of(err)
	want := "review the plan, then repeat bootwright apply --context lab --authorize " + shown + " with --yes"
	if len(reported) != 1 || reported[0].Code != "lifecycle.state" || reported[0].Remediation != want {
		t.Fatalf("the unconfirmed apply reported %+v (%v), want the remedy %q", reported, err, want)
	}
	result, err := h.service.Apply(ctx, ApplyRequest{ContextName: "lab", Authorizations: authorizeTokens(reported[0].Remediation), SkipConfirmation: true, Unattended: true})
	if err != nil || result.Receipt.State != "done" {
		t.Fatalf("the repeat the remedy names = %+v (%v), want it to complete", result, err)
	}
}
