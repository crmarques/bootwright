package ceph

import (
	"context"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func TestSixtyThreeByteNamesPlanUnderTheIdentitiesEveryBuilderNames(t *testing.T) {
	cluster, client, pool, gateway, user := strings.Repeat("c", 63), strings.Repeat("k", 63), strings.Repeat("p", 63), strings.Repeat("g", 63), strings.Repeat("u", 63)
	base := pinnedCatalog(t, func(spec api.Value) api.Value {
		spec = withPairAndHTTPRGW(spec)
		service := requestMap("service_type", "nvmeof", "service_id", "config.primary", "placement", requestMap("hosts", api.StringList("node-1", "node-2")), "spec", requestMap("pool", pool, "group", "primary", "enable_auth", false))
		spec = spec.WithPath(api.ListValue(append(spec.Get("services").Items(), service)...), "services")
		return spec.WithPath(requestMap("base", PinnedRegistry+"/cp/ibm-ceph/nvmeof-synthetic", "version", "synthetic"), "images", "nvmeof")
	})
	objects := append(base.Objects(), replicatedPool(pool), imageOf("disk", "volumes"),
		childOf(api.CephClient, client, "poolRef", "volumes", "access", "readWrite"),
		objectGateway(gateway), childOf(api.CephObjectUser, user, "objectGatewayRef", gateway),
		childOf(api.CephNVMeoFSubsystem, "subsystem", "serviceRef", "nvmeof.config.primary", "nqn", "nqn.2026-01.test.example:subsystem",
			"hosts", api.StringList("nqn.2026-01.test.example:initiator"), "listeners", api.ListValue(requestMap("host", "node-1")),
			"namespaces", api.ListValue(requestMap("nsid", 1, "imageRef", "disk"))))
	for i, object := range objects {
		if object.Kind() == api.CephCluster {
			objects[i] = api.NewObject(api.CephCluster, cluster, object.Labels(), object.Spec())
		} else if object.Spec().Get("clusterRef").Text() == "example" {
			objects[i] = object.WithSpec(object.Spec().With("clusterRef", api.StringValue(cluster)))
		}
	}
	catalog := api.NewCatalog(objects)
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "synthetic"}, State: compilation.NewState(catalog, catalog, nil), Controller: "controller"}
	capabilities := []lifecycle.Capability{NewImage(nil), NewHost(nil)}
	for _, kind := range []api.Kind{api.CephCluster, api.CephPool, api.CephClient, api.CephRBDImage, api.CephObjectGateway, api.CephObjectUser, api.CephNVMeoFSubsystem} {
		capabilities = append(capabilities, New(kind, nil))
	}
	var definitions []reconciliation.BlockDefinition
	for _, capability := range capabilities {
		plan, err := capability.Plan(context.Background(), input)
		if err != nil {
			t.Fatalf("a 63-byte Ceph name does not plan: %v", diagnostics.Of(err))
		}
		definitions = append(definitions, plan.Definitions...)
	}
	planned := map[string]bool{}
	present := map[reconciliation.ObjectRef]bool{}
	for _, definition := range definitions {
		if !reconciliation.ValidSegment(definition.ID) {
			t.Fatalf("the block %q is not a block identity", definition.ID)
		}
		planned[definition.ID] = true
		present[reconciliation.ObjectRef{Kind: definition.Kind, Object: definition.Object}] = true
	}
	for _, id := range []string{ClusterBlockID(cluster), ImageBlockID(cluster), SettingsBlockID(cluster), ClientBlockID(client), GatewayBlockID(gateway), ObjectUserBlockID(user), reconciliation.BlockIdentity("ceph-pool-", pool), ServiceUnitBlockID(cluster, ServiceUnit{Type: "nvmeof", ID: "config.primary"})} {
		if !planned[id] {
			t.Errorf("the builder identity %q names no planned block", id)
		}
	}
	for _, definition := range slices.Clone(definitions) {
		for _, ref := range definition.Requires {
			if !present[ref] {
				present[ref] = true
				definitions = append(definitions, reconciliation.BlockDefinition{ID: reconciliation.BlockIdentity("synthetic-", strings.ToLower(ref.Kind)+"-"+ref.Object), Description: "synthetic " + ref.Object, Stage: reconciliation.StageMachines, Kind: ref.Kind, Object: ref.Object, Implementation: "synthetic", ContentDigest: strings.Repeat("f", 64), Request: []byte(`{}`)})
			}
		}
	}
	if _, err := reconciliation.NewPlan(reconciliation.Apply, definitions); err != nil {
		t.Fatalf("a 63-byte Ceph name leaves a dependency unresolved: %v", diagnostics.Of(err))
	}
}

func TestFrozenRequestsRefuseANamePastTheLabelBound(t *testing.T) {
	long := strings.Repeat("a", 64)
	replication, _ := replicationPlan(t, replicationCatalog("image"))
	replication.Object = long
	data, err := replication.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	_, err = DecodeReplicationRequest(data)
	refusedWith(t, err, "lifecycle.state", "the frozen Ceph replication request is inconsistent", "")
	consumer, err := DecodeConsumerRequest(managedExportPlan(t, false).Request)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	consumer.Name = long
	if data, err = consumer.Canonical(); err != nil {
		t.Fatal(err)
	}
	_, err = DecodeConsumerRequest(data)
	refusedWith(t, err, "lifecycle.state", "the frozen Ceph consumer request is inconsistent", "")
}
