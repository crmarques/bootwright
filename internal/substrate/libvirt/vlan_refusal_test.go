package libvirt

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func vlanNetwork(name, vlan string, cidrs ...api.FieldValue) api.Object {
	spec := api.MapValue(field("provided", api.BoolValue(true)), number("vlan", vlan))
	for _, cidr := range cidrs {
		spec = spec.With(cidr.Name, cidr.Value)
	}
	return api.NewObject(api.Network, name, api.Value{}, spec)
}

func family(name, cidr string) api.FieldValue {
	return field(name, api.MapValue(text("cidr", cidr)))
}

func carrying(network api.Object, refs ...string) api.Object {
	values := make([]api.Value, 0, len(refs))
	for _, ref := range refs {
		values = append(values, api.StringValue(ref))
	}
	arm := network.Spec().Get("libvirt").With("taggedNetworkRefs", api.ListValue(values...))
	return network.WithSpec(network.Spec().With("libvirt", arm))
}

func refusalRequest(t *testing.T, objects ...api.Object) HostRequest {
	t.Helper()
	requests, err := HostRequests(api.NewCatalog(objects), "controller", testContext)
	if err != nil || len(requests) != 1 {
		t.Fatalf("requests = %+v (%v)", requests, err)
	}
	return requests[0]
}

func TestAHostRefusesTheSubnetOfEachTaggedNetworkItHoldsNoAddressIn(t *testing.T) {
	request := refusalRequest(t, controller(), provider(), labNetwork(), vlanNetwork("ceph-cluster", "120", family("ipv4", "203.0.113.0/26")),
		carrying(managedNetwork("ceph-public", "virbr-ceph", "192.0.2.65/26"), "ceph-cluster"))
	if !slices.Equal(request.RefusedPrefixes, []string{"203.0.113.0/26"}) || request.VLANRefusalUnit != "bootwright-lab-lab-libvirt-vlan-refusal.service" {
		t.Fatalf("refusal = %v in %q", request.RefusedPrefixes, request.VLANRefusalUnit)
	}
	if keys := request.ReservationKeys(); !slices.Contains(keys, "unit:bootwright-lab-lab-libvirt-vlan-refusal") {
		t.Fatalf("keys = %v", keys)
	}
}

func TestBothFamiliesAreRefusedInCanonicalOrderOncePerSubnet(t *testing.T) {
	request := refusalRequest(t, controller(), provider(), labNetwork(),
		vlanNetwork("a", "120", family("ipv6", "2001:db8:120::/64"), family("ipv4", "203.0.113.64/26")),
		vlanNetwork("b", "121", family("ipv4", "203.0.113.0/26")),
		carrying(managedNetwork("one", "virbr-one", "192.0.2.65/26"), "a", "b"),
		carrying(managedNetwork("two", "virbr-two", "192.0.2.129/26"), "b", "a"))
	want := []string{"203.0.113.0/26", "203.0.113.64/26", "2001:db8:120::/64"}
	if !slices.Equal(request.RefusedPrefixes, want) {
		t.Fatalf("refused = %v, want %v", request.RefusedPrefixes, want)
	}
}

func TestATaggedNetworkTheHostHoldsAnAddressInIsLeftRouted(t *testing.T) {
	routed := managedNetwork("routed", "virbr-routed", "198.51.100.1/26")
	host := controller()
	host = host.WithSpec(host.Spec().With("network", api.MapValue(field("addresses", api.ListValue(
		api.MapValue(text("name", "vlan"), text("address", "203.0.113.5")))))))
	for name, objects := range map[string][]api.Object{
		"a managed Network of a provider on the host": {controller(), provider(), labNetwork(), routed,
			carrying(managedNetwork("carrier", "virbr-carrier", "192.0.2.65/26"), "routed")},
		"an address the host Machine declares": {host, provider(), labNetwork(), vlanNetwork("vlan", "120", family("ipv4", "203.0.113.0/26")),
			carrying(managedNetwork("carrier", "virbr-carrier", "192.0.2.65/26"), "vlan")},
	} {
		t.Run(name, func(t *testing.T) {
			if request := refusalRequest(t, objects...); len(request.RefusedPrefixes) != 0 {
				t.Fatalf("refused = %v", request.RefusedPrefixes)
			}
		})
	}
}

func TestAProviderWithManagedNetworksAlwaysNamesTheUnitThatRemovesAnEarlierRefusal(t *testing.T) {
	request := refusalRequest(t, controller(), provider(), labNetwork())
	if len(request.RefusedPrefixes) != 0 || request.VLANRefusalUnit == "" {
		t.Fatalf("refusal = %v in %q", request.RefusedPrefixes, request.VLANRefusalUnit)
	}
	external := refusalRequest(t, controller(), provider(field("segments", api.ListValue(api.MapValue(
		text("name", "lab-guests"), field("libvirt", api.MapValue(text("bridge", "br-lab"))))))))
	if external.VLANRefusalUnit != "" || external.RefusedPrefixes != nil {
		t.Fatalf("an external-only provider names %q %v", external.VLANRefusalUnit, external.RefusedPrefixes)
	}
}

