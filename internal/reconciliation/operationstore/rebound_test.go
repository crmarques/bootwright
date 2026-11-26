package operationstore

import (
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

// B1868: a recorded rebind belongs to a removal or a restore, names only
// bindings its operation records as issued, and no chain of them returns to
// a binding it replaced.
func TestARecordedRebindNamesIssuedBindingsItsOperationRecordsWithoutACycle(t *testing.T) {
	for name, test := range map[string]struct {
		operation Operation
		valid     bool
	}{
		"a removal":                      {Operation{Verb: reconciliation.Destroy, Bindings: []string{"bind-1", "bind-2"}, Rebound: map[string]string{"bind-0": "bind-2"}}, true},
		"a restore":                      {Operation{Verb: reconciliation.Apply, Restores: "op-1", Bindings: []string{"bind-3", "bind-4"}, Rebound: map[string]string{"bind-1": "bind-4"}}, true},
		"a chain":                        {Operation{Verb: reconciliation.Destroy, Bindings: []string{"bind-2", "bind-3"}, Rebound: map[string]string{"bind-1": "bind-2", "bind-2": "bind-3"}}, true},
		"an apply that restores nothing": {Operation{Verb: reconciliation.Apply, Bindings: []string{"bind-2"}, Rebound: map[string]string{"bind-1": "bind-2"}}, false},
		"an issued binding not recorded": {Operation{Verb: reconciliation.Destroy, Bindings: []string{"bind-1"}, Rebound: map[string]string{"bind-1": "bind-2"}}, false},
		"a cycle":                        {Operation{Verb: reconciliation.Destroy, Bindings: []string{"bind-1", "bind-2"}, Rebound: map[string]string{"bind-1": "bind-2", "bind-2": "bind-1"}}, false},
		"a binding replaced by itself":   {Operation{Verb: reconciliation.Destroy, Bindings: []string{"bind-1"}, Rebound: map[string]string{"bind-1": "bind-1"}}, false},
	} {
		if err := validateRebound(test.operation); (err == nil) != test.valid {
			t.Errorf("%s: validateRebound = %v, want valid %v", name, err, test.valid)
		}
	}
}
