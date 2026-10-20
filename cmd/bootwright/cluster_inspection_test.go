//go:build linux && amd64

package main

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	containeraccess "github.com/crmarques/bootwright/internal/containercluster/access"
	"github.com/crmarques/bootwright/internal/containercluster/agentinstall"
	"github.com/crmarques/bootwright/internal/environment/inspection"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/storage/ceph"
)

type inspectedEnvelope struct {
	Command string `json:"command"`
	OK      bool   `json:"ok"`
	Result  struct {
		Context  string `json:"context"`
		Clusters []struct {
			Name           string `json:"name"`
			Kind           string `json:"kind"`
			AccessCommands []struct {
				Command string `json:"command"`
				Status  string `json:"status"`
				Reason  string `json:"reason"`
			} `json:"accessCommands"`
		} `json:"clusters"`
		Storage []map[string]json.RawMessage `json:"storage"`
	} `json:"result"`
}

func decodeInspected(t *testing.T, out string) inspectedEnvelope {
	t.Helper()
	var envelope inspectedEnvelope
	if err := json.Unmarshal([]byte(out), &envelope); err != nil || !envelope.OK {
		t.Fatalf("the output is not one successful envelope: %v\n%s", err, out)
	}
	return envelope
}

func TestClusterListAndInfoReadOnlyLocalStateForAnExplicitOrCurrentContext(t *testing.T) {
	services, _, _ := snoContext(t)
	for _, args := range [][]string{{"--context", "lab-sno"}, {}} {
		out, stderr := contextRun(t, services, 0, append([]string{"cluster", "list"}, args...)...)
		if stderr != "" || out != "  NAME   KIND\n  ceph   StorageCluster\n  sno    ContainerCluster\n" {
			t.Fatalf("%v listed %q with standard error %q", args, out, stderr)
		}
		out, stderr = contextRun(t, services, 0, append([]string{"cluster", "list", "--output", "json"}, args...)...)
		listed := decodeInspected(t, out)
		if stderr != "" || listed.Command != "cluster list" || listed.Result.Context != "lab-sno" || len(listed.Result.Clusters) != 2 ||
			listed.Result.Clusters[0].Name != "ceph" || listed.Result.Clusters[0].Kind != "StorageCluster" || listed.Result.Clusters[1].Name != "sno" || listed.Result.Clusters[1].Kind != "ContainerCluster" {
			t.Fatalf("%v listed %s with standard error %q", args, out, stderr)
		}
	}
	out, stderr := contextRun(t, services, 0, "cluster", "info", "--context", "lab-sno", "--output", "json")
	info := decodeInspected(t, out)
	if stderr != "" || len(info.Result.Clusters) != 1 || len(info.Result.Storage) != 1 {
		t.Fatalf("info = %s with standard error %q", out, stderr)
	}
	sno := info.Result.Clusters[0]
	statuses := map[string]string{}
	for _, command := range sno.AccessCommands {
		statuses[command.Command] = command.Status
		if command.Command == "cluster kubeconfig" && !strings.Contains(command.Reason, "bootwright apply --context lab-sno") {
			t.Fatalf("kubeconfig reason = %q", command.Reason)
		}
	}
	if sno.Name != "sno" || statuses["cluster kubeconfig"] != "unavailable" || statuses["cluster oc"] != "not-implemented" || statuses["cluster rsh"] != "not-implemented" {
		t.Fatalf("sno access = %v", statuses)
	}
	external := info.Result.Storage[0]
	if string(external["name"]) != `"ceph"` || external["management"] != nil || external["fsid"] != nil || external["cephCommand"] != nil || external["accessCommands"] == nil {
		t.Fatalf("external entry = %v", external)
	}
	out, stderr = contextRun(t, services, 0, "cluster", "info", "--name", "ceph")
	if stderr != "" || !strings.HasPrefix(out, "  ○ cluster | ceph\n") || strings.Contains(out, "sno") || !strings.Contains(out, "not-applicable") {
		t.Fatalf("selected info = %q with standard error %q", out, stderr)
	}
}

