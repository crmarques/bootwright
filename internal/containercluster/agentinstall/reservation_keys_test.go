package agentinstall

import (
	"slices"
	"testing"
)

func TestTheMediaReservationKeysKeepTheirText(t *testing.T) {
	request := MediaRequest{WorkRoot: "/var/lib/bootwright/work", Image: Publication{Path: "/srv/artifacts/lab/agent.iso"}}
	if got := request.ReservationKeys(); !slices.Equal(got, []string{"path:/srv/artifacts/lab/agent.iso", "path:/var/lib/bootwright/work"}) {
		t.Errorf("keys = %q", got)
	}
	request.DeliveryKind = "push"
	if got := request.ReservationKeys(); !slices.Equal(got, []string{"path:/var/lib/bootwright/work"}) {
		t.Errorf("pushed keys = %q", got)
	}
}
