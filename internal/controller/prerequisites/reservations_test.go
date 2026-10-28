package prerequisites

import (
	"strings"
	"testing"
)

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

// A shared claim conflicts with nothing at acquisition, except a shared bmc:
// key another context holds exclusively, which refuses the registration that
// carries the emulated BMC's installation; an exclusive bmc: claim over a held
// shared one, and two shared claims, conflict with nothing (B803).
func TestOnlyASharedBMCKeyConflictsWithItsExclusiveHolder(t *testing.T) {
	physical := HostReservation{Context: "metal", Kind: "substrate-physical", Service: "metal-01", Keys: []string{"bmc:192.0.2.1:8000/1"}}
	emulated := HostReservation{Context: "lab", Kind: "substrate-machine-bmc", Service: "rhel-01", Keys: []string{"bmc:192.0.2.1:8000/1"}, Shared: true}
	use := HostReservation{Context: "lab", Kind: "substrate-host-use", Service: "lab", Keys: []string{"bridge-use:virbr-lab"}, Shared: true}
	if found := Conflicts([]HostReservation{physical}, []HostReservation{emulated}); len(found) != 1 || found[0].Held.Context != "metal" {
		t.Fatalf("a shared bmc key under an exclusive holder = %+v", found)
	}
	if found := Conflicts([]HostReservation{emulated}, []HostReservation{physical}); len(found) != 0 {
		t.Fatalf("an exclusive bmc key over a shared holder = %+v", found)
	}
	other := use
	other.Context = "other"
	if found := Conflicts([]HostReservation{other, emulated}, []HostReservation{use, emulated}); len(found) != 0 {
		t.Fatalf("shared claims conflicted: %+v", found)
	}
	managed := HostReservation{Context: "other", Kind: "substrate-host", Service: "lab-b", Keys: []string{"bridge:virbr-lab"}}
	if found := Conflicts([]HostReservation{managed}, []HostReservation{use}); len(found) != 0 {
		t.Fatalf("a bridge-use claim conflicted with the managed bridge it uses: %+v", found)
	}
}

// A removal would strand another context's use of a managed bridge this
// context defines, or another context's exclusive claim of the controller
// endpoint this context's emulated BMC serves; a context's own claims, and
// another context's claims of anything else, strand nothing.
func TestAReleaseConflictsWithAnotherContextsUse(t *testing.T) {
	managed := HostReservation{Context: "lab", Kind: "substrate-host", Service: "lab", Keys: []string{"bridge:virbr-lab", "libvirt-network:bootwright-lab-lab"}}
	emulated := HostReservation{Context: "lab", Kind: "substrate-machine-bmc", Service: "rhel-01", Keys: []string{"bmc:192.0.2.1:8000/1"}, Shared: true}
	use := HostReservation{Context: "other", Kind: "substrate-host-use", Service: "edge", Keys: []string{"bridge-use:virbr-lab"}, Shared: true}
	physical := HostReservation{Context: "metal", Kind: "substrate-physical", Service: "metal-01", Keys: []string{"bmc:192.0.2.1:8000/1"}}
	held := []HostReservation{managed, emulated, use, physical}
	found := ReleaseConflicts(held, "lab")
	if len(found) != 2 || found[0].Held.Context != "other" || found[0].WantedKey != "bridge:virbr-lab" || found[1].Held.Context != "metal" {
		t.Fatalf("release conflicts = %+v", found)
	}
	diagnostic := ReleaseDiagnostic(found[0])
	if diagnostic.Code != "controller.conflict" || diagnostic.Remediation != "run bootwright destroy --context other first" ||
		diagnostic.Message != "context other still uses bridge virbr-lab that InfraProvider/lab of this context holds, through InfraProvider/edge, so this removal would take it from under that context" {
		t.Fatalf("diagnostic = %+v", diagnostic)
	}
	if message := ReleaseDiagnostic(found[1]).Message; strings.Contains(message, "192.0.2.1") {
		t.Fatalf("a bmc release named its endpoint: %s", message)
	}
	if found := ReleaseConflicts(held, "other"); len(found) != 0 {
		t.Fatalf("a removal of the user strands %+v", found)
	}
	if found := ReleaseConflicts([]HostReservation{managed, emulated}, "lab"); len(found) != 0 {
		t.Fatalf("a context's own claims strand %+v", found)
	}
}

