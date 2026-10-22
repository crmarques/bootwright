//go:build linux && amd64

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/environment/inspection"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/storage/ceph"
)

type objectHandoffFixture struct {
	catalog api.Catalog
	reads   []string
}

func (f *objectHandoffFixture) Effective(context.Context, string) (api.Catalog, error) {
	return f.catalog, nil
}
func (*objectHandoffFixture) Installed(context.Context, string, string) (bool, error) {
	return false, nil
}
func (*objectHandoffFixture) HostKeyProved(context.Context, string, string) (bool, error) {
	return false, nil
}
func (*objectHandoffFixture) DashboardPassword(context.Context, string, string) (inspection.Custodied, bool, error) {
	return inspection.Custodied{Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("dashboard-private")})}, true, nil
}
func (*objectHandoffFixture) ClientKeyring(context.Context, string, string) (inspection.Custodied, bool, error) {
	return inspection.Custodied{}, false, nil
}
func (f *objectHandoffFixture) ObjectUserCredentials(_ context.Context, contextName, user string) (inspection.Custodied, bool, error) {
	f.reads = append(f.reads, contextName+"/"+user)
	return inspection.Custodied{Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.UsernamePart: []byte("ACCESS"), secrets.PasswordPart: []byte("PRIVATE")})}, true, nil
}

func TestCompiledObjectUserHandoffUsesPublicGatewayReference(t *testing.T) {
	state, _ := compileAcceptance(t, contractSources(t, "libvirt-3-nodes-ceph-object+user"))
	catalog := state.Effective()
	users := catalog.OfKind(api.StorageObjectUser)
	if len(users) != 1 {
		t.Fatal("expected one compiled object user")
	}
	user := users[0]
	gateway := user.Spec().Get("objectGatewayRef").Text()
	if gateway == "" || user.Spec().Get("gatewayRef").Text() != "" {
		t.Fatal("fixture must use the actual public gateway reference")
	}
	plan, err := reconciliation.NewPlan(reconciliation.Apply, productionDefinitions(t, state, "handoff"))
	if err != nil {
		t.Fatal(err)
	}
	var block reconciliation.Block
	for _, candidate := range plan.Blocks {
		if candidate.ID == ceph.ObjectUserBlockID(user.Name()) {
			block = candidate
		}
	}
	request, err := ceph.DecodeRequest(block.Request)
	if err != nil {
		t.Fatal(err)
	}
	evidence := objectHandoffEvidence(t, block, request)
	for _, scenario := range []string{"proved", "missing-frozen", "moved-zone", "moved-gateway", "moved-cluster"} {
		t.Run(scenario, func(t *testing.T) {
			current := catalog
			selectedCluster := request.Cluster
			if scenario == "moved-cluster" {
				selectedCluster = "moved-cluster"
			}
			objects := catalog.Objects()
			for i, object := range objects {
				name := object.Name()
				fields := object.Spec().Fields()
				if scenario == "moved-cluster" && object.Kind() == api.StorageCluster && name == request.Cluster {
					name = selectedCluster
				}
				if scenario == "moved-gateway" && object.Kind() == api.StorageObjectGateway && name == gateway {
					name = "moved-gateway"
				}
				for j := range fields {
					if scenario == "moved-zone" && object.Kind() == api.StorageObjectGateway && fields[j].Name == "zone" {
						fields[j].Value = api.StringValue("moved-zone")
					}
					if scenario == "moved-gateway" && object.Kind() == api.StorageObjectUser && fields[j].Name == "objectGatewayRef" {
						fields[j].Value = api.StringValue("moved-gateway")
					}
					if scenario == "moved-cluster" && (object.Kind() == api.StorageObjectUser || object.Kind() == api.StorageObjectGateway) && fields[j].Name == "clusterRef" {
						fields[j].Value = api.StringValue(selectedCluster)
					}
				}
				objects[i] = api.NewObject(object.Kind(), name, api.Value{}, api.MapValue(fields...))
			}
			current = api.NewCatalog(objects)
			frozen := []reconciliation.Block{block}
			if scenario == "missing-frozen" {
				frozen = nil
			}
			reader := &readerFake{resources: &lifecycle.StorageResourcesResult{Frozen: frozen}, proofs: map[string][]lifecycle.BlockEvidence{string(api.StorageObjectUser) + "/" + user.Name(): {{Kind: block.Kind, Object: block.Object, Implementation: block.Implementation, Verb: reconciliation.Apply, State: reconciliation.BlockDone, Evidence: evidence}}}}
			fixture := &objectHandoffFixture{catalog: current}
			service := inspection.New(fixture, cephInspection{reconciler: reader}, fixture, fixture, func(context.Context) (string, error) { return "handoff", nil }, fixture)
			ordinary, err := service.ClusterInfo(context.Background(), inspection.ClusterInfoRequest{Names: []string{selectedCluster}})
			if err != nil || ordinary == nil || len(fixture.reads) != 0 {
				t.Fatal("ordinary inspection read credentials", err)
			}
			result, err := service.ClusterInfo(context.Background(), inspection.ClusterInfoRequest{Names: []string{selectedCluster}, Secrets: true})
			if scenario != "proved" {
				if err == nil || result != nil || len(fixture.reads) != 0 {
					t.Fatal("unproved scope reached custody", err, fixture.reads)
				}
				return
			}
			if err != nil || result == nil {
				t.Fatal("compiled user was refused", err)
			}
			defer inspection.ClearSecrets(result)
			if len(fixture.reads) != 1 || len(result.Storage[0].Ceph.ObjectUsers) != 1 || result.Storage[0].Ceph.ObjectUsers[0].Gateway != gateway {
				t.Fatal("compiled user did not reach selected gateway custody")
			}
		})
	}
}

func objectHandoffEvidence(t *testing.T, block reconciliation.Block, r ceph.Request) []byte {
	t.Helper()
	e := ceph.Evidence{Version: ceph.EvidenceVersion, Request: block.RequestDigest, Action: r.Action, Owner: "apply-original", FSID: ceph.FSIDFor("apply-original", r.Cluster), Owned: true, Exists: true, Ready: true, Health: "HEALTH_OK", Hosts: []string{}, Monitors: []string{}, MGRStandbys: []string{}, MonAddrs: []ceph.MonitorAddress{}, OSDs: []ceph.OSDEvidence{}, Daemons: []ceph.UnitDaemon{}, Resource: ceph.ResourceEvidence{ID: r.ObjectUser.Name}}
	for _, h := range r.Hosts {
		e.Hosts = append(e.Hosts, h.Name)
		for _, d := range h.Devices {
			e.OSDs = append(e.OSDs, ceph.OSDEvidence{Host: h.Name, Path: d.Path, ID: len(e.OSDs), UUID: fmt.Sprintf("osd-%d", len(e.OSDs))})
		}
	}
	for _, raw := range r.Services {
		var service struct {
			ServiceType string `json:"service_type"`
			Placement   struct {
				Hosts []string `json:"hosts"`
				Count *int     `json:"count"`
			} `json:"placement"`
		}
		if err := json.Unmarshal(raw, &service); err != nil {
			t.Fatal(err)
		}
		hosts := service.Placement.Hosts
		if service.Placement.Count != nil {
			hosts = hosts[:*service.Placement.Count]
		}
		switch service.ServiceType {
		case "mon":
			e.Monitors = hosts
		case "mgr":
			e.MGRActive = hosts[0]
			e.MGRStandbys = hosts[1:]
		}
	}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