func TestClusterInfoRefusesAnUnselectedNameAndNonApplicableSecretsWithoutReadingCustody(t *testing.T) {
	services, repository, _ := snoContext(t)
	produceThroughLentArea(t, services, repository, "lab-sno", agentinstall.InstallBlockID("sno"), agentinstall.KubeconfigOutput, snoKubeconfig)
	for name, code := range map[string]string{"missing": "access.target", "sno-01": "access.target"} {
		out, stderr := contextRun(t, services, 1, "cluster", "info", "--name", name)
		if out != "" {
			t.Fatalf("%s wrote %q", name, out)
		}
		singleDiagnostic(t, stderr, code)
	}
	for name, mention := range map[string]string{"sno": "bootwright cluster kubeconfig", "ceph": "external Ceph StorageCluster"} {
		out, stderr := contextRun(t, services, 1, "cluster", "info", "--secrets", "--name", name)
		singleDiagnostic(t, stderr, "cluster.not-applicable")
		if out != "" || !strings.Contains(stderr, mention) || strings.Contains(stderr, "apiVersion") || strings.Contains(stderr, "api.sno.lab.example.test") {
			t.Fatalf("%s refused with %q and %q", name, out, stderr)
		}
	}
	out, stderr := contextRun(t, services, 2, "cluster", "info", "--secrets")
	if out != "" || !strings.Contains(stderr, "--secrets requires an explicit --name") {
		t.Fatalf("a nameless reveal = %q, %q", out, stderr)
	}
}

type readerFake struct {
	proofs    map[string][]lifecycle.BlockEvidence
	resources *lifecycle.StorageResourcesResult
	failed    error
	reads     []string
}

func (r *readerFake) StorageResources(_ context.Context, contextName string) (*lifecycle.StorageResourcesResult, error) {
	r.reads = append(r.reads, "resources/"+contextName)
	return r.resources, r.failed
}

func (r *readerFake) Evidence(_ context.Context, _, kind, object string) ([]lifecycle.BlockEvidence, error) {
	r.reads = append(r.reads, kind+"/"+object)
	return r.proofs[kind+"/"+object], r.failed
}

type keyFake struct {
	found  bool
	failed error
}

func (k keyFake) HostKey(context.Context, string, string) (machine.HostKeyEvidence, bool, error) {
	return machine.HostKeyEvidence{}, k.found, k.failed
}

func TestInspectionWiringMapsEvidenceReadersToProofs(t *testing.T) {
	ctx := context.Background()
	done := func(implementation string, kind api.Kind, object string) lifecycle.BlockEvidence {
		return lifecycle.BlockEvidence{Kind: string(kind), Object: object, Implementation: implementation, Verb: reconciliation.Apply, State: reconciliation.BlockDone}
	}
	pending := done(agentinstall.InstallImplementation, api.ContainerCluster, "sno")
	pending.State = reconciliation.BlockPending
	other := done("cluster-media-agent-v1", api.ContainerCluster, "sno")
	for name, tt := range map[string]struct {
		proofs []lifecycle.BlockEvidence
		want   bool
	}{
		"install done":     {[]lifecycle.BlockEvidence{other, done(agentinstall.InstallImplementation, api.ContainerCluster, "sno")}, true},
		"install pending":  {[]lifecycle.BlockEvidence{other, pending}, false},
		"only the media":   {[]lifecycle.BlockEvidence{other}, false},
		"no operation yet": {nil, false},
	} {
		reader := &readerFake{proofs: map[string][]lifecycle.BlockEvidence{"ContainerCluster/sno": tt.proofs}}
		got, err := installProofs{reconciler: reader}.Installed(ctx, "lab", "sno")
		if err != nil || got != tt.want || !slices.Equal(reader.reads, []string{"ContainerCluster/sno"}) {
			t.Fatalf("%s: installed = %t (%v) after reads %v", name, got, err, reader.reads)
		}
	}
	failure := errors.New("the lifecycle state is unreadable")
	if _, err := (installProofs{reconciler: &readerFake{failed: failure}}).Installed(ctx, "lab", "sno"); !errors.Is(err, failure) {
		t.Fatalf("install evidence failure = %v", err)
	}
	if _, err := (cephInspection{reconciler: &readerFake{failed: failure}}).ManagedCeph(ctx, "lab", api.Catalog{}, api.Object{}); !errors.Is(err, failure) {
		t.Fatalf("ceph evidence failure = %v", err)
	}
	if _, err := (cephInspection{reconciler: &readerFake{failed: failure}}).Resources(ctx, "lab", api.Catalog{}); !errors.Is(err, failure) {
		t.Fatalf("storage resources failure = %v", err)
	}
	inspectedResourceRows(t)
	if found, err := (hostKeyProofs{keys: keyFake{found: true}}).HostKeyProved(ctx, "lab", "m1"); err != nil || !found {
		t.Fatalf("proved host key = %t (%v)", found, err)
	}
	if found, err := (hostKeyProofs{keys: keyFake{}}).HostKeyProved(ctx, "lab", "m1"); err != nil || found {
		t.Fatalf("unproved host key = %t (%v)", found, err)
	}
	if _, err := (hostKeyProofs{keys: keyFake{failed: failure}}).HostKeyProved(ctx, "lab", "m1"); !errors.Is(err, failure) {
		t.Fatalf("host key failure = %v", err)
	}
}

