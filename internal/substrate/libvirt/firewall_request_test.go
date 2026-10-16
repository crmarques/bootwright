package libvirt

import (
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

func TestHostFirewallRequestAndDigestMatchGoldens(t *testing.T) {
	requests, err := HostRequests(labCatalog(), "controller", testContext)
	if err != nil || len(requests) != 1 {
		t.Fatalf("requests = %v (%v)", requests, err)
	}
	request := requests[0]
	if request.FirewallReadyUnit != "bootwright-lab-lab-libvirt-firewalld-ready.service" {
		t.Fatalf("readiness unit = %q", request.FirewallReadyUnit)
	}
	data, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "request-host-firewall-ready", data)
	if _, err := DecodeHostRequest(data); err != nil {
		t.Fatal(err)
	}
	digest, err := reconciliation.Freeze(map[string]string{"digest": HostContentDigest()}, "host content digest")
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "host-content-digest", digest)
}

func TestFirewallBarrierSharesTheControllerUnitReservationNamespace(t *testing.T) {
	provider := api.NewObject(api.InfraProvider, "bmc-alpha", api.Value{}, provider().Spec())
	guest := guest("alpha-firewalld-ready")
	guest = api.NewObject(api.Machine, guest.Name(), api.Value{}, guest.Spec().With("substrate",
		guest.Spec().Get("substrate").With("providerRef", api.StringValue(provider.Name()))))
	catalog := catalogOf(controller(), provider, networkConfig(), guest)
	hosts, err := HostRequests(catalog, "controller", testContext)
	if err != nil || len(hosts) != 1 {
		t.Fatalf("hosts = %v, %v", hosts, err)
	}
	machines, err := MachineRequests(catalog, "controller", testContext)
	if err != nil || len(machines) != 1 {
		t.Fatalf("machines = %v, %v", machines, err)
	}
	unit := machines[0].Controller.Unit
	if hosts[0].FirewallReadyUnit != unit+".service" {
		t.Fatal("the fixture no longer names the same native service")
	}
	if !slices.Contains(hosts[0].ReservationKeys(), "unit:"+unit) || !slices.Contains(machines[0].ReservationKeys(), "unit:"+unit) {
		t.Fatal("the same native service escaped the shared unit reservation")
	}
}
