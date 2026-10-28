package main

import (
	"context"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller/firewall"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

type firewallProbe struct {
	status  string
	sockets []firewall.Socket
}

func (f *firewallProbe) Inspect(_ context.Context, socket firewall.Socket) (firewall.Observation, error) {
	f.sockets = append(f.sockets, socket)
	return firewall.Observation{Interface: "synthetic0", Zone: "lab", Status: f.status, Reason: "synthetic zone query"}, nil
}

func TestControllerFirewallPlanPreflightAndDependentAdmission(t *testing.T) {
	state, _ := compileAcceptance(t, exampleDirectory(t, "libvirt-3-nodes-ceph"))
	catalog := state.Effective()
	sockets, err := controllerSockets(catalog, "lab")
	if err != nil || len(sockets) == 0 {
		t.Fatal(sockets, err)
	}
	var socket firewall.Socket
	for _, candidate := range sockets {
		if strings.HasPrefix(candidate.Object, "ArtifactServer/") {
			socket = candidate
			break
		}
	}
	if socket.Block == "" {
		t.Fatal("no artifact listener projected")
	}
	listener := reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: socket.Block, Stage: reconciliation.StageInfrastructureServices}}
	consumer := reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: "consumer", Dependencies: []string{listener.ID}, Stage: reconciliation.StageMachines}}
	full := reconciliation.Plan{Verb: reconciliation.Apply, Blocks: []reconciliation.Block{listener, consumer}}
	for _, status := range []string{"open", "closed", "unknown"} {
		probe := &firewallProbe{status: status}
		rows, err := firewallPreflight(probe)(context.Background(), catalog, "lab")
		if err != nil || len(rows) == 0 {
			t.Fatal(rows, err)
		}
		for _, row := range rows {
			if (row.Status == "ready") != (status == "open") {
				t.Fatal(row, status)
			}
		}
		preview, err := firewallPlanInspection(probe)(context.Background(), "lab", catalog, full, []reconciliation.Block{consumer})
		if err != nil || len(preview) != len(rows) || preview[0].Status != status {
			t.Fatal(preview, err)
		}
		err = firewallApplyAdmission(probe)(context.Background(), "lab", catalog, full, []reconciliation.Block{consumer})
		if (err == nil) != (status == "open") {
			t.Fatalf("dependent admission %s: %v", status, err)
		}
		before := len(probe.sockets)
		if err := firewallApplyAdmission(probe)(context.Background(), "lab", catalog, full, []reconciliation.Block{{BlockDefinition: reconciliation.BlockDefinition{ID: "controller", Stage: reconciliation.StageController}}}); err != nil {
			t.Fatal("controller-only stage refused", err)
		}
		if len(probe.sockets) != before {
			t.Fatal("controller-only stage probed unrelated listener sockets")
		}
	}
	for _, socket := range sockets {
		if socket.Object == string(api.DNSServer) {
			t.Fatal("listener omitted its object identity")
		}
	}
}

func TestWildcardListenerWithoutEndpointRemainsAnUnknownSocket(t *testing.T) {
	state, _ := compileAcceptance(t, exampleDirectory(t, "libvirt-3-nodes-ceph"))
	objects := state.Effective().Objects()
	for index, object := range objects {
		if object.Kind() != api.ArtifactServer || object.Spec().Get("management").Text() != "managed" {
			continue
		}
		listeners := append(object.Spec().Get("listeners").Items(), api.MapValue().With("name", api.StringValue("unaddressed")).With("protocol", api.StringValue("http")).With("port", api.IntegerValue("18080")))
		objects[index] = api.NewObject(object.Kind(), object.Name(), object.Labels(), object.Spec().With("bindAddress", api.StringValue("0.0.0.0")).With("listeners", api.ListValue(listeners...)))
		break
	}
	sockets, err := controllerSockets(api.NewCatalog(objects), "lab")
	if err != nil {
		t.Fatalf("projection error: %#v", err)
	}
	for _, socket := range sockets {
		if socket.Port == 18080 && socket.Address == "0.0.0.0" {
			return
		}
	}
	t.Fatal("wildcard listener with no endpoint disappeared from firewall inspection")
}

func TestSocketTargetNamesTheObjectAddressPortAndProtocol(t *testing.T) {
	for _, row := range []struct {
		socket firewall.Socket
		want   string
	}{
		{firewall.Socket{Object: "DNSServer/dns", Address: "192.0.2.53", Port: 53, Protocol: "udp"}, "DNSServer/dns 192.0.2.53:53/udp"},
		{firewall.Socket{Object: "DNSServer/dns", Address: "192.0.2.53", Port: 53, Protocol: "tcp"}, "DNSServer/dns 192.0.2.53:53/tcp"},
		{firewall.Socket{Object: "ArtifactServer/a", Address: "2001:db8::1", Port: 8443, Protocol: "tcp"}, "ArtifactServer/a [2001:db8::1]:8443/tcp"},
	} {
		if got := socketTarget(row.socket); got != row.want {
			t.Errorf("target %q, want %q", got, row.want)
		}
	}
}