func inspectedStorageCluster() (api.Catalog, api.Object) {
	machineObject := func(name, address string) api.Object {
		addresses := api.ListValue(api.MapValue(api.FieldValue{Name: "name", Value: api.StringValue("mgmt")}, api.FieldValue{Name: "address", Value: api.StringValue(address)}))
		return api.NewObject(api.Machine, name, api.Value{}, api.MapValue(
			api.FieldValue{Name: "access", Value: api.MapValue(api.FieldValue{Name: "ssh", Value: api.MapValue(api.FieldValue{Name: "addressRef", Value: api.StringValue("mgmt")})})},
			api.FieldValue{Name: "network", Value: api.MapValue(api.FieldValue{Name: "addresses", Value: addresses})}))
	}
	node := api.MapValue(api.FieldValue{Name: "name", Value: api.StringValue("rhel-01")}, api.FieldValue{Name: "fqdn", Value: api.StringValue("rhel-01.lab.test")}, api.FieldValue{Name: "machineRef", Value: api.StringValue("m1")})
	cluster := api.NewObject(api.StorageCluster, "storage", api.Value{}, api.MapValue(
		api.FieldValue{Name: "ceph", Value: api.MapValue(api.FieldValue{Name: "topology", Value: api.MapValue(api.FieldValue{Name: "nodes", Value: api.ListValue(node)})})}))
	return api.NewCatalog([]api.Object{cluster, machineObject("m1", "192.0.2.11")}), cluster
}

const inspectedDigest = "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"

