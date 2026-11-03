//go:build linux && amd64

package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/environment/inspection"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/trust"
)

type catalogGraph struct{ catalog api.Catalog }

func (g catalogGraph) Effective(context.Context, string) (api.Catalog, error) { return g.catalog, nil }

type recordsFake struct {
	data   []byte
	failed error
	reads  int
}

func (r *recordsFake) ReadHostKeys(context.Context, string) ([]byte, error) {
	r.reads++
	return r.data, r.failed
}

func trustRecord(t *testing.T, machineName, address string, port int) []byte {
	t.Helper()
	data, err := trust.Store{FormatVersion: trust.FormatVersion, Hosts: []trust.Record{{
		Machine: machineName, Address: address, Port: port, KeyType: "ssh-ed25519", PublicKey: "AAAAC3NzaC1lZDI1NTE5AAAAIHostKeyBytes",
		Fingerprint: "SHA256:fingerprint", Source: trust.SourceEnrollment, Recorded: "2026-11-03T00:00:00Z",
	}}}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func text(value string) api.Value { return api.StringValue(value) }

func fieldOf(name string, value api.Value) api.FieldValue {
	return api.FieldValue{Name: name, Value: value}
}

// sshMachine builds a Machine reached over SSH at address; pin names its
// knownHostsRef Secret, and installed makes Bootwright install its OS.
func sshMachine(name, address, pin string, installed bool) api.Object {
	ssh := []api.FieldValue{fieldOf("addressRef", text("mgmt"))}
	if pin != "" {
		ssh = append(ssh, fieldOf("knownHostsRef", text(pin)))
	}
	fields := []api.FieldValue{
		fieldOf("access", api.MapValue(fieldOf("ssh", api.MapValue(ssh...)))),
		fieldOf("network", api.MapValue(fieldOf("addresses", api.ListValue(api.MapValue(fieldOf("name", text("mgmt")), fieldOf("address", text(address))))))),
	}
	if installed {
		fields = append(fields, fieldOf("os", api.MapValue(fieldOf("provided", api.BoolValue(false)), fieldOf("installProfileRef", text("profile")))))
	}
	return api.NewObject(api.Machine, name, api.Value{}, api.MapValue(fields...))
}

func TestHostKeyProofsCountsEverySourceASessionProvesWithoutFirstUse(t *testing.T) {
	ctx := context.Background()
	stored := trustRecord(t, "m1", "192.0.2.11", 22)
	tests := []struct {
		name    string
		machine string
		object  api.Object
		records *recordsFake
		keys    keyFake
		want    bool
		reads   int
	}{
		{name: "a declared pin proves a provided Machine without any store", object: sshMachine("m1", "192.0.2.11", "pin", false), records: &recordsFake{}, want: true},
		{name: "a declared pin outranks installation evidence that is absent", object: sshMachine("m1", "192.0.2.11", "pin", true), records: &recordsFake{}, want: true},
		{name: "installation evidence proves an installed Machine", object: sshMachine("m1", "192.0.2.11", "", true), records: &recordsFake{}, keys: keyFake{found: true}, want: true},
		{name: "an installed Machine without evidence is unproved even with a store record", object: sshMachine("m1", "192.0.2.11", "", true), records: &recordsFake{data: stored}},
		{name: "a trust record for the dialed endpoint proves a provided Machine", object: sshMachine("m1", "192.0.2.11", "", false), records: &recordsFake{data: stored}, want: true, reads: 1},
		{name: "a trust record for another address is no proof", object: sshMachine("m1", "192.0.2.99", "", false), records: &recordsFake{data: stored}, reads: 1},
		{name: "a trust record for another Machine is no proof", machine: "m2", object: sshMachine("m2", "192.0.2.11", "", false), records: &recordsFake{data: stored}, reads: 1},
		{name: "an empty store proves nothing", object: sshMachine("m1", "192.0.2.11", "", false), records: &recordsFake{}, reads: 1},
		{name: "a damaged store proves nothing", object: sshMachine("m1", "192.0.2.11", "", false), records: &recordsFake{data: []byte("{")}, reads: 1},
		{name: "a Machine declaring no SSH access proves nothing", object: api.NewObject(api.Machine, "m1", api.Value{}, api.MapValue()), records: &recordsFake{data: stored}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proofs := hostKeyProofs{keys: tt.keys, records: tt.records}
			machineName := tt.machine
			if machineName == "" {
				machineName = "m1"
			}
			got, err := proofs.HostKeyProved(ctx, "lab", machineName, tt.object)
			if err != nil || got != tt.want || tt.records.reads != tt.reads {
				t.Fatalf("proved = %t (%v) after %d store reads, want %t after %d", got, err, tt.records.reads, tt.want, tt.reads)
			}
		})
	}
	failure := errors.New("the trust store is unreadable")
	if _, err := (hostKeyProofs{records: &recordsFake{failed: failure}}).HostKeyProved(ctx, "lab", "m1", sshMachine("m1", "192.0.2.11", "", false)); !errors.Is(err, failure) {
		t.Fatalf("store failure = %v", err)
	}
	if got, err := (hostKeyProofs{}).HostKeyProved(ctx, "lab", "m1", sshMachine("m1", "192.0.2.11", "", false)); err != nil || got {
		t.Fatalf("no store bound proved = %t (%v)", got, err)
	}
}