// A releasing claim conflicts with exactly the claims its release re-proof
// would refuse over: another context's shared use of its bridge, or an
// exclusive holder of its emulated BMC endpoint. The same claims beside a
// claim that is not releasing conflict with nothing (B839).
func TestAReleasingClaimConflictsWithWhatItsReleaseGuards(t *testing.T) {
	bridge := HostReservation{Context: "other", Kind: "substrate-host", Service: "lab", Keys: []string{"bridge:virbr-lab"}}
	bmc := HostReservation{Context: "other", Kind: "substrate-machine-bmc", Service: "node-01", Keys: []string{"bmc:192.0.2.1:8000/node-01"}, Shared: true}
	for name, test := range map[string]struct {
		held     HostReservation
		wanted   HostReservation
		conflict bool
	}{
		"a bridge use":                      {held: bridge, wanted: HostReservation{Kind: "substrate-host-use", Service: "edge", Keys: []string{"bridge-use:virbr-lab"}, Shared: true}, conflict: true},
		"another bridge's use":              {held: bridge, wanted: HostReservation{Kind: "substrate-host-use", Service: "edge", Keys: []string{"bridge-use:virbr-other"}, Shared: true}},
		"an exclusive BMC holder":           {held: bmc, wanted: HostReservation{Kind: "substrate-physical", Service: "metal-01", Keys: []string{"bmc:192.0.2.1:8000/node-01"}}, conflict: true},
		"a shared BMC holder":               {held: bmc, wanted: HostReservation{Kind: "substrate-machine-bmc", Service: "node-02", Keys: []string{"bmc:192.0.2.1:8000/node-01"}, Shared: true}},
		"a bridge use on another SSH host":  {held: HostReservation{Context: "other", Kind: "substrate-host", Service: "lab", Keys: []string{"ssh/192.0.2.9:22/bridge:virbr-lab"}}, wanted: HostReservation{Kind: "substrate-host-use", Service: "edge", Keys: []string{"bridge-use:virbr-lab"}, Shared: true}},
		"a bridge use on the same SSH host": {held: HostReservation{Context: "other", Kind: "substrate-host", Service: "lab", Keys: []string{"ssh/192.0.2.9:22/bridge:virbr-lab"}}, wanted: HostReservation{Kind: "substrate-host-use", Service: "edge", Keys: []string{"ssh/192.0.2.9:22/bridge-use:virbr-lab"}, Shared: true}, conflict: true},
	} {
		t.Run(name, func(t *testing.T) {
			wanted := test.wanted
			wanted.Context = "lab"
			if conflicts := Conflicts([]HostReservation{test.held}, []HostReservation{wanted}); len(conflicts) != 0 {
				t.Fatalf("a claim that is not releasing conflicts: %+v", conflicts)
			}
			releasing := test.held
			releasing.Releasing = true
			conflicts := Conflicts([]HostReservation{releasing}, []HostReservation{wanted})
			if len(conflicts) > 0 != test.conflict || test.conflict && !conflicts[0].Releasing {
				t.Fatalf("conflicts = %+v, want %t", conflicts, test.conflict)
			}
			if test.conflict {
				diagnostic := ConflictDiagnostic(conflicts[0])
				if diagnostic.Code != "controller.conflict" || !strings.HasPrefix(diagnostic.Message, "context other is removing ") ||
					!strings.Contains(diagnostic.Message, "until that removal completes") || strings.Contains(diagnostic.Message, "192.0.2.1") {
					t.Fatalf("diagnostic = %+v", diagnostic)
				}
			}
		})
	}
}

