package libvirt

import (
	"encoding/json"
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/substrate"
)

func legNetwork(name, address, vlan string, fields ...api.FieldValue) api.Object {
	o := managedNetwork(name, "br-trunk", address)
	spec := o.Spec().With("vlan", api.IntegerValue(vlan))
	for _, extra := range fields {
		spec = spec.With("libvirt", spec.Get("libvirt").With(extra.Name, extra.Value))
	}
	return api.NewObject(api.Network, name, api.Value{}, spec)
}

func taggedObjects() []api.Object {
	cluster := api.NewObject(api.Network, "ceph-cluster", api.Value{}, api.MapValue(field("provided", api.BoolValue(true)), number("vlan", "120"),
		field("ipv4", api.MapValue(text("cidr", "203.0.113.0/26")))))
	public := legNetwork("ceph-public", "203.0.113.65/26", "140", field("taggedNetworkRefs", api.StringList("ceph-cluster")))
	arbiter := legNetwork("ceph-arbiter", "203.0.113.129/26", "150", text("forward", "none"))
	g := guest("ceph-01")
	g = g.WithSpec(g.Spec().WithPath(api.ListValue(api.MapValue(text("nicRef", "enp1s0"), text("segmentRef", "ceph-public"))), "substrate", "libvirt", "nics"))
	return []api.Object{controller(), provider(), networkConfig(), labNetwork(), cluster, public, arbiter, g}
}

func taggedHostRequest(t *testing.T, objects ...api.Object) HostRequest {
	t.Helper()
	requests, err := HostRequests(catalogOf(objects...), "controller", testContext)
	if err != nil || len(requests) != 1 {
		t.Fatalf("requests = %+v (%v)", requests, diagnostics.Of(err))
	}
	return requests[0]
}

func TestATaggedManagedNetworkFreezesALegAndItsTrunk(t *testing.T) {
	request := taggedHostRequest(t, taggedObjects()...)
	trunk := substrate.TrunkConnectionName(testContext, "lab-libvirt", "br-trunk")
	wantTrunks := []Trunk{{Bridge: "br-trunk", MAC: substrate.TrunkMAC(testContext, "lab-libvirt", "br-trunk"), Name: trunk, UUID: substrate.ConnectionUUID(testContext, trunk)}}
	if !slices.Equal(request.Trunks, wantTrunks) || trunk != "bootwright-lab-lab-libvirt-trunk-br-trunk" {
		t.Fatalf("trunks = %+v", request.Trunks)
	}
	wantLegs := []Leg{
		{Address: "203.0.113.129/26", Device: "br-trunk.150", Forward: "none", Name: "bootwright-lab-ceph-arbiter", Network: "ceph-arbiter", Trunk: "br-trunk",
			UUID: substrate.ConnectionUUID(testContext, "bootwright-lab-ceph-arbiter"), VLAN: 150},
		{Address: "203.0.113.65/26", Device: "br-trunk.140", Forward: "nat", Name: "bootwright-lab-ceph-public", Network: "ceph-public", Trunk: "br-trunk",
			UUID: substrate.ConnectionUUID(testContext, "bootwright-lab-ceph-public"), VLAN: 140},
	}
	if !slices.Equal(request.Legs, wantLegs) {
		t.Fatalf("legs = %+v", request.Legs)
	}
	if len(request.Networks) != 1 || request.Networks[0].Name != "bootwright-lab-lab-guests" {
		t.Fatalf("a leg was frozen as a libvirt network: %+v", request.Networks)
	}
	if !slices.Equal(request.RefusedPrefixes, []string{"203.0.113.0/26"}) {
		t.Fatalf("refused = %v", request.RefusedPrefixes)
	}
	alone := taggedHostRequest(t, controller(), provider(), legNetwork("ceph-public", "203.0.113.65/26", "140"))
	if alone.FirewallReadyUnit == "" || alone.VLANRefusalUnit == "" || len(alone.Networks) != 0 {
		t.Fatalf("a provider with only a leg = %+v", alone)
	}
	if impacts := hostImpacts(reconciliation.Apply, request); !slices.Contains(impacts, "create-bridge br-trunk") || !slices.Contains(impacts, "create-vlan-link br-trunk.140") {
		t.Fatalf("impacts = %v", impacts)
	}
	if impacts := hostImpacts(reconciliation.Destroy, request); !slices.Contains(impacts, "remove-bridge br-trunk") || !slices.Contains(impacts, "remove-vlan-link br-trunk.150") {
		t.Fatalf("impacts = %v", impacts)
	}
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	matchesGolden(t, "request-host-tagged", canonical)
	if decoded, err := DecodeHostRequest(canonical); err != nil || !slices.Equal(decoded.Legs, request.Legs) {
		t.Fatalf("decoded = %+v (%v)", decoded, diagnostics.Of(err))
	}
}

