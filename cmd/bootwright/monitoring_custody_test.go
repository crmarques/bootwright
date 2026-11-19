//go:build linux && amd64

package main

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/environment/inspection"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
	"github.com/crmarques/bootwright/internal/storage/ceph"
)

type currentFake struct {
	reads    []string
	material secrets.Material
	failed   error
}

func (c *currentFake) ReadCurrent(_ context.Context, request custody.ReadCurrentRequest) ([]secretstore.BoundMaterial, error) {
	c.reads = append(c.reads, request.ContextName+"/"+strings.Join(request.Names, ","))
	if c.failed != nil {
		return nil, c.failed
	}
	return []secretstore.BoundMaterial{{Material: c.material}}, nil
}

func monitoringBlock(t *testing.T, cluster string, bindings ...ceph.ServiceBinding) reconciliation.Block {
	t.Helper()
	r := ceph.Request{Version: ceph.Version, Action: "service", Cluster: cluster, Service: &ceph.ServiceUnit{Type: "monitoring", Bindings: bindings}}
	data, err := r.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	return reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{Kind: "CephCluster", Object: cluster, Implementation: ceph.Implementation, Request: data}}
}

func TestGrafanaPasswordComesFromItsBoundSecret(t *testing.T) {
	t.Parallel()
	bound := ceph.ServiceBinding{Path: "/spec/initial_admin_password", SecretRef: "grafana-admin", ServiceRef: "grafana"}
	other := ceph.ServiceBinding{Path: "/spec/credentials/snmp_community", SecretRef: "snmp", ServiceRef: "snmp-gateway"}
	frozen := []reconciliation.Block{monitoringBlock(t, "other", ceph.ServiceBinding{Path: bound.Path, SecretRef: "foreign", ServiceRef: "grafana"}), monitoringBlock(t, "storage", other, bound)}
	reader := &readerFake{resources: &lifecycle.StorageResourcesResult{Frozen: frozen}}
	current := &currentFake{material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("gr-Secret.99")})}
	got, found, err := (inspectionCustody{frozen: reader, current: current}).GrafanaPassword(context.Background(), "lab", "storage")
	value, _ := got.Material.Part(secrets.ValuePart)
	if err != nil || !found || got.Unproved || string(value) != "gr-Secret.99" || !slices.Equal(current.reads, []string{"lab/grafana-admin"}) {
		t.Fatalf("Grafana custody = %q %t (%v) after %v", value, found, err, current.reads)
	}
	got.Material.Clear()
	anonymous := &readerFake{resources: &lifecycle.StorageResourcesResult{Frozen: []reconciliation.Block{monitoringBlock(t, "storage", other)}}}
	current = &currentFake{}
	if _, found, err := (inspectionCustody{frozen: anonymous, current: current}).GrafanaPassword(context.Background(), "lab", "storage"); found || err != nil || len(current.reads) != 0 {
		t.Fatalf("an unbound Grafana read %v (%t, %v)", current.reads, found, err)
	}
	failure := errors.New("the keyring is locked")
	if _, _, err := (inspectionCustody{frozen: reader, current: &currentFake{failed: failure}}).GrafanaPassword(context.Background(), "lab", "storage"); !errors.Is(err, failure) {
		t.Fatalf("a custody failure = %v", err)
	}
}

func TestMonitoringCustodyReadsTheGatewayBlocksEntry(t *testing.T) {
	t.Parallel()
	value := `{"alertmanager":{"password":"am-Secret.42","user":"am-admin"},"prometheus":{"password":"pr-Secret.17","user":"pr-admin"}}`
	material := secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(value)})
	reader := &producedFake{material: material, found: true, unproved: true}
	got, found, err := (inspectionCustody{reader: reader}).MonitoringCredentials(context.Background(), "lab", "storage", "prometheus")
	user, _ := got.Material.Part(secrets.UsernamePart)
	password, _ := got.Material.Part(secrets.PasswordPart)
	if err != nil || !found || !got.Unproved || string(user) != "pr-admin" || string(password) != "pr-Secret.17" || !zeroed(material) ||
		!slices.Equal(reader.reads, []string{"lab/ceph-service-storage-mgmt-gateway/monitoring"}) {
		t.Fatalf("monitoring custody = %q %q %t (%v) after %v", user, password, found, err, reader.reads)
	}
	got.Material.Clear()
	foreign := secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(`{"alertmanager":{"password":"am-Secret.42","user":"am-admin"}}`)})
	if _, found, err := (inspectionCustody{reader: &producedFake{material: foreign, found: true}}).MonitoringCredentials(context.Background(), "lab", "storage", "prometheus"); !found || !errors.Is(err, inspection.ErrMonitoringMaterial) || !zeroed(foreign) {
		t.Fatalf("a login without prometheus = %t (%v)", found, err)
	}
	if _, found, err := (inspectionCustody{reader: &producedFake{}}).MonitoringCredentials(context.Background(), "lab", "storage", "prometheus"); found || err != nil {
		t.Fatalf("an absent entry = %t (%v)", found, err)
	}
}