func inspectedClusterProofs(t *testing.T) []lifecycle.BlockEvidence {
	t.Helper()
	digest := inspectedDigest
	image, err := json.Marshal(ceph.ImageEvidence{Postcondition: true, Release: "9.9.1.2", Base: "cp.icr.io/cp/ibm-ceph/ceph-9-rhel9", Tag: "v9.9.1-25074", Digest: digest, Platform: ceph.PinnedPlatform, Request: "digest"})
	if err != nil {
		t.Fatal(err)
	}
	bootstrap, err := json.Marshal(ceph.Evidence{
		Version: ceph.EvidenceVersion, Request: "digest", Action: "cluster", Owner: "apply", FSID: "6b8f1c2e-4d7a-5e3f-9a1b-0c2d4e6f8a1b", Owned: true, Exists: true, Ready: true,
		Health: "HEALTH_OK", Hosts: []string{"rhel-01.lab.test"}, Monitors: []string{"rhel-01.lab.test"}, MGRActive: "rhel-01.lab.test", MGRStandbys: []string{}, OSDs: []ceph.OSDEvidence{}, Daemons: []ceph.UnitDaemon{},
		MonAddrs:        []ceph.MonitorAddress{{Host: "rhel-01.lab.test", Address: "198.51.100.11"}},
		Dashboard:       ceph.DashboardEvidence{URL: "https://rhel-01.lab.test:8443/", CertificateSHA256: strings.Repeat("c", 64), Reachable: true},
		Specializations: []string{"ceph-bootstrap-license-call-continues"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return []lifecycle.BlockEvidence{
		{Kind: "StorageCluster", Object: "storage", Implementation: ceph.ImageImplementation, Verb: reconciliation.Apply, State: reconciliation.BlockDone, Evidence: image},
		{Kind: "StorageCluster", Object: "storage", Implementation: ceph.Implementation, Verb: reconciliation.Apply, State: reconciliation.BlockDone, Evidence: bootstrap},
	}
}

func TestCephInspectionReportsAReasonWithoutProofsAndTheFSIDWithThem(t *testing.T) {
	catalog, cluster := inspectedStorageCluster()
	reader := &readerFake{}
	proof, err := (cephInspection{reconciler: reader}).ManagedCeph(context.Background(), "lab", catalog, cluster)
	if err != nil || proof.Reason == "" || proof.FSID != "" || !slices.Equal(reader.reads, []string{"StorageCluster/storage"}) {
		t.Fatalf("proof without evidence = %+v (%v) after reads %v", proof, err, reader.reads)
	}
	digest := inspectedDigest
	reader.proofs = map[string][]lifecycle.BlockEvidence{"StorageCluster/storage": inspectedClusterProofs(t)}
	proof, err = (cephInspection{reconciler: reader}).ManagedCeph(context.Background(), "lab", catalog, cluster)
	if err != nil || proof.Reason != "" || proof.FSID != "6b8f1c2e-4d7a-5e3f-9a1b-0c2d4e6f8a1b" || proof.Image.Digest != digest || proof.Image.Platform != "linux/amd64" ||
		len(proof.Monitors) != 1 || proof.Monitors[0].Host != "rhel-01" || proof.Monitors[0].FQDN != "rhel-01.lab.test" || proof.Monitors[0].Address != "198.51.100.11" ||
		!slices.Equal(proof.Specializations, []string{"ceph-bootstrap-license-call-continues"}) {
		t.Fatalf("proof = %+v (%v)", proof, err)
	}
	want := inspection.Dashboard{Status: "ready", User: "admin", CertificateSHA256: strings.Repeat("c", 64),
		URLs: []inspection.DashboardURL{{Host: "rhel-01", URL: "https://rhel-01.lab.test:8443/", Role: "active"}}}
	if !reflect.DeepEqual(proof.Dashboard, want) {
		t.Fatalf("dashboard = %+v, want %+v", proof.Dashboard, want)
	}
}

type producedFake struct {
	reads    []string
	material secrets.Material
	unproved bool
	found    bool
	failed   error
}

func (p *producedFake) ReadProduced(_ context.Context, contextName, block, name string) (containeraccess.Custodied, bool, error) {
	p.reads = append(p.reads, contextName+"/"+block+"/"+name)
	return containeraccess.Custodied{Material: p.material, Unproved: p.unproved}, p.found, p.failed
}

func TestDashboardCustodyReadsTheClusterBlocksDashboardEntry(t *testing.T) {
	reader := &producedFake{material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("Xq7-Lm2_Rv9.Tk4-Wz8_Hn3.Pb6-Jd5_")}), unproved: true, found: true}
	got, found, err := (inspectionCustody{reader: reader}).DashboardPassword(context.Background(), "lab", "storage")
	value, _ := got.Material.Part(secrets.ValuePart)
	if err != nil || !found || !got.Unproved || string(value) != "Xq7-Lm2_Rv9.Tk4-Wz8_Hn3.Pb6-Jd5_" || !slices.Equal(reader.reads, []string{"lab/ceph-cluster-storage/dashboard"}) {
		t.Fatalf("custody = %+v %t (%v) after %v", got, found, err, reader.reads)
	}
	failure := errors.New("the custody store is unreadable")
	if _, _, err := (inspectionCustody{reader: &producedFake{failed: failure}}).DashboardPassword(context.Background(), "lab", "storage"); !errors.Is(err, failure) {
		t.Fatalf("custody failure = %v", err)
	}
}

func zeroed(material secrets.Material) bool {
	for _, part := range material.Parts() {
		if value, _ := material.Part(part); len(value) == 0 || strings.Trim(string(value), "\x00") != "" {
			return false
		}
	}
	return len(material.Parts()) != 0
}

func TestClientCustodyReadsTheClientBlocksEntry(t *testing.T) {
	const key = "AQBx57l5Y2FuYXJ5LWNsaWVudC1rZXk9PQ=="
	entry := `[{"caps":{"mgr":"profile rbd pool=volumes","mon":"profile rbd","osd":"profile rbd pool=volumes"},"entity":"client.vm-disks","key":"` + key + `"}]`
	source := secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(entry)})
	reader := &producedFake{material: source, unproved: true, found: true}
	got, found, err := (inspectionCustody{reader: reader}).ClientKeyring(context.Background(), "lab", "vm-disks")
	value, _ := got.Material.Part(secrets.ValuePart)
	if err != nil || !found || !got.Unproved || string(value) != "[client.vm-disks]\n\tkey = "+key+"\n" || !slices.Equal(reader.reads, []string{"lab/ceph-client-vm-disks/client"}) || !zeroed(source) {
		t.Fatalf("custody = %q %t %t (%v) after %v", value, got.Unproved, found, err, reader.reads)
	}
	for name, material := range map[string]secrets.Material{
		"another client": secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(strings.Replace(entry, "client.vm-disks", "client.other", 1))}),
		"a keyring":      secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("[client.vm-disks]\n\tkey = " + key + "\n")}),
		"another part":   secrets.NewMaterial(map[secrets.Part][]byte{secrets.PasswordPart: []byte(entry)}),
		"two parts":      secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(entry), secrets.PasswordPart: []byte("x")}),
	} {
		got, _, err := (inspectionCustody{reader: &producedFake{material: material, found: true}}).ClientKeyring(context.Background(), "lab", "vm-disks")
		if !errors.Is(err, inspection.ErrClientMaterial) || len(got.Material.Parts()) != 0 || !zeroed(material) {
			t.Fatalf("%s: custody = %+v (%v)", name, got, err)
		}
	}
	if _, found, err := (inspectionCustody{reader: &producedFake{}}).ClientKeyring(context.Background(), "lab", "vm-disks"); found || err != nil {
		t.Fatalf("absent entry = %t (%v)", found, err)
	}
	failure := errors.New("the custody store is unreadable")
	if _, _, err := (inspectionCustody{reader: &producedFake{failed: failure}}).ClientKeyring(context.Background(), "lab", "vm-disks"); !errors.Is(err, failure) {
		t.Fatalf("custody failure = %v", err)
	}
}