func refusingEvidence(t *testing.T, request HostRequest, damage func(*HostEvidence)) error {
	t.Helper()
	var evidence HostEvidence
	if err := json.Unmarshal(hostEvidence(request, "digest"), &evidence); err != nil {
		t.Fatal(err)
	}
	evidence.VLANRefusal = VLANRefusalEvidence{Answered: true, Enabled: true, Name: request.VLANRefusalUnit, Owned: true, Present: true,
		Routes: slices.Clone(request.RefusedPrefixes), State: "active"}
	damage(&evidence)
	return ValidateHostPresence(encode(t, evidence), request, "digest")
}

func TestHostPresenceRequiresTheOwnedUnitAndEveryRefusingRoute(t *testing.T) {
	request := refusalRequest(t, controller(), provider(), labNetwork(), vlanNetwork("ceph-cluster", "120", family("ipv4", "203.0.113.0/26")),
		carrying(managedNetwork("ceph-public", "virbr-ceph", "192.0.2.65/26"), "ceph-cluster"))
	if err := refusingEvidence(t, request, func(*HostEvidence) {}); err != nil {
		t.Fatalf("a held refusal was refused: %v", err)
	}
	for name, damage := range map[string]func(*VLANRefusalEvidence){
		"unread":      func(e *VLANRefusalEvidence) { e.Answered = false },
		"missing":     func(e *VLANRefusalEvidence) { e.Present = false },
		"foreign":     func(e *VLANRefusalEvidence) { e.Owned = false },
		"disabled":    func(e *VLANRefusalEvidence) { e.Enabled = false },
		"stopped":     func(e *VLANRefusalEvidence) { e.State = "inactive" },
		"route gone":  func(e *VLANRefusalEvidence) { e.Routes = nil },
		"extra route": func(e *VLANRefusalEvidence) { e.Routes = append(e.Routes, "198.18.0.0/24") },
		"unreported":  func(e *VLANRefusalEvidence) { *e = VLANRefusalEvidence{} },
	} {
		t.Run(name, func(t *testing.T) {
			if err := refusingEvidence(t, request, func(e *HostEvidence) { damage(&e.VLANRefusal) }); err == nil {
				t.Fatal("evidence that does not hold the refusal was accepted")
			}
		})
	}
}

func TestAStoppedRefusalLeavesNoUnitOrRouteBehind(t *testing.T) {
	request := refusalRequest(t, controller(), provider(), labNetwork())
	if err := refusingEvidence(t, request, func(e *HostEvidence) {
		e.VLANRefusal = VLANRefusalEvidence{Answered: true, Name: request.VLANRefusalUnit}
	}); err != nil {
		t.Fatalf("a provider that refuses nothing was refused: %v", err)
	}
	for name, leftover := range map[string]VLANRefusalEvidence{
		"unit":  {Answered: true, Name: request.VLANRefusalUnit, Present: true, Owned: true, Enabled: true, State: "active"},
		"route": {Answered: true, Name: request.VLANRefusalUnit, Routes: []string{"203.0.113.0/26"}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := refusingEvidence(t, request, func(e *HostEvidence) { e.VLANRefusal = leftover }); err == nil {
				t.Fatal("a refusal the request no longer declares was accepted")
			}
		})
	}
}

func TestRemovalProvesTheUnitAndRoutesGone(t *testing.T) {
	request := refusalRequest(t, controller(), provider(), labNetwork(), vlanNetwork("ceph-cluster", "120", family("ipv4", "203.0.113.0/26")),
		carrying(managedNetwork("ceph-public", "virbr-ceph", "192.0.2.65/26"), "ceph-cluster"))
	gone := HostEvidence{Absent: true, ConnectionsAnswered: true, Directory: observed(false), PoolAnswered: true, Postcondition: true, Request: "digest", URI: true,
		FirewallReady: FirewallReadyEvidence{Name: request.FirewallReadyUnit, Answered: true},
		VLANRefusal:   VLANRefusalEvidence{Name: request.VLANRefusalUnit, Answered: true}}
	if err := ValidateHostAbsence(encode(t, gone), "digest"); err != nil {
		t.Fatalf("positive absence refused: %v", err)
	}
	for name, damage := range map[string]func(*VLANRefusalEvidence){
		"unread": func(e *VLANRefusalEvidence) { e.Answered = false },
		"unit":   func(e *VLANRefusalEvidence) { e.Present, e.Owned = true, true },
		"link":   func(e *VLANRefusalEvidence) { e.Enabled = true },
		"route":  func(e *VLANRefusalEvidence) { e.Routes = []string{"203.0.113.0/26"} },
		"state":  func(e *VLANRefusalEvidence) { e.State = "active" },
	} {
		t.Run(name, func(t *testing.T) {
			left := gone
			damage(&left.VLANRefusal)
			if err := ValidateHostAbsence(encode(t, left), "digest"); err == nil {
				t.Fatal("a removal that left the refusal behind was proved")
			}
		})
	}
}