func TestAMachineNICOnALegSegmentAttachesToTheTrunkBridge(t *testing.T) {
	requests, err := MachineRequests(catalogOf(taggedObjects()...), "controller", testContext)
	if err != nil || len(requests) != 1 {
		t.Fatalf("requests = %+v (%v)", requests, diagnostics.Of(err))
	}
	if got := requests[0].Interfaces; len(got) != 1 || got[0].Bridge != "br-trunk" || got[0].Network != "" || got[0].MTU != 0 {
		t.Fatalf("interfaces = %+v", got)
	}
}

func evidenceOf(t *testing.T, request HostRequest, change func(*HostEvidence)) []byte {
	t.Helper()
	var evidence HostEvidence
	if err := json.Unmarshal(hostEvidence(request, evidenceDigest), &evidence); err != nil {
		t.Fatal(err)
	}
	change(&evidence)
	data, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestHostPresenceRequiresEveryOwnedConnection(t *testing.T) {
	request := taggedHostRequest(t, taggedObjects()...)
	if err := ValidateHostPresence(evidenceOf(t, request, func(*HostEvidence) {}), request, evidenceDigest); err != nil {
		t.Fatalf("complete evidence was refused: %v", diagnostics.Of(err))
	}
	for name, change := range map[string]func(*HostEvidence){
		"no NetworkManager":    func(e *HostEvidence) { e.NetworkManager = false },
		"a missing connection": func(e *HostEvidence) { e.Connections = e.Connections[1:] },
		"an undeclared one": func(e *HostEvidence) {
			e.Connections = append(e.Connections, ConnectionEvidence{Name: "bootwright-lab-other", Role: roleLeg})
		},
		"another device":          func(e *HostEvidence) { e.Connections[0].Device = "br-trunk.99" },
		"another role":            func(e *HostEvidence) { e.Connections[0].Role = roleTrunk },
		"an unanswered read":      func(e *HostEvidence) { e.Connections[0].Answered = false },
		"a foreign profile":       func(e *HostEvidence) { e.Connections[1].Owned = false },
		"a drifted definition":    func(e *HostEvidence) { e.Connections[2].Definition = false },
		"an inactive leg":         func(e *HostEvidence) { e.Connections[0].Active = false },
		"another zone":            func(e *HostEvidence) { e.Connections[2].Zone = "public" },
		"a connection never made": func(e *HostEvidence) { e.Connections[1].Present = false },
	} {
		if err := ValidateHostPresence(evidenceOf(t, request, change), request, evidenceDigest); err == nil {
			t.Errorf("presence accepted %s", name)
		}
	}
	plain := hostRequest(t)
	extra := evidenceOf(t, plain, func(e *HostEvidence) {
		e.Connections = []ConnectionEvidence{{Answered: true, Name: "bootwright-lab-ceph-public", Role: roleLeg, Device: "br-trunk.140"}}
	})
	if err := ValidateHostPresence(extra, plain, evidenceDigest); err == nil {
		t.Error("presence accepted a connection the request does not freeze")
	}
}

func TestAnOwnedConnectionAloneIsAPartialRealization(t *testing.T) {
	request := taggedHostRequest(t, taggedObjects()...)
	trunkOnly := func(e *HostEvidence) {
		e.Postcondition, e.Pool, e.PoolOwned, e.Directory = false, "", false, observed(false)
		e.FirewallReady = FirewallReadyEvidence{Name: request.FirewallReadyUnit, Answered: true}
		e.VLANRefusal = VLANRefusalEvidence{Name: request.VLANRefusalUnit, Answered: true}
		e.Networks = []NetworkEvidence{{Answered: true, Managed: true, Name: request.Networks[0].Name}}
		for index := range e.Connections {
			if e.Connections[index].Role == roleLeg {
				e.Connections[index] = ConnectionEvidence{Answered: true, Name: e.Connections[index].Name, Role: roleLeg, Device: e.Connections[index].Device}
			}
		}
	}
	data := evidenceOf(t, request, trunkOnly)
	if err := ValidateHostPartial(data, evidenceDigest); err != nil {
		t.Fatalf("an owned trunk alone was not a partial realization: %v", diagnostics.Of(err))
	}
	if err := ValidateHostRemovalUnfinished(data, request, evidenceDigest); err != nil {
		t.Fatalf("an owned trunk alone was not an unfinished removal: %v", diagnostics.Of(err))
	}
	foreign := evidenceOf(t, request, func(e *HostEvidence) {
		trunkOnly(e)
		e.Connections[2].Owned = false
	})
	if err := ValidateHostPartial(foreign, evidenceDigest); err == nil {
		t.Fatal("a foreign profile of the trunk's name read as this context's partial realization")
	}
	if err := ValidateHostUnremoved(evidenceOf(t, request, func(e *HostEvidence) { e.Postcondition = false }), request, evidenceDigest); err != nil {
		t.Fatalf("a host holding every connection read as a started removal: %v", diagnostics.Of(err))
	}
	gone := evidenceOf(t, request, func(e *HostEvidence) {
		trunkOnly(e)
		e.Absent, e.Postcondition, e.Networks = true, true, []NetworkEvidence{{Answered: true, Managed: true, Name: request.Networks[0].Name}}
		for index := range e.Connections {
			e.Connections[index] = ConnectionEvidence{Answered: true, Name: e.Connections[index].Name, Role: e.Connections[index].Role, Device: e.Connections[index].Device}
		}
	})
	if err := ValidateHostAbsence(gone, evidenceDigest); err != nil {
		t.Fatalf("removal evidence was refused: %v", diagnostics.Of(err))
	}
	for name, change := range map[string]func(*ConnectionEvidence){
		"an unanswered read":  func(c *ConnectionEvidence) { c.Answered = false },
		"a remaining profile": func(c *ConnectionEvidence) { c.Present = true },
		"an active device":    func(c *ConnectionEvidence) { c.Active = true },
	} {
		var evidence HostEvidence
		if err := json.Unmarshal(gone, &evidence); err != nil {
			t.Fatal(err)
		}
		change(&evidence.Connections[1])
		data, _ := json.Marshal(evidence)
		if err := ValidateHostAbsence(data, evidenceDigest); err == nil {
			t.Errorf("absence accepted %s", name)
		}
	}
}

func TestTaggedHostEvidenceMatchesItsGolden(t *testing.T) {
	request := taggedHostRequest(t, taggedObjects()...)
	var evidence HostEvidence
	if err := json.Unmarshal(hostEvidence(request, evidenceDigest), &evidence); err != nil {
		t.Fatal(err)
	}
	canonical, err := reconciliation.Freeze(evidence, "provider host evidence")
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	matchesGolden(t, "host-evidence-tagged-completed", canonical)
	if err := ValidateHostPresence(canonical, request, evidenceDigest); err != nil {
		t.Fatalf("presence refused its golden: %v", diagnostics.Of(err))
	}
}
