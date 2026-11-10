package libvirt

import (
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/substrate"
)

func TestTheRoutingRequestFreezesEveryDeclaredPairAndNoOther(t *testing.T) {
	isolated := legNetwork("isolated", "203.0.113.193/26", "160", text("forward", "none"), field("routedNetworkRefs", api.StringList("lab-guests", "missing")))
	request := taggedHostRequest(t, append(taggedObjects(), isolated)...)
	routing := request.Routing
	if routing == nil || routing.Unit != "bootwright-lab-lab-libvirt-routing.service" || RoutingTable(*routing) != "bootwright-lab-lab-libvirt-routing" {
		t.Fatalf("routing = %+v", routing)
	}
	want := []RoutedPair{{From: "br-trunk.140", To: "br-trunk.150"}, {From: "br-trunk.150", To: "br-trunk.140"}}
	if !slices.Equal(routing.Routed, want) || !routing.Forwarding {
		t.Fatalf("routed = %+v, forwarding %v", routing.Routed, routing.Forwarding)
	}
	if !slices.Equal(routing.NATPrefixes, []string{"203.0.113.64/26"}) {
		t.Fatalf("nat = %v", routing.NATPrefixes)
	}
	if !slices.Equal(routing.KeptPrefixes, []string{"198.51.100.0/24", "203.0.113.0/26", "203.0.113.64/26", "203.0.113.128/26", "203.0.113.192/26"}) {
		t.Fatalf("kept = %v", routing.KeptPrefixes)
	}
	if !slices.Equal(routing.ManagedDevices, []string{"br-trunk", "br-trunk.140", "br-trunk.150", "br-trunk.160", "virbr-lab"}) {
		t.Fatalf("managed = %v", routing.ManagedDevices)
	}
	quiet := taggedHostRequest(t, controller(), provider(), legNetwork("isolated", "203.0.113.193/26", "160", text("forward", "none")))
	if quiet.Routing == nil || quiet.Routing.Forwarding || len(quiet.Routing.Routed) != 0 || len(quiet.Routing.NATPrefixes) != 0 {
		t.Fatalf("an isolated leg = %+v", quiet.Routing)
	}
	if plain := hostRequest(t); plain.Routing != nil {
		t.Fatalf("a provider without a leg froze routing %+v", plain.Routing)
	}
	if impacts := hostImpacts(reconciliation.Apply, request); !slices.Contains(impacts, "create-nft-table inet bootwright-lab-lab-libvirt-routing") {
		t.Fatalf("impacts = %v", impacts)
	}
}

func TestTheHostReservationClaimsTheTrunkLegsAndRoutingUnit(t *testing.T) {
	keys := taggedHostRequest(t, taggedObjects()...).ReservationKeys()
	for _, key := range []string{"bridge:br-trunk", "bridge:br-trunk.140", "bridge:br-trunk.150", "prefix:203.0.113.64/26", "prefix:203.0.113.128/26",
		"unit:bootwright-lab-lab-libvirt-routing", "unit:bootwright-lab-lab-libvirt-vlan-refusal", "unit:bootwright-lab-lab-libvirt-firewalld-ready"} {
		if !slices.Contains(keys, key) {
			t.Errorf("the reservation lacks %s: %v", key, keys)
		}
	}
	if slices.Contains(keys, "libvirt-network:bootwright-lab-ceph-public") {
		t.Errorf("a leg claimed a libvirt network: %v", keys)
	}
}

func TestTheManagedNetworkBoundWithATrunkKeepsTheHostReservationWithinItsKeys(t *testing.T) {
	objects := managedNetworks(substrate.MaxManagedNetworks - 2)
	objects = append(objects, legNetwork("leg", "10.250.0.1/24", "140"))
	request := taggedHostRequest(t, objects...)
	keys := request.ReservationKeys()
	if want := 4 + 1 + 3*(substrate.MaxManagedNetworks-2) + 2 + 1; len(keys) != want || len(keys) > prerequisites.MaxReservationKeys-2 {
		t.Fatalf("%d keys, want %d: %v", len(keys), want, keys)
	}
	if issues := substrate.Validate(objects[len(objects)-1], catalogOf(objects...)); len(issues) != 0 {
		t.Fatalf("the bound refused %d Networks and a trunk: %v", substrate.MaxManagedNetworks-1, issues)
	}
}

func TestTheRoutingTableIsProvedOwnedLoadedAndForwarding(t *testing.T) {
	request := taggedHostRequest(t, taggedObjects()...)
	for name, change := range map[string]func(*HostEvidence){
		"an unanswered read":   func(e *HostEvidence) { e.Routing.Answered = false },
		"another unit":         func(e *HostEvidence) { e.Routing.Name = "bootwright-lab-other-routing.service" },
		"a foreign unit":       func(e *HostEvidence) { e.Routing.Owned = false },
		"a disabled unit":      func(e *HostEvidence) { e.Routing.Enabled = false },
		"a failed unit":        func(e *HostEvidence) { e.Routing.State = "failed" },
		"no table":             func(e *HostEvidence) { e.Routing.Table = "" },
		"a drifted table":      func(e *HostEvidence) { e.Routing.Loaded = false },
		"forwarding off":       func(e *HostEvidence) { e.Routing.Forwarding = false },
		"an undeclared absent": func(e *HostEvidence) { e.Routing = RoutingEvidence{} },
	} {
		if err := ValidateHostPresence(evidenceOf(t, request, change), request, evidenceDigest); err == nil {
			t.Errorf("presence accepted %s", name)
		}
	}
	plain := hostRequest(t)
	stray := evidenceOf(t, plain, func(e *HostEvidence) {
		e.Routing = RoutingEvidence{Answered: true, Table: "bootwright-lab-lab-libvirt-routing"}
	})
	if err := ValidateHostPresence(stray, plain, evidenceDigest); err == nil {
		t.Error("presence accepted a table the request does not freeze")
	}
	failed := evidenceOf(t, request, func(e *HostEvidence) {
		e.Postcondition, e.Routing.State, e.Routing.Table, e.Routing.Loaded = false, "failed", "", false
	})
	if err := ValidateHostUnremoved(failed, request, evidenceDigest); err != nil {
		t.Fatalf("an owned, enabled routing unit that failed read as a started removal: %v", err)
	}
	for name, routing := range map[string]RoutingEvidence{
		"a remaining table": {Answered: true, Name: request.Routing.Unit, Table: RoutingTable(*request.Routing)},
		"a remaining unit":  {Answered: true, Name: request.Routing.Unit, Present: true, Owned: true},
		"an unanswered":     {Name: request.Routing.Unit},
	} {
		if err := routingAbsent(routing); err == nil {
			t.Errorf("absence accepted %s", name)
		}
	}
	if err := routingAbsent(RoutingEvidence{Answered: true, Name: request.Routing.Unit}); err != nil {
		t.Errorf("absence refused a removed unit and table: %v", err)
	}
	if _, err := routingOwned(RoutingEvidence{Answered: true, Name: request.Routing.Unit, Table: RoutingTable(*request.Routing)}); err == nil {
		t.Error("a table of the routing name with no owned unit read as this provider's")
	}
}
