package ceph

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func TestCoreConfigIsFrozenAcrossClusterChildrenAndChangedIntent(t *testing.T) {
	catalog := poolRequestCatalog(3, 3)
	cluster, _ := catalog.Find(api.StorageCluster, "example")
	config := requestMap("osd", requestMap("osd_memory_target_autotune", "false", "osd_memory_target", "2147483648"),
		"global", requestMap("osd_pool_default_size", "2", "osd_pool_default_min_size", "2"))
	changed := cluster.WithSpec(cluster.Spec().WithPath(config, "ceph", "config"))
	objects := catalog.Objects()
	for i, object := range objects {
		if object.Identity() == changed.Identity() {
			objects[i] = changed
		}
	}
	pool := api.NewObject(api.StoragePool, "volumes", api.Value{}, requestMap("clusterRef", "example", "type", "replicated", "role", "rbd", "replicated", requestMap("size", 2, "minSize", 2)))
	objects = append(objects, pool)
	catalog = api.NewCatalog(objects)
	client := api.NewObject(api.StorageClient, "consumer", api.Value{}, requestMap("clusterRef", "example", "poolRef", "volumes", "access", "read-write"))
	for _, object := range []api.Object{changed, pool, client} {
		r, err := New(object.Kind(), nil).request(catalog, object, "synthetic")
		if err != nil || r.Config["osd"]["osd_memory_target"] != "2147483648" || r.Config["osd"]["osd_memory_target_autotune"] != "false" ||
			r.Config["global"]["osd_pool_default_size"] != "2" || r.Config["global"]["osd_pool_default_min_size"] != "2" {
			t.Fatalf("%s lost frozen config: %+v %v", object.Identity(), r.Config, err)
		}
		original, err := r.Canonical()
		if err != nil {
			t.Fatal(err)
		}
		r.Config["osd"]["osd_memory_target"] = "4294967296"
		next, err := r.Canonical()
		if err != nil || bytes.Equal(original, next) {
			t.Fatal("changed memory target did not change frozen request")
		}
		old := bytes.Replace(original, []byte(`"version":"ceph-core-v6"`), []byte(`"version":"ceph-core-v5"`), 1)
		if _, err := DecodeRequest(old); err == nil {
			t.Fatal("old frozen request silently migrated")
		}
		if !changed.Spec().Get("ceph", "config").Equal(config) {
			t.Fatal("request mutation changed authored config")
		}
	}
	bad := changed.WithSpec(changed.Spec().WithPath(api.StringValue("true"), "ceph", "config", "osd", "osd_memory_target_autotune"))
	if _, err := baseRequest(catalog, bad, "synthetic"); err == nil {
		t.Fatal("compiler bypassed config admission")
	}
}

func requestMap(pairs ...any) api.Value {
	fields := []api.FieldValue{}
	for i := 0; i < len(pairs); i += 2 {
		var value api.Value
		switch v := pairs[i+1].(type) {
		case api.Value:
			value = v
		case string:
			value = api.StringValue(v)
		case int:
			value = api.IntegerValue(fmt.Sprint(v))
		case bool:
			value = api.BoolValue(v)
		default:
			panic("unsupported request fixture value")
		}
		fields = append(fields, api.FieldValue{Name: pairs[i].(string), Value: value})
	}
	return api.MapValue(fields...)
}

func TestClusterSSHTransportIsFrozenForClusterAndChildren(t *testing.T) {
	for _, transport := range []string{"", "native", "verified-tunnel", "unknown"} {
		t.Run(transport, func(t *testing.T) {
			catalog := poolRequestCatalog(3, 3)
			cluster, _ := catalog.Find(api.StorageCluster, "example")
			if transport != "" {
				cluster = cluster.WithSpec(cluster.Spec().WithPath(api.StringValue(transport), "ceph", "cephadm", "clusterSSH", "transport"))
			}
			objects := catalog.Objects()
			for i := range objects {
				if objects[i].Identity() == cluster.Identity() {
					objects[i] = cluster
				}
			}
			pool := api.NewObject(api.StoragePool, "volumes", api.Value{}, requestMap("clusterRef", "example", "type", "replicated", "role", "rbd", "replicated", requestMap("size", 2, "minSize", 2)))
			objects = append(objects, pool)
			catalog = api.NewCatalog(objects)
			client := api.NewObject(api.StorageClient, "consumer", api.Value{}, requestMap("clusterRef", "example", "poolRef", "volumes", "access", "read-write"))
			for _, object := range []api.Object{cluster, pool, client} {
				r, err := New(object.Kind(), nil).request(catalog, object, "synthetic")
				if transport == "unknown" {
					if err == nil {
						t.Fatal("compiler accepted unknown transport")
					}
					continue
				}
				want := transport
				if want == "" {
					want = "native"
				}
				if err != nil || r.ClusterSSHTransport != want {
					t.Fatalf("%s transport=%s err=%v", object.Identity(), r.ClusterSSHTransport, err)
				}
				before, err := r.Canonical()
				if err != nil {
					t.Fatal(err)
				}
				r.ClusterSSHTransport = "provider-default"
				after, err := r.Canonical()
				if err != nil || bytes.Equal(before, after) {
					t.Fatal("transport change did not change frozen intent")
				}
			}
		})
	}
}

