package inspection

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/secrets"
)

const testKeyring = "[client.vm-disks]\n\tkey = AQBx57l5Y2FuYXJ5LWNsaWVudC1rZXk9PQ==\n"

func storageClient(name, cluster string) api.Object {
	return api.NewObject(api.StorageClient, name, api.Value{}, api.MapValue(field("clusterRef", text(cluster)), field("poolRef", text("volumes"))))
}

func keyringFixture(t *testing.T) *fixture {
	f := newFixture(t)
	f.catalog = api.NewCatalog(append(f.catalog.Objects(), storageClient("vm-disks", "storage"), storageClient("archive-reader", "archive")))
	f.keyrings = map[string]Custodied{"vm-disks": {Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(testKeyring)})}}
	return f
}

func refusal(t *testing.T, err error, code, message, remediation string) {
	t.Helper()
	found := diagnostics.Of(err)
	if len(found) != 1 || found[0].Code != code || found[0].Message != message || found[0].Remediation != remediation {
		t.Fatalf("refusal = %v, want %s %q next %q", err, code, message, remediation)
	}
}

func TestKeyringRefusalOrderReadsNoCustodyBeforeTarget(t *testing.T) {
	clients := "bootwright cluster info --context lab --name storage lists the StorageClients of storage"
	kinds := "bootwright cluster info --context lab lists the clusters this context selects and their kinds"
	tests := []struct {
		name, cluster, client, code, message, remediation string
	}{
		{"no cluster", " ", "vm-disks", "access.target", "bootwright cluster keyring requires one cluster name",
			"bootwright cluster info --context lab lists the clusters this context selects"},
		{"no client", "storage", "", "access.target", "bootwright cluster keyring requires one StorageClient name", clients},
		{"unknown cluster", "missing", "vm-disks", "access.target", "the context lab selects no StorageCluster named missing",
			"name a managed Ceph StorageCluster this context selects; bootwright cluster info --context lab lists them"},
		{"container", "edge", "vm-disks", "cluster.not-applicable",
			"bootwright cluster keyring does not apply to edge, an OpenShift or OKD ContainerCluster; it applies to managed Ceph StorageClusters only", kinds},
		{"external", "archive", "archive-reader", "cluster.not-applicable",
			"bootwright cluster keyring does not apply to archive, an external Ceph StorageCluster; it applies to managed Ceph StorageClusters only", kinds},
		{"unknown client", "storage", "missing", "access.target", "the context lab selects no StorageClient named missing", clients},
		{"another cluster's client", "storage", "archive-reader", "access.target", "the StorageClient archive-reader belongs to StorageCluster archive, not storage", clients},
	}
	for _, tt := range tests {
		f := keyringFixture(t)
		result, err := f.service("lab").ClientKeyring(context.Background(), KeyringRequest{Name: tt.cluster, Client: tt.client})
		if result != nil {
			t.Fatalf("%s: result %+v", tt.name, result)
		}
		refusal(t, err, tt.code, tt.message, tt.remediation)
		f.untouched()
	}
	f := keyringFixture(t)
	result, err := f.service("lab").ClientKeyring(context.Background(), KeyringRequest{Name: " storage ", Client: " vm-disks "})
	value, _ := result.Material.Part(secrets.ValuePart)
	if err != nil || string(value) != testKeyring || result.Context != "lab" || result.Cluster != "storage" || result.Client != "vm-disks" || result.Unproved ||
		!slices.Equal(f.clients, []string{"lab/vm-disks"}) || len(f.custody) != 0 {
		t.Fatalf("keyring = %+v (%v) after %v", result, err, f.clients)
	}
	f = keyringFixture(t)
	delete(f.keyrings, "vm-disks")
	_, err = f.service("lab").ClientKeyring(context.Background(), KeyringRequest{Name: "storage", Client: "vm-disks"})
	refusal(t, err, "access.unavailable", "this context holds no keyring for StorageClient vm-disks of StorageCluster storage; an apply keeps it once it creates the client",
		"bootwright apply --context lab --stage clusters")
	f = keyringFixture(t)
	f.clientErr = fmt.Errorf("decoding: %w", ErrClientMaterial)
	_, err = f.service("lab").ClientKeyring(context.Background(), KeyringRequest{Name: "storage", Client: "vm-disks"})
	refusal(t, err, "secret.store.corrupt", "this context's custody holds a keyring entry for StorageClient vm-disks that is not one Ceph credential of client.vm-disks",
		"read it natively with the administrator command that bootwright cluster info --context lab --name storage prints, followed by auth get client.vm-disks")
	f = keyringFixture(t)
	f.clientErr = errors.New("the custody store is unreadable")
	if _, err := f.service("lab").ClientKeyring(context.Background(), KeyringRequest{Name: "storage", Client: "vm-disks"}); !errors.Is(err, f.clientErr) || len(f.clients) != 1 {
		t.Fatalf("read failure = %v", err)
	}
	unwired := keyringFixture(t)
	service := New(unwired, unwired, unwired, unwired, func(context.Context) (string, error) { return "lab", nil }, nil)
	if _, err := service.ClientKeyring(context.Background(), KeyringRequest{Name: "storage", Client: "vm-disks"}); !errors.Is(err, availability.ErrNotImplemented) {
		t.Fatalf("a service without custody = %v", err)
	}
}