func TestCephInspectionReadsEndpointEvidence(t *testing.T) {
	catalog, cluster := inspectedStorageCluster()
	frozen := func(kind, object string, r ceph.Request, digest string) reconciliation.Block {
		r.Version = ceph.Version
		data, err := r.Canonical()
		if err != nil {
			t.Fatal(err)
		}
		return reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{Kind: kind, Object: object, Implementation: ceph.Implementation, Request: data}, RequestDigest: strings.Repeat(digest, 64)}
	}
	mgmtDocument := json.RawMessage(`{"placement":{"hosts":["rhel-01.lab.test"]},"service_type":"mgmt-gateway","spec":{"port":9443,"virtual_ip":"192.0.2.60"}}`)
	rgwDocument := json.RawMessage(`{"placement":{"hosts":["rhel-01.lab.test"]},"service_id":"objects","service_type":"rgw","spec":{"rgw_realm":"lab","rgw_zone":"lab","rgw_zonegroup":"lab","ssl":true}}`)
	mgmt := frozen("StorageCluster", "storage", ceph.Request{Action: "service", Cluster: "storage", Service: &ceph.ServiceUnit{Type: "mgmt-gateway", Documents: []json.RawMessage{mgmtDocument}}}, "a")
	objects := frozen("StorageObjectGateway", "objects", ceph.Request{Action: "gateway", Cluster: "storage", Gateway: &ceph.Gateway{Name: "objects", Endpoint: "s3.lab.test", Documents: []json.RawMessage{rgwDocument}}}, "b")
	foreign := frozen("StorageObjectGateway", "foreign", ceph.Request{Action: "gateway", Cluster: "other", Gateway: &ceph.Gateway{Name: "foreign", Documents: []json.RawMessage{rgwDocument}}}, "c")
	proved := func(block reconciliation.Block, action string) lifecycle.BlockEvidence {
		data, err := json.Marshal(ceph.Evidence{Version: ceph.EvidenceVersion, Request: block.RequestDigest, Action: action, Owner: "apply", FSID: "6b8f1c2e-4d7a-5e3f-9a1b-0c2d4e6f8a1b", Owned: true, Exists: true, Ready: true,
			Health: "HEALTH_OK", Hosts: []string{}, Monitors: []string{}, MGRStandbys: []string{}, MonAddrs: []ceph.MonitorAddress{}, OSDs: []ceph.OSDEvidence{}, Daemons: []ceph.UnitDaemon{},
			Endpoint: ceph.EndpointEvidence{CertificateSHA256: strings.Repeat("d", 64), Reachable: true}})
		if err != nil {
			t.Fatal(err)
		}
		return lifecycle.BlockEvidence{Kind: block.Kind, Object: block.Object, Implementation: block.Implementation, Verb: reconciliation.Apply, State: reconciliation.BlockDone, Evidence: data}
	}
	reader := &readerFake{
		proofs: map[string][]lifecycle.BlockEvidence{
			"StorageCluster/storage":       append(inspectedClusterProofs(t), proved(mgmt, "service")),
			"StorageObjectGateway/objects": {proved(objects, "gateway")},
			"StorageObjectGateway/foreign": {proved(foreign, "gateway")},
		},
		resources: &lifecycle.StorageResourcesResult{Frozen: []reconciliation.Block{mgmt, objects, foreign}},
	}
	proof, err := (cephInspection{reconciler: reader}).ManagedCeph(context.Background(), "lab", catalog, cluster)
	want := []inspection.Endpoint{
		{Service: "mgmt-gateway", URLs: []string{"https://192.0.2.60:9443/"}, CertificateSHA256: strings.Repeat("d", 64), Issuer: inspection.IssuerCephadm, Status: inspection.EndpointReady},
		{Service: "rgw.objects", URLs: []string{"https://s3.lab.test:443/"}, CertificateSHA256: strings.Repeat("d", 64), Issuer: inspection.IssuerCephadm, Status: inspection.EndpointReady},
	}
	if err != nil || !reflect.DeepEqual(proof.Endpoints, want) || !slices.Equal(reader.reads, []string{"StorageCluster/storage", "resources/lab", "StorageObjectGateway/objects"}) {
		t.Fatalf("endpoints = %+v (%v) after reads %v, want %+v", proof.Endpoints, err, reader.reads, want)
	}
	bare := &readerFake{proofs: map[string][]lifecycle.BlockEvidence{"StorageCluster/storage": inspectedClusterProofs(t)}}
	proof, err = (cephInspection{reconciler: bare}).ManagedCeph(context.Background(), "lab", catalog, cluster)
	if err != nil || proof.Endpoints == nil || len(proof.Endpoints) != 0 || proof.FSID == "" {
		t.Fatalf("a missing resources result gave %#v (%v)", proof.Endpoints, err)
	}
}