// A removal marks the claims of its effect's object that guard another
// context's use, and only those; a whole-context removal names every one.
func TestARemovalReleasesOnlyTheGuardingClaimsOfItsObject(t *testing.T) {
	for name, test := range map[string]struct {
		reservation  HostReservation
		kind, object string
		releases     bool
	}{
		"its bridge":                {reservation: HostReservation{Kind: "substrate-host", Service: "lab", Keys: []string{"bridge:virbr-lab", "libvirt-network:lab"}}, kind: "InfraProvider", object: "lab", releases: true},
		"another provider's bridge": {reservation: HostReservation{Kind: "substrate-host", Service: "lab", Keys: []string{"bridge:virbr-lab"}}, kind: "InfraProvider", object: "edge"},
		"a key that guards no use":  {reservation: HostReservation{Kind: "substrate-host", Service: "lab", Keys: []string{"libvirt-network:lab"}}, kind: "InfraProvider", object: "lab"},
		"its shared BMC endpoint":   {reservation: HostReservation{Kind: "substrate-machine-bmc", Service: "node-01", Keys: []string{"bmc:192.0.2.1:8000/node-01"}, Shared: true}, kind: "Machine", object: "node-01", releases: true},
		"an exclusive BMC endpoint": {reservation: HostReservation{Kind: "substrate-physical", Service: "node-01", Keys: []string{"bmc:192.0.2.1:8000/node-01"}}, kind: "Machine", object: "node-01"},
		"a bridge use":              {reservation: HostReservation{Kind: "substrate-host-use", Service: "lab", Keys: []string{"bridge-use:virbr-lab"}, Shared: true}, kind: "InfraProvider", object: "lab"},
		"a whole-context removal":   {reservation: HostReservation{Kind: "substrate-host", Service: "lab", Keys: []string{"bridge:virbr-lab"}}, releases: true},
	} {
		t.Run(name, func(t *testing.T) {
			if got := Releases(test.reservation, test.kind, test.object); got != test.releases {
				t.Fatalf("Releases = %t, want %t", got, test.releases)
			}
		})
	}
}

// A host-key qualified key reads like an address-qualified one: it conflicts
// by its class's relation with keys of its own qualifier only, guards a
// release on its own host, prints the fingerprint in OpenSSH's form, and one
// conflict under both forms is reported once (B844).
func TestAHostKeyQualifiedKeyIsOneHostAcrossSpellings(t *testing.T) {
	pinned, other := "sshkey/SHA256:ab-cd_ef/", "sshkey/SHA256:zz/"
	if host, key := unqualified(pinned + "socket:0.0.0.0:3128"); host != pinned || key != "socket:0.0.0.0:3128" {
		t.Fatalf("unqualified = %q %q", host, key)
	}
	if host, key := unqualified("sshkey/unterminated"); host != "" || key != "sshkey/unterminated" {
		t.Fatalf("an unterminated qualifier read %q %q", host, key)
	}
	if !keysConflict(pinned+"socket:0.0.0.0:3128", pinned+"socket:192.0.2.10:3128") || keysConflict(pinned+"socket:0.0.0.0:3128", other+"socket:192.0.2.10:3128") ||
		keysConflict(pinned+"socket:0.0.0.0:3128", "ssh/192.0.2.10:22/socket:192.0.2.10:3128") {
		t.Fatal("host-key qualified sockets compare across qualifiers")
	}
	if got := describeKey(pinned + "bridge:virbr-lab"); got != "bridge virbr-lab on the SSH host whose pinned host key is SHA256:ab+cd/ef" {
		t.Fatalf("describeKey = %q", got)
	}
	held := []HostReservation{
		{Context: "lab", Kind: "substrate-host", Service: "lab", Keys: []string{"ssh/192.0.2.10:22/bridge:virbr-lab", pinned + "bridge:virbr-lab"}},
		{Context: "other", Kind: "substrate-host-use", Service: "edge", Keys: []string{"ssh/kvm.example.test:22/bridge-use:virbr-lab", pinned + "bridge-use:virbr-lab"}, Shared: true},
	}
	if found := ReleaseConflicts(held, "lab"); len(found) != 1 || found[0].HeldKey != pinned+"bridge-use:virbr-lab" {
		t.Fatalf("a release across two spellings = %+v", found)
	}
	wanted := []HostReservation{{Context: "lab", Kind: "proxy", Service: "b", Keys: []string{"ssh/192.0.2.10:22/socket:192.0.2.10:3128", pinned + "socket:192.0.2.10:3128"}}}
	holder := []HostReservation{{Context: "other", Kind: "proxy", Service: "a", Keys: []string{"ssh/192.0.2.10:22/socket:0.0.0.0:3128", pinned + "socket:0.0.0.0:3128"}}}
	conflicts := Conflicts(holder, wanted)
	if len(conflicts) != 1 {
		t.Fatalf("one conflict under both forms = %+v", conflicts)
	}
	keyed := ConflictDiagnostic(Conflict{Held: holder[0], Wanted: wanted[0], HeldKey: pinned + "socket:0.0.0.0:3128", WantedKey: pinned + "socket:192.0.2.10:3128"})
	if !strings.HasSuffix(keyed.Remediation, "give each its own SSH host key, since a pinned host key names one host") {
		t.Fatalf("remediation = %q", keyed.Remediation)
	}
}