func poolRequestCatalog(hostCount, osdHosts int) api.Catalog {
	nodes := []api.Value{}
	objects := []api.Object{}
	for i := 1; i <= hostCount; i++ {
		name := fmt.Sprintf("node-%d", i)
		labels := []string{"mon"}
		if i <= osdHosts {
			labels = append(labels, "storage")
		}
		nodes = append(nodes, requestMap("name", name, "machineRef", name, "labels", api.StringList(labels...)))
		objects = append(objects, api.NewObject(api.Machine, name, api.Value{}, requestMap(
			"substrate", requestMap("providerRef", "hypervisor", "profileRef", "guest"),
			"os", requestMap("provided", false, "installProfileRef", "rhel"),
			"network", requestMap("addresses", api.ListValue(requestMap("name", "ip", "address", fmt.Sprintf("192.0.2.%d", i)))),
			"access", requestMap("ssh", requestMap("addressRef", "ip", "user", "operator", "auth", requestMap("privateKeyRef", "fleet-key"))),
		)))
	}
	objects = append(objects,
		api.NewObject(api.InfraProvider, "hypervisor", api.Value{}, requestMap("libvirt", requestMap("machineProfiles", api.ListValue(requestMap("name", "guest", "dataDisks", api.ListValue(requestMap("name", "data", "sizeGiB", 1))))))),
		api.NewObject(api.Entitlement, "ibm", api.Value{}, requestMap("license", requestMap("accept", true), "registry", requestMap("url", PinnedRegistry, "credentialsRef", "registry"))),
		api.NewObject(api.StorageCluster, "example", api.Value{}, requestMap("ceph", requestMap(
			"distribution", "ibm", "release", "9.9.1.2", "packageVersion", "20.2.1-405.el9cp", "entitlementRef", "ibm",
			"image", requestMap("base", PinnedRepository, "version", PinnedTag),
			"ibm", requestMap("callHome", "enabled"),
			"cephadm", requestMap("ansible", requestMap("packageVersion", "5.0.3-1"), "clusterSSH", requestMap("user", "operator", "keyRef", "cluster-key"), "bootstrap", requestMap("node", "node-1")),
			"networks", requestMap("publicCIDRs", api.StringList("192.0.2.0/24")),
			"topology", requestMap("nodes", api.ListValue(nodes...)),
			"services", api.ListValue(
				requestMap("service_type", "mon", "placement", requestMap("label", "mon", "count", 3)),
				requestMap("service_type", "mgr", "placement", requestMap("label", "mon", "count", 2)),
				requestMap("service_type", "osd", "service_id", "data", "placement", requestMap("label", "storage"), "spec", requestMap("data_devices", requestMap("paths", api.StringList("/dev/vdb")))),
			),
		))),
	)
	return api.NewCatalog(objects)
}

