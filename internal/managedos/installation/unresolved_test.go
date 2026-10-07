package installation

import (
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// An apply's observation of a guest holding this operation's marker, its
// postcondition claimed while its fleet account does not answer, is named by
// the presence check that refused it. A removal reads only the published
// content, which it either finds withdrawn or left to converge, so no evidence
// for this request leaves it unknown; evidence for another request and no
// evidence at all are left to the general reason.
func TestUnresolvedNamesTheRefusingCheck(t *testing.T) {
	execution, request := execution(t, "digest")
	block := execution.Block
	block.Object = "rhel-01"
	marker, err := MarkerFor(request, "digest")
	if err != nil {
		t.Fatal(err)
	}
	silent := encode(t, Evidence{
		Address: request.Address, HostKey: "ssh-ed25519 AAAAHOST", Marker: string(marker), Postcondition: true, Power: "On", Request: "digest",
	})
	other := encode(t, Evidence{Marker: string(marker), Postcondition: true, Power: "On", Request: "another"})
	subject := "the installation of Machine rhel-01 on Machine controller"
	for name, test := range map[string]struct {
		verb     reconciliation.Verb
		evidence []byte
		want     lifecycle.Unresolved
		explains bool
	}{
		"an apply over a guest whose fleet account does not answer": {reconciliation.Apply, silent, lifecycle.Unresolved{
			Reason: subject + " is not what its apply froze: the installed machine did not answer as its fleet account",
			Remedy: "restore " + subject + " to what its frozen request names",
		}, true},
		"a removal over that guest":    {reconciliation.Destroy, silent, lifecycle.Unresolved{}, false},
		"evidence for another request": {reconciliation.Apply, other, lifecycle.Unresolved{}, false},
		"no evidence":                  {reconciliation.Apply, nil, lifecycle.Unresolved{}, false},
	} {
		t.Run(name, func(t *testing.T) {
			got, explains := New(nil).Unresolved(test.verb, block, test.evidence)
			if explains != test.explains || got != test.want {
				t.Fatalf("unresolved = %+v (%t), want %+v (%t)", got, explains, test.want, test.explains)
			}
		})
	}
}
