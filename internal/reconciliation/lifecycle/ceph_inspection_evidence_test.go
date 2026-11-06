package lifecycle_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/storage/ceph"
)

// Storage inspection decodes each Ceph host block's evidence against that
// block's frozen request digest, so the specializations a host proved are
// reported from the evidence the lifecycle read hands it, beside the
// cluster's own.
func TestCephInspectionReportsTheHostSpecializationsTheLifecycleEvidenceCarries(t *testing.T) {
	ctx := context.Background()
	define := func(id, implementation string) reconciliation.BlockDefinition {
		return reconciliation.BlockDefinition{
			ID: id, Description: "realize " + id, Stage: reconciliation.StageClusters, Kind: string(api.CephCluster), Object: "storage",
			Implementation: implementation, ContentDigest: strings.Repeat("c", 64), Request: json.RawMessage(`{"name":"` + id + `"}`),
		}
	}
	definitions := []reconciliation.BlockDefinition{
		define("ceph-image-storage", ceph.ImageImplementation),
		define("ceph-host-storage-rhel-01", ceph.HostImplementation),
		define("ceph-cluster-storage", ceph.Implementation),
	}
	hostDigest, err := reconciliation.RequestDigest(definitions[1])
	if err != nil {
		t.Fatal(err)
	}
	encode := func(evidence any) lifecycle.Result {
		data, err := json.Marshal(evidence)
		if err != nil {
			t.Fatal(err)
		}
		return lifecycle.Result{Outcome: reconciliation.OutcomeChanged, Evidence: data}
	}
	journey := lifecycle.NewScriptedJourney(t, definitions, map[string]lifecycle.Result{
		"ceph-image-storage": encode(ceph.ImageEvidence{Postcondition: true, Release: "9.9.1.2", Base: "cp.icr.io/cp/ibm-ceph/ceph-9-rhel9", Tag: "v9.9.1-25074",
			Digest: "sha256:" + strings.Repeat("e", 64), Platform: ceph.PinnedPlatform, Request: "digest"}),
		"ceph-host-storage-rhel-01": encode(ceph.HostEvidence{Devices: []ceph.HostDevice{}, FSID: "6b8f1c2e-4d7a-5e3f-9a1b-0c2d4e6f8a1b", Host: "rhel-01.lab.test",
			Ready: true, Request: hostDigest, Specializations: []string{"ceph-host-fips-continues"}, Version: ceph.HostEvidenceVersion}),
		"ceph-cluster-storage": encode(ceph.Evidence{
			Version: ceph.EvidenceVersion, Request: "digest", Action: "cluster", Owner: "apply", FSID: "6b8f1c2e-4d7a-5e3f-9a1b-0c2d4e6f8a1b", Owned: true, Exists: true, Ready: true,
			Health: "HEALTH_OK", Hosts: []string{"rhel-01.lab.test"}, Monitors: []string{"rhel-01.lab.test"}, MGRActive: "rhel-01.lab.test", MGRStandbys: []string{},
			OSDs: []ceph.OSDEvidence{}, Daemons: []ceph.UnitDaemon{}, MonAddrs: []ceph.MonitorAddress{{Host: "rhel-01.lab.test", Address: "198.51.100.11"}},
			Specializations: []string{"ceph-bootstrap-license-call-continues"},
		}),
	})
	if _, err := journey.Service.Apply(ctx, lifecycle.ApplyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true}); err != nil {
		t.Fatalf("apply: %v", diagnostics.Of(err))
	}
	proofs, err := journey.Service.Evidence(ctx, lifecycle.JourneyContext, string(api.CephCluster), "storage")
	if err != nil {
		t.Fatal(err)
	}
	addresses := api.ListValue(api.MapValue(api.FieldValue{Name: "name", Value: api.StringValue("mgmt")}, api.FieldValue{Name: "address", Value: api.StringValue("192.0.2.11")}))
	host := api.NewObject(api.Machine, "m1", api.Value{}, api.MapValue(
		api.FieldValue{Name: "access", Value: api.MapValue(api.FieldValue{Name: "ssh", Value: api.MapValue(api.FieldValue{Name: "addressRef", Value: api.StringValue("mgmt")})})},
		api.FieldValue{Name: "network", Value: api.MapValue(api.FieldValue{Name: "addresses", Value: addresses})}))
	node := api.MapValue(api.FieldValue{Name: "name", Value: api.StringValue("rhel-01")}, api.FieldValue{Name: "fqdn", Value: api.StringValue("rhel-01.lab.test")},
		api.FieldValue{Name: "machineRef", Value: api.StringValue("m1")})
	cluster := api.NewObject(api.CephCluster, "storage", api.Value{}, api.MapValue(
		api.FieldValue{Name: "topology", Value: api.MapValue(api.FieldValue{Name: "nodes", Value: api.ListValue(node)})}))
	found, reason := ceph.Inspect(api.NewCatalog([]api.Object{cluster, host}), cluster, proofs)
	if want := []string{"ceph-bootstrap-license-call-continues", "ceph-host-fips-continues"}; reason != "" || !slices.Equal(found.Specializations, want) {
		t.Fatalf("inspection = %+v (%q), want the specializations %v", found.Specializations, reason, want)
	}
}
