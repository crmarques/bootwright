package installation

import (
	"slices"
	"testing"
)

func TestTheInstallationReservationKeysKeepTheirText(t *testing.T) {
	request := Request{Image: &Publication{Path: "/srv/artifacts/lab/node.iso"}, Private: &Publication{Path: "/srv/artifacts/.private/lab/node"}}
	if got := request.ReservationKeys(); !slices.Equal(got, []string{"path:/srv/artifacts/.private/lab/node", "path:/srv/artifacts/lab/node.iso"}) {
		t.Errorf("keys = %q", got)
	}
}
