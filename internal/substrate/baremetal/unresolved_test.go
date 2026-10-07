package baremetal

import (
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// An apply's observation of the machine that answered without every declared
// address is named by the presence proof that refused it. A removal reads
// nothing and always completes, so it explains nothing; evidence for another
// request and no evidence at all are left to the general reason.
func TestUnresolvedNamesTheRefusingCheck(t *testing.T) {
	definition := planOf(t, reconciliation.Apply).Definitions[0]
	block := reconciliation.Block{BlockDefinition: definition, RequestDigest: "digest"}
	short := encode(t, Evidence{Addresses: []string{"aa:bb:cc:dd:ee:01"}, Postcondition: true, Power: "Off", Request: "digest", UUID: "uuid-1"})
	other := encode(t, Evidence{Addresses: []string{"aa:bb:cc:dd:ee:01"}, Postcondition: true, Power: "Off", Request: "another", UUID: "uuid-1"})
	subject := "Machine server on Machine controller"
	for name, test := range map[string]struct {
		verb     reconciliation.Verb
		evidence []byte
		want     lifecycle.Unresolved
		explains bool
	}{
		"an apply over a machine short of an address": {reconciliation.Apply, short, lifecycle.Unresolved{
			Reason: subject + " is not what its apply froze: the machine does not report every hardware address this Machine declares",
			Remedy: "restore " + subject + " to what its frozen request names",
		}, true},
		"a removal":                    {reconciliation.Destroy, short, lifecycle.Unresolved{}, false},
		"evidence for another request": {reconciliation.Apply, other, lifecycle.Unresolved{}, false},
		"no evidence":                  {reconciliation.Apply, nil, lifecycle.Unresolved{}, false},
	} {
		t.Run(name, func(t *testing.T) {
			got, explains := NewMachine(nil).Unresolved(test.verb, block, test.evidence)
			if explains != test.explains || got != test.want {
				t.Fatalf("unresolved = %+v (%t), want %+v (%t)", got, explains, test.want, test.explains)
			}
		})
	}
}
