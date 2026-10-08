package installation

import (
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

// A bound fleet key the attempt cannot read names the exact command that
// repeats the operation, which reopens its Secret bindings: an apply's
// continuation with its tokens, or status's next command when the attempt
// carries none.
func TestAnUnboundFleetKeyNamesTheCommandThatReopensIt(t *testing.T) {
	const reference = "fleet-key"
	for _, continuation := range []string{"bootwright apply --context lab --authorize data-loss", ""} {
		want := "repeat bootwright apply --context lab --authorize data-loss, which reopens this operation's Secret bindings"
		if continuation == "" {
			want = "run bootwright status --context lab and repeat the command it names, which reopens this operation's Secret bindings"
		}
		for code, material := range map[string]map[string]secrets.Material{
			"secret.store": {},
			"secret.part":  {reference: secrets.NewMaterial(map[secrets.Part][]byte{secrets.PrivateKeyPart: []byte("private")})},
		} {
			execution := lifecycle.Execution{Context: "lab", Continuation: continuation, Material: material}
			_, err := publicHalf(execution, reference, "fleet access key")
			if got := diagnostics.Of(err); len(got) != 1 || got[0].Code != code || got[0].Remediation != want {
				t.Fatalf("continuation %q, %s: refused with %#v, want the remedy %q", continuation, code, got, want)
			}
		}
	}
}
