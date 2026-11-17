package managedservice

import (
	"slices"
	"testing"
)

func TestTheManagedServiceReservationKeysKeepTheirText(t *testing.T) {
	sockets := SocketKeys("::", 53, []Endpoint{{Name: "ip", Address: "192.0.2.10"}})
	if !slices.Equal(sockets, []string{"socket::::53", "socket:192.0.2.10:53"}) {
		t.Errorf("sockets = %q", sockets)
	}
	if got := SocketKeys("192.0.2.10", 3128, nil); !slices.Equal(got, []string{"socket:192.0.2.10:3128"}) {
		t.Errorf("bound sockets = %q", got)
	}
	want := []string{"path:/srv/bootwright/dns", "socket:192.0.2.10:53", "socket::::53", "unit:bootwright-dns"}
	if got := ReservationKeys("bootwright-dns", "/srv/bootwright/dns", sockets); !slices.Equal(got, want) {
		t.Errorf("keys = %q, want %q", got, want)
	}
}