func TestAProviderRefusesMoreSubnetsThanOneHostHoldsAtPlanTime(t *testing.T) {
	for _, count := range []int{MaxRefusedPrefixes / 2, MaxRefusedPrefixes/2 + 1} {
		objects := []api.Object{controller(), provider(), labNetwork()}
		var refs []string
		for index := range count {
			name := fmt.Sprintf("vlan-%02d", index)
			refs = append(refs, name)
			objects = append(objects, vlanNetwork(name, fmt.Sprint(100+index),
				family("ipv4", fmt.Sprintf("203.0.%d.0/24", index)), family("ipv6", fmt.Sprintf("2001:db8:%x::/64", index))))
		}
		objects = append(objects, carrying(managedNetwork("carrier", "virbr-carrier", "192.0.2.65/26"), refs...))
		requests, err := HostRequests(api.NewCatalog(objects), "controller", testContext)
		if 2*count <= MaxRefusedPrefixes {
			if err != nil || len(requests) != 1 || len(requests[0].RefusedPrefixes) != MaxRefusedPrefixes {
				t.Fatalf("%d subnets: %+v (%v)", 2*count, requests, err)
			}
			continue
		}
		found := diagnostics.Of(err)
		if len(found) != 1 || found[0].Code != "api.invariant" || found[0].Field != "$.spec.libvirt.taggedNetworkRefs" ||
			found[0].Object == nil || found[0].Object.Name != "lab-libvirt" || !strings.Contains(found[0].Message, "66 subnets") || found[0].Remediation == "" {
			t.Fatalf("%d subnets: %+v", 2*count, found)
		}
	}
}

func TestRefusingRoutesLeftWithoutTheirUnitAreAnUnfinishedRemoval(t *testing.T) {
	request := refusalRequest(t, controller(), provider(), labNetwork(), vlanNetwork("ceph-cluster", "120", family("ipv4", "203.0.113.0/26")),
		carrying(managedNetwork("ceph-public", "virbr-ceph", "192.0.2.65/26"), "ceph-cluster"))
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	residual := HostEvidence{Directory: observed(false), PoolAnswered: true, Request: "digest", URI: true,
		FirewallReady: FirewallReadyEvidence{Name: request.FirewallReadyUnit, Answered: true},
		VLANRefusal:   VLANRefusalEvidence{Name: request.VLANRefusalUnit, Answered: true, Routes: []string{"203.0.113.0/26"}}}
	if err := ValidateHostRemovalUnfinished(encode(t, residual), request, "digest"); err != nil {
		t.Fatalf("held routes without their unit were not an unfinished removal: %v", err)
	}
	if err := ValidateHostPartial(encode(t, residual), "digest"); err != nil {
		t.Fatalf("held routes without their unit were not a partial realization: %v", err)
	}
	execution := lifecycle.Execution{Block: reconciliation.Block{
		BlockDefinition: reconciliation.BlockDefinition{ID: request.Identity.Block, Request: canonical}, RequestDigest: "digest",
	}}
	for _, verb := range []reconciliation.Verb{reconciliation.Apply, reconciliation.Destroy} {
		capability := NewHost(&fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: encode(t, residual)}})
		observe := capability.Observe
		if verb == reconciliation.Destroy {
			observe = capability.ObserveRemoval
		}
		observation, err := observe(context.Background(), execution)
		if err != nil || observation.Effect != reconciliation.EffectPartial {
			t.Fatalf("%s over held routes without their unit = %+v, %v", verb, observation, err)
		}
	}
	for name, damage := range map[string]func(*VLANRefusalEvidence){
		"unread":       func(e *VLANRefusalEvidence) { e.Answered = false },
		"foreign unit": func(e *VLANRefusalEvidence) { e.Present = true },
		"no route":     func(e *VLANRefusalEvidence) { e.Routes = nil },
	} {
		t.Run(name, func(t *testing.T) {
			left := residual
			damage(&left.VLANRefusal)
			if err := ValidateHostRemovalUnfinished(encode(t, left), request, "digest"); err == nil {
				t.Fatal("evidence that proves no owned refusing route was accepted as an unfinished removal")
			}
		})
	}
}
