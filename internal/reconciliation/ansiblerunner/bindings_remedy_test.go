package ansiblerunner

import (
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

// A bound Secret a run needs but cannot read names the exact command that
// repeats its operation, which reopens the operation's Secret bindings, or
// status's next command when the run carries none.
func TestAMissingBindingNamesTheCommandThatReopensIt(t *testing.T) {
	for _, repeat := range []string{"bootwright apply --context lab --authorize data-loss", ""} {
		for code, material := range map[string]map[string]secrets.Material{
			"secret.store": nil,
			"secret.part":  {"key": secrets.NewMaterial(map[secrets.Part][]byte{secrets.PublicKeyPart: []byte("public")})},
		} {
			_, err := materialBytes(lifecycle.RunRequest{
				Context: "lab", Repeat: repeat, Material: material,
				Materials: []lifecycle.MaterialFile{{Name: "id", Part: secrets.PrivateKeyPart, Secret: "key"}},
			})
			want := "repeat bootwright apply --context lab --authorize data-loss, which reopens this operation's Secret bindings"
			if repeat == "" {
				want = "run bootwright status --context lab and repeat the command it names, which reopens this operation's Secret bindings"
			}
			if got := diagnostics.Of(err); len(got) != 1 || got[0].Code != code || got[0].Remediation != want {
				t.Fatalf("repeat %q, %s: refused with %#v, want the remedy %q", repeat, code, got, want)
			}
		}
	}
}
