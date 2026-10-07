package prerequisites

import "testing"

func TestAWildcardSocketConflictsWithEveryAddressAtItsPort(t *testing.T) {
	for name, test := range map[string]struct {
		held, wanted             string
		heldShared, wantedShared bool
		conflict                 bool
	}{
		"the same key":                        {held: "unit:bootwright-proxy", wanted: "unit:bootwright-proxy", conflict: true},
		"another address at one port":         {held: "socket:192.0.2.1:3128", wanted: "socket:192.0.2.2:3128"},
		"a held IPv4 wildcard":                {held: "socket:0.0.0.0:3128", wanted: "socket:192.0.2.2:3128", conflict: true},
		"a wanted IPv4 wildcard":              {held: "socket:192.0.2.2:3128", wanted: "socket:0.0.0.0:3128", conflict: true},
		"a held IPv6 wildcard":                {held: "socket::::3128", wanted: "socket:fd00::1:3128", conflict: true},
		"a wanted IPv6 wildcard":              {held: "socket:192.0.2.2:3128", wanted: "socket::::3128", conflict: true},
		"both wildcards":                      {held: "socket:0.0.0.0:3128", wanted: "socket::::3128", conflict: true},
		"a mapped wildcard":                   {held: "socket:::ffff:0.0.0.0:3128", wanted: "socket:192.0.2.2:3128", conflict: true},
		"a wildcard at another port":          {held: "socket:0.0.0.0:3128", wanted: "socket:192.0.2.2:3129"},
		"a port whose digits end the other":   {held: "socket:0.0.0.0:80", wanted: "socket:192.0.2.2:8080"},
		"an address ending in the port":       {held: "socket:0.0.0.0:80", wanted: "socket:fd00::80:8080"},
		"a wildcard beside another key class": {held: "socket:0.0.0.0:3128", wanted: "path:/var/lib/bootwright-services/lab/proxy"},
		"a shared held wildcard":              {held: "socket:0.0.0.0:3128", wanted: "socket:192.0.2.2:3128", heldShared: true},
		"a shared wanted wildcard":            {held: "socket:192.0.2.2:3128", wanted: "socket:0.0.0.0:3128", wantedShared: true},
	} {
		t.Run(name, func(t *testing.T) {
			held := []HostReservation{{Context: "other", Kind: "proxy", Service: "a", Keys: []string{test.held}, Shared: test.heldShared}}
			wanted := []HostReservation{{Context: "lab", Kind: "proxy", Service: "b", Keys: []string{test.wanted}, Shared: test.wantedShared}}
			conflicts := Conflicts(held, wanted)
			if len(conflicts) > 0 != test.conflict || len(conflicts) > 0 && conflicts[0].Held.Context != "other" {
				t.Fatalf("conflicts = %+v, want %t", conflicts, test.conflict)
			}
		})
	}
}

// One context's own sockets conflict as two contexts' do, but a claim never
// conflicts with itself and only sockets are compared, since two of one
// context's installations may publish one package tree.
func TestOneContextsOwnSocketsConflictAsAnotherContextsDo(t *testing.T) {
	for name, test := range map[string]struct {
		first, second []string
		shared        bool
		sockets       [2]string
	}{
		"one socket":                         {first: []string{"socket:192.0.2.1:3128"}, second: []string{"socket:192.0.2.1:3128"}, sockets: [2]string{"192.0.2.1:3128", "192.0.2.1:3128"}},
		"a wildcard beside an address":       {first: []string{"socket:0.0.0.0:3128"}, second: []string{"socket:192.0.2.2:3128"}, sockets: [2]string{"0.0.0.0:3128", "192.0.2.2:3128"}},
		"an address beside an IPv6 wildcard": {first: []string{"socket:192.0.2.2:8000"}, second: []string{"socket::::8000"}, sockets: [2]string{"192.0.2.2:8000", "[::]:8000"}},
		"one IPv6 socket":                    {first: []string{"socket:fd00::1:8000"}, second: []string{"socket:fd00::1:8000"}, sockets: [2]string{"[fd00::1]:8000", "[fd00::1]:8000"}},
		"another address at one port":        {first: []string{"socket:192.0.2.1:3128"}, second: []string{"socket:192.0.2.2:3128"}},
		"a wildcard at another port":         {first: []string{"socket:0.0.0.0:3128"}, second: []string{"socket:192.0.2.2:3129"}},
		"one path":                           {first: []string{"path:/srv/tree"}, second: []string{"path:/srv/tree"}},
		"a shared claim":                     {first: []string{"socket:0.0.0.0:3128"}, second: []string{"socket:192.0.2.2:3128"}, shared: true},
	} {
		t.Run(name, func(t *testing.T) {
			reservations := []HostReservation{
				{Context: "lab", Kind: "proxy", Service: "a", Keys: test.first},
				{Context: "lab", Kind: "artifact-server", Service: "b", Keys: test.second, Shared: test.shared},
			}
			conflict, found := ConflictingSockets(reservations)
			if found != (test.sockets[0] != "") {
				t.Fatalf("conflict = %t (%+v), want %t", found, conflict, test.sockets[0] != "")
			}
			if found && (conflict.First.Service != "a" || conflict.Second.Service != "b" || [2]string{conflict.FirstSocket, conflict.SecondSocket} != test.sockets) {
				t.Fatalf("conflict = %+v, want a at %s and b at %s", conflict, test.sockets[0], test.sockets[1])
			}
		})
	}
	wildcard := []HostReservation{{Context: "lab", Kind: "proxy", Service: "a", Keys: []string{"socket:0.0.0.0:3128", "socket:192.0.2.1:3128"}}}
	if conflict, found := ConflictingSockets(wildcard); found {
		t.Fatalf("a wildcard bind conflicts with its own endpoint socket: %+v", conflict)
	}
}

func TestAConflictNamesTheFirstHolderInStoredOrder(t *testing.T) {
	held := []HostReservation{
		{Context: "alpha", Kind: "proxy", Service: "a", Keys: []string{"socket:192.0.2.1:3128"}},
		{Context: "bravo", Kind: "proxy", Service: "b", Keys: []string{"socket:192.0.2.2:3128"}},
	}
	wanted := []HostReservation{{Context: "lab", Kind: "proxy", Service: "c", Keys: []string{"socket:0.0.0.0:3128"}}}
	conflicts := Conflicts(held, wanted)
	if len(conflicts) != 2 || conflicts[0].Held.Context != "alpha" || conflicts[1].Held.Context != "bravo" {
		t.Fatalf("conflicts = %+v, want alpha then bravo", conflicts)
	}
}
