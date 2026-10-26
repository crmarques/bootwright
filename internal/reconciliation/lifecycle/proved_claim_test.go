package lifecycle

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// A done block whose evidence names a host resource is claimed before any
// block that depends on it starts: a second spelling of one physical
// controller, whose system another context already claimed, refuses
// controller.conflict naming that context, and the dependent block, which
// would insert media, never runs. Without a holder the claim lands beside the
// plan's own reservations, and the dependent block runs.
func TestAProvedClaimIsTakenBeforeItsDependentStarts(t *testing.T) {
	system := "bmc-system:4c4c4544-0042-3510-8052-b4c04f4d4e31"
	for name, holder := range map[string]string{"held by another context": "first", "unheld": ""} {
		t.Run(name, func(t *testing.T) {
			h := newPlannedHarness(t, chainedDefinitions())
			h.capability.claims = map[string][]string{"alpha": {"bmc:198.51.100.21:443/1", system}}
			if holder != "" {
				h.workspace.foreign = []prerequisites.HostReservation{{Context: holder, Kind: "substrate-physical", Service: "metal-01",
					Keys: []string{"bmc:bmc-01.metal.example.test:443/1", system}}}
			}
			_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
			if holder == "" {
				if err != nil {
					t.Fatal(err)
				}
				if !slices.ContainsFunc(h.workspace.reservations, func(held prerequisites.HostReservation) bool {
					return held.Service == "alpha" && slices.Contains(held.Keys, system)
				}) {
					t.Fatalf("the proved claim was not taken: %+v", h.workspace.reservations)
				}
				return
			}
			reported := diagnostics.Of(err)
			if len(reported) == 0 || reported[0].Code != "controller.conflict" || !strings.Contains(reported[0].Message, holder) {
				t.Fatalf("a claim another context holds was taken: %#v", reported)
			}
			if !slices.Equal(h.capability.applies, []string{"alpha"}) {
				t.Fatalf("applied %v, want alpha alone", h.capability.applies)
			}
		})
	}
}

// A fresh apply republishes its planned reservations, which cannot name a UUID
// only a proof learns, so keepProved carries this context's own bmc-system:
// keys onto the planned reservation of the same kind and service, and nothing
// another context or another service holds.
func TestKeepProvedCarriesOnlyTheOwnProvedSystemKey(t *testing.T) {
	planned := []prerequisites.HostReservation{{Context: "lab", Kind: "substrate-physical", Service: "metal-01", Keys: []string{"bmc:a:443/1"}}}
	held := []prerequisites.HostReservation{
		{Context: "lab", Kind: "substrate-physical", Service: "metal-01", Keys: []string{"bmc-system:u1", "bmc:a:443/1"}},
		{Context: "other", Kind: "substrate-physical", Service: "metal-01", Keys: []string{"bmc-system:u2"}},
		{Context: "lab", Kind: "substrate-physical", Service: "metal-02", Keys: []string{"bmc-system:u3"}},
	}
	kept := keepProved(held, "lab", planned)
	if len(kept) != 1 || !slices.Equal(kept[0].Keys, []string{"bmc:a:443/1", "bmc-system:u1"}) || len(planned[0].Keys) != 1 {
		t.Fatalf("kept = %+v, planned %+v", kept, planned)
	}
}
