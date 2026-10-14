package operationstore

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

// An index only the retired Ceph qualification fixture wrote, of version 2 or
// carrying a qualification member, refuses as that fixture's index: it names
// the build that prepared the fixture, and the orphan acknowledgement only
// without that build. Every other index is left to its own validation.
func TestAnIndexTheRetiredQualificationFixtureWroteRefusesWithItsExit(t *testing.T) {
	const named = "dispose or clean up the fixture and destroy the context with the build that prepared it, then bootwright context delete --name lab --purge; only if that build is unavailable, bootwright context delete --name lab --purge --allow-orphans, which abandons its Machines, disks and reserved keyring bindings"
	source := "op-" + strings.Repeat("ab", 16)
	anchor := `"qualification":{"id":"qual-` + strings.Repeat("cd", 16) + `","source":"` + source + `"}`
	for form, data := range map[string]string{
		"version 2":              `{"version":2,"current":"` + source + `",` + anchor + "}\n",
		"a qualification member": `{"version":1,"current":"` + source + `",` + anchor + "}\n",
	} {
		_, err := func() (Index, error) {
			store, area := newStore(t)
			area.files[indexPath] = []byte(data)
			return store.Index(context.Background())
		}()
		reported := diagnostics.Of(err)
		if !errors.Is(err, ErrRetiredIndex) || len(reported) != 1 || reported[0].Code != "lifecycle.state" || reported[0].Remediation != RetiredIndexRemedy("<ctx>") {
			t.Errorf("%s: the store read %+v (%v)", form, reported, err)
		}
		err = CheckIndex([]byte(data), "lab")
		reported = diagnostics.Of(err)
		if !errors.Is(err, ErrRetiredIndex) || len(reported) != 1 || reported[0].Code != "lifecycle.state" || reported[0].Remediation != named ||
			reported[0].Message != "context lab holds the operation index of the retired Ceph qualification fixture, which this build cannot read" {
			t.Errorf("%s: the check refused with %+v", form, reported)
		}
	}
	if err := CheckIndex([]byte(`{"version":1,"current":"`+source+`"}`+"\n"), "lab"); err != nil {
		t.Fatalf("an ordinary index refused: %v", err)
	}
}
