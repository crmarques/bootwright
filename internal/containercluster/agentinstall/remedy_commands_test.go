package agentinstall

import (
	"context"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/material"
)

// An attempt that holds no retained controller areas names the setup and the
// controller stage of its own context, so a copied remedy never stages
// another context's controller.
func TestAToolLookupWithoutRetainedAreasNamesTheStageCommandOfItsContext(t *testing.T) {
	_, err := ToolPath(context.Background(), lifecycle.Execution{Context: "lab"}, Tool{Kind: "openshift", Version: "4.18.1"}, "openshift-install")
	got := diagnostics.Of(err)
	if len(got) != 1 || got[0].Code != "controller.state" ||
		got[0].Remediation != "run bootwright setup, then bootwright apply --context lab --stage controller" {
		t.Fatalf("the lookup refused with %#v", got)
	}
}

// A bound administration key the attempt cannot read names the exact command
// that repeats the operation, which reopens its Secret bindings: an apply's
// continuation with its tokens, or status's next command when the attempt
// carries none.
func TestAnUnboundAdministrationKeyNamesTheCommandThatReopensIt(t *testing.T) {
	const reference = "cluster-key"
	for _, continuation := range []string{"bootwright apply --context lab --authorize data-loss", ""} {
		want := "repeat bootwright apply --context lab --authorize data-loss, which reopens this operation's Secret bindings"
		if continuation == "" {
			want = "run bootwright status --context lab and repeat the command it names, which reopens this operation's Secret bindings"
		}
		for code, bound := range map[string]map[string]secrets.Material{
			"secret.store": {},
		} {
			execution := lifecycle.Execution{Context: "lab", Continuation: continuation, Material: bound}
			_, err := NewMedia(nil, material.AuthorizedKey).publicHalf(execution, reference)
			if got := diagnostics.Of(err); len(got) != 1 || got[0].Code != code || got[0].Remediation != want {
				t.Fatalf("continuation %q, %s: refused with %#v, want the remedy %q", continuation, code, got, want)
			}
		}
	}
}
