package ceph

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/storage/cephcluster"
)

func replicationCatalog(mode string) api.Catalog {
	objects := poolRequestCatalog(3, 3).Objects()
	cluster, _ := api.NewCatalog(objects).Find(api.CephCluster, "example")
	nodes := []api.Value{}
	for _, object := range slices.Clone(objects) {
		if object.Kind() != api.Machine {
			continue
		}
		name := "peer-" + object.Name()
		address := strings.ReplaceAll(object.Spec().Get("network", "addresses").Items()[0].Get("address").Text(), "192.0.2.", "198.51.100.")
		spec := object.Spec().WithPath(api.ListValue(requestMap("name", "ip", "address", address)), "network", "addresses")
		objects = append(objects, api.NewObject(api.Machine, name, api.Value{}, spec))
		nodes = append(nodes, requestMap("name", name, "machineRef", name, "labels", api.StringList("mon", "storage")))
	}
	services := append(cluster.Spec().Get("services").Items(), requestMap("service_type", "rbd-mirror", "placement", requestMap("label", "mon", "count", 1)))
	peer := cluster.Spec().WithPath(api.ListValue(nodes...), "topology", "nodes").WithPath(api.StringValue("peer-node-1"), "cephadm", "bootstrap", "nodeRef").WithPath(api.StringList("198.51.100.0/24"), "networks", "publicCIDRs").WithPath(api.ListValue(services...), "services")
	objects = append(objects, api.NewObject(api.CephCluster, "secondary", api.Value{}, peer))
	left := replicatedPool("primary-volumes", "nativeName", "volumes", "mirroring", requestMap("mode", mode))
	right := replicatedPool("secondary-volumes", "nativeName", "volumes", "mirroring", requestMap("mode", mode))
	right = right.WithSpec(right.Spec().With("clusterRef", api.StringValue("secondary")))
	features := append(slices.Clone(defaultFeatures), "journaling")
	image := imageOf("disk", "primary-volumes", "features", api.StringList(features...))
	spec := requestMap("clusterRef", "example", "type", "rbd", "poolRef", left.Name(), "peerClusterRef", "secondary", "peerPoolRef", right.Name())
	if mode == "image" {
		spec = spec.With("imageRefs", api.StringList("disk"))
	}
	return api.NewCatalog(append(objects, left, right, image, api.NewObject(api.CephReplication, "replica", api.Value{}, spec)))
}
func replicationPlan(t *testing.T, catalog api.Catalog) (ReplicationRequest, reconciliation.Block) {
	t.Helper()
	plan, err := NewReplication(nil).Plan(context.Background(), lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "synthetic"}, State: compilation.NewState(catalog, catalog, nil), Controller: "controller"})
	if err != nil || len(plan.Definitions) != 1 {
		t.Fatalf("replication plan: %+v %v", plan, diagnostics.Of(err))
	}
	definition := plan.Definitions[0]
	digest, err := reconciliation.RequestDigest(definition)
	if err != nil {
		t.Fatal(err)
	}
	r, err := DecodeReplicationRequest(definition.Request)
	if err != nil {
		t.Fatal(err)
	}
	return r, reconciliation.Block{BlockDefinition: definition, RequestDigest: digest}
}
func TestReplicationPlansBothClustersAndNativeNames(t *testing.T) {
	for _, mode := range []string{"image", "pool"} {
		t.Run(mode, func(t *testing.T) {
			catalog := replicationCatalog(mode)
			r, block := replicationPlan(t, catalog)
			if r.Mode != mode || r.Primary.Pool.Name != "volumes" || r.Secondary.Pool.Name != "volumes" || r.Primary.PoolObject != "primary-volumes" || r.Secondary.PoolObject != "secondary-volumes" {
				t.Fatalf("wrong endpoints: %+v", r)
			}
			if !slices.Equal(block.Exclusive, []string{"ceph/example", "ceph/secondary"}) {
				t.Fatalf("exclusive claims: %v", block.Exclusive)
			}
			for _, name := range []string{ClusterBlockID("example"), ClusterBlockID("secondary"), ImageBlockID("example"), ImageBlockID("secondary"), ServiceUnitBlockID("secondary", ServiceUnit{Type: "rbd-mirror"})} {
				if !slices.Contains(block.Dependencies, name) {
					t.Errorf("missing dependency %s", name)
				}
			}
			for _, ref := range []reconciliation.ObjectRef{{Kind: string(api.CephPool), Object: "primary-volumes"}, {Kind: string(api.CephPool), Object: "secondary-volumes"}, {Kind: string(api.CephRBDImage), Object: "disk"}} {
				if !slices.Contains(block.Requires, ref) {
					t.Errorf("missing requirement %+v", ref)
				}
			}
			image, _ := catalog.Find(api.CephRBDImage, "disk")
			p, err := New(api.CephRBDImage, nil).Plan(context.Background(), lifecycle.PlanInput{State: compilation.NewState(catalog, catalog, nil), Context: lifecycle.ContextIdentity{Name: "synthetic"}, Controller: "controller"})
			if err != nil || len(p.Definitions) != 1 || p.Definitions[0].Requires[0].Object != image.Spec().Get("poolRef").Text() {
				t.Fatalf("native alias replaced graph identity: %+v %v", p, err)
			}
		})
	}
}
func TestReplicationAdmissionRefusesForeignOrAmbiguousTopology(t *testing.T) {
	changes := map[string]func(api.Object) api.Object{
		"same cluster": func(o api.Object) api.Object {
			return o.WithSpec(o.Spec().With("peerClusterRef", api.StringValue("example")))
		},
		"wrong peer pool": func(o api.Object) api.Object {
			return o.WithSpec(o.Spec().With("peerPoolRef", api.StringValue("primary-volumes")))
		},
		"no images": func(o api.Object) api.Object { return o.WithSpec(o.Spec().With("imageRefs", api.StringList())) },
		"foreign image": func(o api.Object) api.Object {
			return o.WithSpec(o.Spec().With("imageRefs", api.StringList("missing")))
		},
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			catalog := replicationCatalog("image")
			object, _ := catalog.Find(api.CephReplication, "replica")
			if issues := cephcluster.Validate(change(object), catalog); len(issues) == 0 {
				t.Fatal("invalid relationship admitted")
			}
		})
	}
	for _, kind := range []string{"native-name", "mode", "external", "overlap", "journaling", "receiver-images", "bound"} {
		t.Run(kind, func(t *testing.T) {
			catalog := replicationCatalog("image")
			objects := catalog.Objects()
			for i, o := range objects {
				switch {
				case kind == "native-name" && o.Name() == "secondary-volumes":
					objects[i] = o.WithSpec(o.Spec().With("nativeName", api.StringValue("other")))
				case kind == "mode" && o.Name() == "secondary-volumes":
					objects[i] = o.WithSpec(o.Spec().WithPath(api.StringValue("pool"), "mirroring", "mode"))
				case kind == "external" && o.Name() == "secondary":
					objects[i] = o.WithSpec(o.Spec().With("management", api.StringValue("external")))
				case kind == "overlap" && o.Name() == "secondary":
					primary, _ := catalog.Find(api.CephCluster, "example")
					objects[i] = o.WithSpec(o.Spec().WithPath(primary.Spec().Get("topology", "nodes"), "topology", "nodes"))
				case kind == "journaling" && o.Name() == "disk":
					objects[i] = o.WithSpec(o.Spec().With("features", api.StringList(defaultFeatures...)))
				}
			}
			if kind == "receiver-images" {
				objects = append(objects, imageOf("peer-disk", "secondary-volumes").WithSpec(requestMap("clusterRef", "secondary", "poolRef", "secondary-volumes", "sizeBytes", 1048576)))
			}
			if kind == "bound" {
				image, _ := catalog.Find(api.CephRBDImage, "disk")
				for i := 1; i <= 3; i++ {
					objects = append(objects, api.NewObject(api.CephRBDImage, fmt.Sprintf("disk-%d", i), api.Value{}, image.Spec()))
				}
				for i, o := range objects {
					if o.Kind() == api.CephReplication {
						objects[i] = o.WithSpec(o.Spec().With("imageRefs", api.StringList("disk", "disk-1", "disk-2", "disk-3")))
					}
				}
			}
			changed := api.NewCatalog(objects)
			object, _ := changed.Find(api.CephReplication, "replica")
			if issues := cephcluster.Validate(object, changed); len(issues) == 0 {
				t.Fatal("invalid relationship admitted")
			}
		})
	}
}
func replicationExecution(t *testing.T) (ReplicationRequest, lifecycle.Execution) {
	t.Helper()
	catalog := replicationCatalog("image")
	r, block := replicationPlan(t, catalog)
	e := lifecycle.Execution{Block: block, ApplyOperation: "apply-original", Operation: "apply-retry", Context: "synthetic"}
	for _, name := range []string{"example", "secondary"} {
		cluster, _ := catalog.Find(api.CephCluster, name)
		base, err := baseRequest(catalog, cluster, "synthetic")
		if err != nil {
			t.Fatal(err)
		}
		base.Action = "cluster"
		base.Controller = "controller"
		e.ApplyProved = append(e.ApplyProved, coreClusterProof(t, base, func(e *Evidence) {
			e.MGRActive = base.Hosts[0].Name
			e.MGRStandbys = []string{base.Hosts[1].Name}
			e.Dashboard.URL = "https://" + base.Hosts[0].Name + ":8443/"
			for i := range e.MonAddrs {
				e.MonAddrs[i].Address = base.Hosts[i].Address
			}
		}), imageProof(t, name, base.Image, resolvedDigest))
	}
	return r, e
}
func TestReplicationCompletesFromBothOriginalClusterProofs(t *testing.T) {
	r, e := replicationExecution(t)
	completed, scopes, err := NewReplication(nil).prepare(context.Background(), e)
	if err != nil || len(scopes) != 2 {
		t.Fatalf("completed scopes: %d %v", len(scopes), diagnostics.Of(err))
	}
	for i, end := range []ReplicationEndpoint{completed.Primary, completed.Secondary} {
		if len(end.Scope) == 0 || scopes[i].Owner != "apply-original" || scopes[i].FSID != FSIDFor("apply-original", end.Cluster) {
			t.Fatalf("retry replaced original identity: %+v", end)
		}
	}
	for _, edit := range []func(*lifecycle.Execution){func(e *lifecycle.Execution) { e.ApplyProved = e.ApplyProved[:2] }, func(e *lifecycle.Execution) { e.ApplyOperation = "apply-other" }, func(e *lifecycle.Execution) { e.ApplyProved = append(e.ApplyProved, e.ApplyProved[2]) }} {
		changed := e
		changed.ApplyProved = slices.Clone(e.ApplyProved)
		edit(&changed)
		if _, _, err := NewReplication(nil).prepare(context.Background(), changed); err == nil {
			t.Fatal("unproved endpoint reached execution")
		}
	}
	data, _ := r.Canonical()
	decoded, err := DecodeReplicationRequest(data)
	if err != nil || !reflect.DeepEqual(r, decoded) {
		t.Fatal("frozen round trip changed")
	}
	completedBytes, _ := completed.Canonical()
	if _, err := DecodeReplicationRequest(completedBytes); err == nil {
		t.Fatal("runtime scopes admitted as canonical request")
	}
	machines, err := NewReplication(nil).HostMachines(e.Block)
	if err != nil || len(machines) != 6 {
		t.Fatalf("retirement machines: %v %v", machines, err)
	}
	if _, err := NewReplication(nil).Destroy(context.Background(), e); err == nil {
		t.Fatal("surviving hosts allowed removal")
	}
	e.RetiredWithHosts = true
	if result, err := NewReplication(nil).Destroy(context.Background(), e); err != nil || result.Outcome != reconciliation.OutcomeChanged {
		t.Fatalf("retirement: %+v %v", result, err)
	}
}
func TestReplicationEvidenceAndBudget(t *testing.T) {
	r, e := replicationExecution(t)
	ready := ReplicationEvidence{Version: ReplicationEvidenceVersion, Request: e.Block.RequestDigest, Owner: e.ApplyOperation, Object: r.Object, PrimaryFSID: FSIDFor(e.ApplyOperation, r.Primary.Cluster), SecondaryFSID: FSIDFor(e.ApplyOperation, r.Secondary.Cluster), Owned: true, Exists: true, Ready: true, Images: []ReplicatedImageEvidence{{Name: "disk", GlobalID: FSIDFor("image", "disk"), PrimaryID: "a1", SecondaryID: "b1", Synced: true}}}
	data, _ := json.Marshal(ready)
	if effect, err := EvaluateReplicationEvidence(data, r, e.ApplyOperation, e.Block.RequestDigest); err != nil || effect != reconciliation.EffectCompleted {
		t.Fatalf("ready: %s %v", effect, err)
	}
	for _, edit := range []func(*ReplicationEvidence){func(e *ReplicationEvidence) { e.SecondaryFSID = e.PrimaryFSID }, func(e *ReplicationEvidence) { e.Images[0].SecondaryID = "" }, func(e *ReplicationEvidence) { e.Images[0].Synced = false }, func(e *ReplicationEvidence) { e.Owner = "apply-other" }, func(e *ReplicationEvidence) { e.Owned = false }} {
		wrong := ready
		wrong.Images = slices.Clone(ready.Images)
		edit(&wrong)
		data, _ := json.Marshal(wrong)
		if _, err := EvaluateReplicationEvidence(data, r, e.ApplyOperation, e.Block.RequestDigest); err == nil {
			t.Fatal("false completion accepted")
		}
	}
	for count := 1; count <= 3; count++ {
		r.Images = make([]RBDImage, count)
		if got := replicationDeadline(r, "apply"); got != time.Duration((8+3*count)*660+1860+60)*time.Second || got > 6*time.Hour {
			t.Fatalf("apply budget %s", got)
		}
		if got := replicationDeadline(r, "observe"); got != time.Duration((2+2*count)*660+60)*time.Second {
			t.Fatalf("observe budget %s", got)
		}
	}
}

func TestReplicationRequestGolden(t *testing.T) {
	_, block := replicationPlan(t, replicationCatalog("image"))
	matchesGolden(t, "request-replication", block.Request, false)
}

func TestReplicationMaterialSelectsEachBootstrapHostsKey(t *testing.T) {
	r := coreRequest()
	e := withMaterial(executionFor(t, r), r)
	scopes := []ClusterScope{scopeOf(r), scopeOf(r)}
	for i := range scopes {
		scopes[i].Hosts = slices.Clone(scopes[i].Hosts)
		scopes[i].Bootstrap = scopes[i].Hosts[i+1].Name
		for j := range scopes[i].Hosts {
			scopes[i].Hosts[j].SSHKeyRef = fmt.Sprintf("endpoint-%d-host-%d-key", i, j)
		}
	}
	_, files, cleanup, err := replicationMaterial(e, scopes, dependencyProofs{})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	for i, variable := range []string{"primarySSHKey", "secondarySSHKey"} {
		want := fmt.Sprintf("endpoint-%d-host-%d-key", i, i+1)
		if files[i].Variable != variable || files[i].Secret != want {
			t.Errorf("%s uses %q, want the bootstrap host's %q", variable, files[i].Secret, want)
		}
	}
}