func TestCorePoolUsesProviderReplicationCapacityAndDefaults(t *testing.T) {
	for _, test := range []struct {
		name              string
		hosts, osdHosts   int
		replicated        api.Value
		wantSize, wantMin int
		refusal           string
	}{
		{"three hosts with explicit two replicas", 3, 3, requestMap("size", 2, "minSize", 2), 2, 2, ""},
		{"omitted minimum retains two", 3, 3, requestMap("size", 2), 2, 2, ""},
		{"three replicas need a fourth OSD host", 3, 3, requestMap("size", 3, "minSize", 2), 0, 0, "OSD-bearing hosts"},
		{"omitted size remains three", 3, 3, api.Value{}, 0, 0, "OSD-bearing hosts"},
		{"zero size remains three", 3, 3, requestMap("size", 0, "minSize", 0), 0, 0, "OSD-bearing hosts"},
		{"four OSD hosts support default three", 4, 4, api.Value{}, 3, 2, ""},
		{"four OSD hosts support zero defaults", 4, 4, requestMap("size", 0, "minSize", 0), 3, 2, ""},
		{"monitor-only host supplies no extra OSD capacity", 4, 3, requestMap("size", 3, "minSize", 2), 0, 0, "OSD-bearing hosts"},
		{"five OSD hosts support four replicas", 5, 5, requestMap("size", 4, "minSize", 2), 4, 2, ""},
		{"minimum one remains unsafe", 3, 3, requestMap("size", 2, "minSize", 1), 0, 0, "minSize between two and size"},
		{"minimum cannot exceed replicas", 4, 4, requestMap("size", 2, "minSize", 3), 0, 0, "minSize between two and size"},
	} {
		t.Run(test.name, func(t *testing.T) {
			catalog := poolRequestCatalog(test.hosts, test.osdHosts)
			spec := requestMap("clusterRef", "example", "type", "replicated", "role", "rbd")
			if test.replicated.Present() {
				spec = spec.With("replicated", test.replicated)
			}
			pool := api.NewObject(api.StoragePool, "volumes", api.Value{}, spec)
			request, err := poolRequest(catalog, pool, "synthetic")
			if test.refusal != "" {
				issues := diagnostics.Of(err)
				if len(issues) != 1 || issues[0].Code != "api.invariant" || !strings.Contains(issues[0].Message, test.refusal) || issues[0].Remediation == "" {
					t.Fatalf("missing actionable capacity refusal: %v %+v", err, issues)
				}
				return
			}
			if err != nil || request.Pool == nil || request.Pool.Size != test.wantSize || request.Pool.MinSize != test.wantMin {
				t.Fatalf("pool request=%+v err=%v", request.Pool, err)
			}
		})
	}
}

func TestDisabledCallHomePlansAndChangesOnlyItsOwnField(t *testing.T) {
	type planner func(context.Context, lifecycle.PlanInput) (lifecycle.CapabilityPlan, error)
	plan := func(callHome string) (Request, map[string][]byte, []byte) {
		catalog := pinnedCatalog(t, func(spec api.Value) api.Value {
			return spec.WithPath(api.StringValue(callHome), "ceph", "ibm", "callHome")
		})
		input := lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "synthetic"}, State: compilation.NewState(catalog, catalog, nil), Controller: "controller"}
		blocks := map[string][]byte{}
		for name, plan := range map[string]planner{"cluster": New(api.StorageCluster, nil).Plan, "host": NewHost(nil).Plan, "image": NewImage(nil).Plan} {
			planned, err := plan(context.Background(), input)
			if err != nil || len(planned.Definitions) == 0 {
				t.Fatalf("%s did not plan with callHome %s: %+v %v", name, callHome, planned, err)
			}
			for _, definition := range planned.Definitions {
				blocks[name+"/"+definition.ID] = definition.Request
			}
		}
		object, _ := catalog.Find(api.StorageCluster, "example")
		cluster, err := baseRequest(catalog, object, "synthetic")
		if err != nil {
			t.Fatal(err)
		}
		sources, err := packageSources(catalog, cluster, "synthetic")
		if err != nil {
			t.Fatal(err)
		}
		packages, err := json.Marshal(sources)
		if err != nil {
			t.Fatal(err)
		}
		return cluster, blocks, packages
	}
	disabled, disabledBlocks, disabledPackages := plan("disabled")
	enabled, enabledBlocks, enabledPackages := plan("enabled")
	if disabled.CallHome != "disabled" || enabled.CallHome != "enabled" {
		t.Fatalf("frozen callHome: disabled=%q enabled=%q", disabled.CallHome, enabled.CallHome)
	}
	if !bytes.Equal(disabledPackages, enabledPackages) {
		t.Fatal("package request moved with callHome")
	}
	if len(disabledBlocks) != len(enabledBlocks) || len(disabledBlocks) < 3 {
		t.Fatalf("block sets differ: %d vs %d", len(disabledBlocks), len(enabledBlocks))
	}
	want, have := []byte(`"callHome":"enabled"`), []byte(`"callHome":"disabled"`)
	for id, request := range disabledBlocks {
		other, ok := enabledBlocks[id]
		if !ok {
			t.Fatalf("block %s missing when enabled", id)
		}
		if strings.HasPrefix(id, "image/") {
			if !bytes.Equal(request, other) {
				t.Fatalf("%s request moved with callHome", id)
			}
			continue
		}
		if bytes.Count(request, have) != 1 || bytes.Count(other, want) != 1 || !bytes.Equal(bytes.Replace(request, have, want, 1), other) {
			t.Fatalf("%s differs beyond its single callHome field", id)
		}
	}
}
