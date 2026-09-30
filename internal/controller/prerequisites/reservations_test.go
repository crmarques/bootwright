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
			owner, conflict := ConflictingContext(held, wanted)
			if conflict != test.conflict || conflict && owner != "other" {
				t.Fatalf("conflict = %t naming %q, want %t", conflict, owner, test.conflict)
			}
		})
	}
}

func TestAConflictNamesTheFirstHolderInStoredOrder(t *testing.T) {
	held := []HostReservation{
		{Context: "alpha", Kind: "proxy", Service: "a", Keys: []string{"socket:192.0.2.1:3128"}},
		{Context: "bravo", Kind: "proxy", Service: "b", Keys: []string{"socket:192.0.2.2:3128"}},
	}
	wanted := []HostReservation{{Context: "lab", Kind: "proxy", Service: "c", Keys: []string{"socket:0.0.0.0:3128"}}}
	if owner, conflict := ConflictingContext(held, wanted); !conflict || owner != "alpha" {
		t.Fatalf("conflict = %t naming %q, want alpha", conflict, owner)
	}
}