func TestKeyringRefusesClientOfAnotherCluster(t *testing.T) {
	f := newFixture(t)
	nodes := node("rhel-01", "rhel-01.lab.test", "machine-a")
	f.catalog = api.NewCatalog([]api.Object{
		storageCluster("ceph-a", "managed", "rhel-01", nodes), storageCluster("ceph-b", "managed", "rhel-01", nodes), storageClient("vm-disks", "ceph-a"),
	})
	f.keyrings = map[string]Custodied{"vm-disks": {Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(testKeyring)})}}
	result, err := f.service("lab").ClientKeyring(context.Background(), KeyringRequest{Name: "ceph-b", Client: "vm-disks"})
	if result != nil {
		t.Fatalf("result %+v", result)
	}
	refusal(t, err, "access.target", "the StorageClient vm-disks belongs to StorageCluster ceph-a, not ceph-b",
		"bootwright cluster info --context lab --name ceph-b lists the StorageClients of ceph-b")
	f.untouched()
}

func keyringEntry(t *testing.T, commands []AccessCommand) AccessCommand {
	t.Helper()
	index := slices.IndexFunc(commands, func(c AccessCommand) bool { return c.Command == CommandKeyring })
	if index != 1 {
		t.Fatalf("no keyring entry after exec in %+v", commands)
	}
	return commands[index]
}

func TestClusterInfoKeyringEntryFollowsTheClientRows(t *testing.T) {
	tests := []struct {
		name string
		rows []Resource
		want AccessCommand
	}{
		{"done", []Resource{{Kind: "StoragePool", Name: "volumes", Status: ResourceDone}, {Kind: "StorageClient", Name: "a", Status: ResourcePending}, {Kind: "StorageClient", Name: "vm-disks", Status: ResourceDone}},
			AccessCommand{Command: CommandKeyring, Status: StatusReady}},
		{"pending", []Resource{{Kind: "StorageClient", Name: "vm-disks", Status: ResourcePending}, {Kind: "StorageClient", Name: "b", Status: ResourceFailed}},
			AccessCommand{Command: CommandKeyring, Status: StatusUnavailable, Reason: "no StorageClient of storage has a completed apply; run bootwright apply --context lab --stage clusters"}},
		{"none", []Resource{{Kind: "StoragePool", Name: "volumes", Status: ResourceDone}},
			AccessCommand{Command: CommandKeyring, Status: StatusUnavailable, Reason: "the StorageCluster storage declares no StorageClient"}},
	}
	for _, tt := range tests {
		f := keyringFixture(t)
		f.rows = map[string][]Resource{"storage": tt.rows}
		result, err := f.service("lab").ClusterInfo(context.Background(), ClusterInfoRequest{})
		if err != nil {
			t.Fatal(err)
		}
		if got := keyringEntry(t, result.Storage[1].AccessCommands); got != tt.want {
			t.Fatalf("%s: keyring = %+v, want %+v", tt.name, got, tt.want)
		}
		for _, entry := range append(slices.Clone(result.Clusters), ContainerEntry{Name: result.Storage[0].Name, AccessCommands: result.Storage[0].AccessCommands}) {
			want := AccessCommand{Command: CommandKeyring, Status: StatusNotApplicable, Reason: "applies to managed Ceph StorageClusters only"}
			if got := keyringEntry(t, entry.AccessCommands); got != want {
				t.Fatalf("%s: %s keyring = %+v", tt.name, entry.Name, got)
			}
		}
		if len(f.clients) != 0 || len(f.custody) != 0 {
			t.Fatalf("%s: cluster info read custody %v %v", tt.name, f.clients, f.custody)
		}
	}
}