func stretchSessions(t *testing.T, machines []api.Object, records trustRecords) (map[string]inspection.AccessCommand, string) {
	t.Helper()
	nodes := make([]api.Value, 0, len(machines))
	for _, m := range machines {
		nodes = append(nodes, api.MapValue(fieldOf("name", text(m.Name())), fieldOf("fqdn", text(m.Name()+".lab.test")), fieldOf("machineRef", text(m.Name()))))
	}
	cluster := api.NewObject(api.CephCluster, "stretch", api.Value{}, api.MapValue(
		fieldOf("management", text("managed")),
		fieldOf("topology", api.MapValue(fieldOf("nodes", api.ListValue(nodes...))))))
	reader := &readerFake{resources: &lifecycle.StorageResourcesResult{}}
	service := inspection.New(catalogGraph{catalog: api.NewCatalog(append([]api.Object{cluster}, machines...))},
		cephInspection{reconciler: reader}, installProofs{reconciler: reader},
		hostKeyProofs{keys: machineHostKeys{reconciler: reader}, records: records},
		func(context.Context) (string, error) { return "lab", nil }, inspectionCustody{reader: &producedFake{}}).WithMembers(clusterMembers())
	result, err := service.ClusterInfo(context.Background(), inspection.ClusterInfoRequest{Names: []string{"stretch"}})
	if err != nil || len(result.Storage) != 1 {
		t.Fatalf("info = %+v (%v)", result, err)
	}
	commands := map[string]inspection.AccessCommand{}
	for _, command := range result.Storage[0].AccessCommands {
		commands[command.Command] = command
	}
	return commands, strings.Join(reader.reads, ",")
}

// arbiter-01 sorts before ceph-01, so it is the default node of the cluster.
func TestClusterInfoTreatsAPinnedProvidedTiebreakerAsProved(t *testing.T) {
	machines := []api.Object{
		sshMachine("ceph-01", "192.0.2.11", "", true),
		sshMachine("arbiter-01", "192.0.2.50", "arbiter-known-hosts", false),
		sshMachine("ceph-02", "192.0.2.12", "", true),
	}
	commands, _ := stretchSessions(t, machines, nil)
	for _, name := range []string{inspection.CommandExec, inspection.CommandRsh} {
		if command := commands[name]; command.Status != inspection.StatusReady || command.Node != "arbiter-01" || command.Reason != "" {
			t.Fatalf("%s = %+v, want ready on arbiter-01", name, command)
		}
	}
}

func TestClusterInfoRemedyForAnUnprovedProvidedMachineNamesMachineTrust(t *testing.T) {
	machines := []api.Object{
		sshMachine("arbiter-01", "192.0.2.50", "", false),
		sshMachine("ceph-01", "192.0.2.11", "", true),
	}
	commands, _ := stretchSessions(t, machines, &recordsFake{})
	for _, name := range []string{inspection.CommandExec, inspection.CommandRsh} {
		command := commands[name]
		if command.Status != inspection.StatusUnavailable || command.Node != "arbiter-01" ||
			!strings.Contains(command.Reason, "bootwright machine trust --context lab --name arbiter-01") ||
			!strings.Contains(command.Reason, "access.ssh.knownHostsRef") || strings.Contains(command.Reason, "--stage machines") {
			t.Fatalf("%s = %+v", name, command)
		}
	}
	commands, _ = stretchSessions(t, machines, &recordsFake{data: trustRecord(t, "arbiter-01", "192.0.2.50", 22)})
	if command := commands[inspection.CommandExec]; command.Status != inspection.StatusReady || command.Node != "arbiter-01" {
		t.Fatalf("after machine trust, exec = %+v", command)
	}
}

func TestClusterInfoKeepsTheMachinesStageRemedyForAnInstalledMachine(t *testing.T) {
	machines := []api.Object{sshMachine("ceph-01", "192.0.2.11", "", true)}
	commands, reads := stretchSessions(t, machines, &recordsFake{data: trustRecord(t, "ceph-01", "192.0.2.11", 22)})
	command := commands[inspection.CommandExec]
	if command.Status != inspection.StatusUnavailable || !strings.Contains(command.Reason, "bootwright apply --context lab --stage machines") ||
		strings.Contains(command.Reason, "machine trust") {
		t.Fatalf("exec = %+v after reads %s", command, reads)
	}
}