func inspectedResourceRows(t *testing.T) {
	t.Helper()
	text := func(name, value string) api.FieldValue {
		return api.FieldValue{Name: name, Value: api.StringValue(value)}
	}
	catalog := api.NewCatalog([]api.Object{
		api.NewObject(api.StorageCluster, "ceph", api.Value{}, api.MapValue(text("management", "managed"))),
		api.NewObject(api.StorageCluster, "archive", api.Value{}, api.MapValue(text("management", "external"))),
		api.NewObject(api.StoragePool, "rbd-data", api.Value{}, api.MapValue(text("clusterRef", "ceph"))),
		api.NewObject(api.StorageObjectGateway, "objects", api.Value{}, api.MapValue(text("clusterRef", "ceph"), text("realm", "lab"), text("zoneGroup", "lab"), text("zone", "lab-a"))),
		api.NewObject(api.StorageExport, "foundation", api.Value{}, api.MapValue(text("clusterRef", "ceph"))),
	})
	request, err := ceph.Request{Version: ceph.Version, Action: "pool", Pool: &ceph.Pool{Name: "rbd-data", Size: 2, MinSize: 2, Application: "rbd"}}.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	reader := &readerFake{resources: &lifecycle.StorageResourcesResult{
		Clusters: []lifecycle.ClusterSummary{
			{Name: "archive", Kind: "StorageCluster", Status: lifecycle.RealizationPending, Resources: []lifecycle.ResourceSummary{}},
			{Name: "ceph", Kind: "StorageCluster", Status: lifecycle.RealizationDone, Resources: []lifecycle.ResourceSummary{
				{Kind: "StoragePool", Name: "rbd-data", Status: lifecycle.RealizationDone},
				{Kind: "StorageObjectGateway", Name: "objects", Status: lifecycle.RealizationPending},
				{Kind: "StorageExport", Name: "foundation", Status: lifecycle.RealizationUnsupported},
			}},
		},
		Frozen: []reconciliation.Block{{BlockDefinition: reconciliation.BlockDefinition{Kind: "StoragePool", Object: "rbd-data", Implementation: ceph.Implementation, Request: request}}},
	}}
	rows, err := (cephInspection{reconciler: reader}).Resources(context.Background(), "lab", catalog)
	want := map[string][]inspection.Resource{"ceph": {
		{Kind: "StoragePool", Name: "rbd-data", Status: "done", Scope: "replicated size 2/min 2, application rbd"},
		{Kind: "StorageObjectGateway", Name: "objects", Status: "pending"},
		{Kind: "StorageExport", Name: "foundation", Status: "unsupported", Scope: "export"},
	}}
	if err != nil || !reflect.DeepEqual(rows, want) || !slices.Equal(reader.reads, []string{"resources/lab"}) {
		t.Fatalf("resources = %+v (%v) after reads %v, want %+v", rows, err, reader.reads, want)
	}
}
