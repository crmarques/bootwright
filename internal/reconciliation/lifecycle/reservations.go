package lifecycle

import (
	"cmp"
	"slices"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

// canonicalReservations combines contributions to one shared claim and refuses
// an exclusive or inconsistent one; planFrom validates in place and keeps the
// sorted result only once hostKeyedReservations has attached every host key.
func canonicalReservations(claims []prerequisites.HostReservation) ([]prerequisites.HostReservation, error) {
	ordered := slices.Clone(claims)
	for i := range ordered {
		ordered[i].Keys = slices.Sorted(slices.Values(ordered[i].Keys))
		if len(slices.Compact(slices.Clone(ordered[i].Keys))) != len(ordered[i].Keys) {
			return nil, failure("lifecycle.state", "a capability declared duplicate host reservation keys", "")
		}
	}
	compare := func(a, b prerequisites.HostReservation) int {
		return cmp.Or(cmp.Compare(a.Context, b.Context), cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Service, b.Service))
	}
	slices.SortFunc(ordered, compare)
	var combined []prerequisites.HostReservation
	for _, claim := range ordered {
		if len(combined) == 0 || compare(combined[len(combined)-1], claim) != 0 {
			combined = append(combined, claim)
		} else {
			previous := &combined[len(combined)-1]
			if !previous.Shared || !claim.Shared || previous.ObjectKind != claim.ObjectKind || previous.Releasing != claim.Releasing {
				return nil, failure("lifecycle.state", "capabilities declared incompatible host reservations for "+claim.Kind+"/"+claim.Service, "")
			}
			previous.Keys = append(previous.Keys, claim.Keys...)
			slices.Sort(previous.Keys)
			previous.Keys = slices.Compact(previous.Keys)
		}
		if len(combined[len(combined)-1].Keys) > prerequisites.MaxPublishedReservationKeys {
			return nil, failure("lifecycle.state", "the composed host reservation exceeds its key bound", "")
		}
	}
	return combined, nil
}
