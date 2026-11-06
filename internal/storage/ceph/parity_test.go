package ceph

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/storage/cephcluster"
)

func TestAdmittedManagerModulesPlan(t *testing.T) {
	for _, modules := range [][]string{{"cephadm"}, {"rbd_support"}, {"cephadm", "rbd_support"}} {
		catalog := planCatalog(3, 3, func(spec api.Value) api.Value { return spec.With("mgrModules", api.StringList(modules...)) })
		input := lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "synthetic"}, Controller: "controller", State: compilation.NewState(catalog, catalog, nil)}
		if plan, err := New(api.CephCluster, nil).Plan(context.Background(), input); err != nil || len(plan.Definitions) == 0 {
			t.Fatalf("%v: %+v", modules, diagnostics.Of(err))
		}
	}
}

func TestDelegatedPlacementsRefuseDecoratedHosts(t *testing.T) {
	cluster, _ := poolRequestCatalog(3, 3).Find(api.CephCluster, "example")
	for _, family := range []string{"rgw", "nfs", "nvmeof", "mds"} {
		for _, host := range []api.Value{api.StringValue("node-1:192.0.2.0/24"), api.StringValue("node-1=daemon"), requestMap("hostname", "node-1", "network", "192.0.2.0/24"), requestMap("hostname", "node-1", "name", "daemon")} {
			service := requestMap("service_type", family, "service_id", "synthetic", "placement", requestMap("hosts", api.ListValue(host)))
			if _, err := serviceHosts(cluster, service); err == nil || !strings.Contains(diagnostics.Of(err)[0].Message, "placement.hosts") {
				t.Errorf("%s %s: %v", family, host.Text(), err)
			}
			if _, err := unitDocumentOf(cluster, service, func(api.Value) any { return struct{}{} }); err == nil {
				t.Errorf("%s decorated host reached native document", family)
			}
		}
	}
}

func TestRGWExplicitSingleDaemonPerHostPlans(t *testing.T) {
	cluster, _ := poolRequestCatalog(3, 3).Find(api.CephCluster, "example")
	service := requestMap("service_type", "rgw", "service_id", "synthetic", "placement", requestMap("hosts", api.StringList("node-1")))
	document := func(api.Value) any { return struct{}{} }
	implicit, err := unitDocumentOf(cluster, service, document)
	if err != nil {
		t.Fatal(err)
	}
	explicit, err := unitDocumentOf(cluster, service.WithPath(api.IntegerValue("1"), "placement", "count_per_host"), document)
	if err != nil || !bytes.Equal(implicit, explicit) {
		t.Fatalf("single daemon changed document: %s %v", explicit, err)
	}
}

func TestPoolBudgetUsesClusterReplicaDefault(t *testing.T) {
	pool := childOf(api.CephPool, "volumes", "role", "rbd", "replicated", requestMap(), "autoscale", requestMap("pgNumMin", 256))
	catalog := planCatalog(3, 3, withCorePair, pool)
	request, err := poolRequest(catalog, pool, "synthetic")
	if err != nil || request.Pool.Size != 2 {
		t.Fatalf("cluster 2/2 default exceeded budget: %+v %v", request.Pool, err)
	}
}

func TestPolicyRequestRefusesReplicaFloorBypass(t *testing.T) {
	for _, protection := range []api.Value{requestMap("size", 1), requestMap("minSize", 1)} {
		policy := childOf(api.CephPlacementPolicy, "policy", "ruleName", "synthetic", "replicated", protection)
		catalog := planCatalog(4, 4, withCorePair, policy)
		if _, err := policyRequest(catalog, policy, "synthetic"); err == nil {
			t.Fatal("policy with replica floor violation planned")
		}
	}
}

func TestAuthenticatedNVMeOFBindingsReachTheAdapter(t *testing.T) {
	objects := nvmeCatalog().Objects()
	paths := []string{"root_ca_cert", "server_cert", "server_key", "client_cert", "client_key"}
	bindings := []api.Value{}
	for _, path := range paths {
		bindings = append(bindings, requestMap("path", "/spec/"+path, "secretRef", "synthetic-"+strings.Split(path, "_")[0]))
	}
	for i, object := range objects {
		if object.Kind() != api.CephCluster {
			continue
		}
		services := object.Spec().Get("services").Items()
		for j, service := range services {
			if service.Get("service_type").Text() == "nvmeof" {
				services[j] = service.WithPath(api.BoolValue(true), "spec", "enable_auth")
			}
		}
		objects[i] = object.WithSpec(object.Spec().With("services", api.ListValue(services...)).With("serviceBindings", api.ListValue(requestMap("serviceRef", "nvmeof.config.primary", "secrets", api.ListValue(bindings...)))))
	}
	for _, name := range []string{"root", "server", "client"} {
		objects = append(objects, api.NewObject(api.Secret, "synthetic-"+name, api.Value{}, requestMap("type", "tlsCertificate")))
	}
	catalog := api.NewCatalog(objects)
	cluster, _ := catalog.Find(api.CephCluster, "example")
	if issues := cephcluster.ValidatePartial(cluster, catalog); len(issues) != 0 {
		t.Fatalf("auth bindings refused at admission: %+v", issues)
	}
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "synthetic"}, Controller: "controller", State: compilation.NewState(catalog, catalog, nil)}
	for _, kind := range []api.Kind{api.CephCluster, api.CephNVMeoFSubsystem} {
		plan, err := New(kind, nil).Plan(context.Background(), input)
		if err != nil {
			t.Fatalf("%s plan: %+v", kind, diagnostics.Of(err))
		}
		if kind != api.CephCluster {
			continue
		}
		found := false
		for _, block := range plan.Definitions {
			request, err := DecodeRequest(block.Request)
			if err != nil {
				t.Fatal(err)
			}
			if request.Service == nil || request.Service.Type != "nvmeof" {
				continue
			}
			found = true
			if len(request.Service.Bindings) != 5 || !bytes.Contains(request.Service.Documents[0], []byte(`"enable_auth":true`)) {
				t.Fatalf("missing auth intent: %+v", request.Service)
			}
			material := map[string]secrets.Material{}
			for _, binding := range request.Service.Bindings {
				material[binding.SecretRef] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.CertificatePart: []byte("synthetic certificate"), secrets.PrivateKeyPart: []byte("synthetic key")})
			}
			files, err := unitMaterial(lifecycle.Execution{Material: material}, request, "apply")
			if err != nil || len(files) != 5 {
				t.Fatalf("adapter material: %+v %v", files, err)
			}
			for _, path := range paths {
				variable := "nvmeof_" + path
				if !slices.ContainsFunc(files, func(file lifecycle.MaterialFile) bool {
					return file.Variable == variable && file.Secret == "synthetic-"+strings.Split(path, "_")[0]
				}) {
					t.Errorf("adapter omitted %s", variable)
				}
			}
			delete(material, request.Service.Bindings[0].SecretRef)
			if _, err := unitMaterial(lifecycle.Execution{Material: material}, request, "apply"); err == nil {
				t.Fatal("missing TLS binding material was ignored")
			}
		}
		if !found {
			t.Fatal("no NVMe gateway service block")
		}
	}
}

func TestSeparateCephAddressesRefuseByNamedField(t *testing.T) {
	for _, bootstrap := range []bool{true, false} {
		catalog := poolRequestCatalog(3, 3)
		objects := catalog.Objects()
		field := "spec.cephadm.addressRef"
		if bootstrap {
			field = "spec.cephadm.bootstrap.addressRef"
		}
		for i, object := range objects {
			if object.Kind() == api.Machine {
				objects[i] = object.WithSpec(object.Spec().WithPath(api.ListValue(append(object.Spec().Get("network", "addresses").Items(), requestMap("name", "storage", "address", "198.51.100.10"))...), "network", "addresses"))
			}
			if object.Kind() == api.CephCluster {
				spec := object.Spec()
				if bootstrap {
					spec = spec.WithPath(api.StringValue("storage"), "cephadm", "bootstrap", "addressRef")
				} else {
					spec = spec.WithPath(api.StringValue("storage"), "cephadm", "addressRef").WithPath(api.StringValue("ip"), "cephadm", "bootstrap", "addressRef")
				}
				objects[i] = object.WithSpec(spec)
			}
		}
		_, err := planned(t, api.NewCatalog(objects), api.CephCluster, "example")
		issues := diagnostics.Of(err)
		if len(issues) == 0 || issues[0].Code != "lifecycle.unsupported" || !strings.Contains(issues[0].Message, field) {
			t.Errorf("%s refusal: %+v", field, issues)
		}
	}
	catalog, cluster := tiebreakerCatalog(func(machine, cluster api.Value) (api.Value, api.Value) {
		return machine.WithPath(api.ListValue(append(machine.Get("network", "addresses").Items(), requestMap("name", "storage", "address", "198.51.100.11"))...), "network", "addresses"), cluster
	})
	m, _ := catalog.Find(api.Machine, "tie")
	node := cluster.Spec().Get("topology", "nodes").Items()[4]
	r := Request{ClusterSSHUser: "operator", ClusterKeyRef: "cluster-key"}
	err := bindProvidedHost(&r, catalog, m, node, "tie", "node-1", "storage")
	issues := diagnostics.Of(err)
	if len(issues) == 0 || !strings.Contains(issues[0].Message, "spec.cephadm.addressRef") {
		t.Fatalf("provided refusal: %+v", issues)
	}
}

func TestConsumerPlansEveryLongAdmittedName(t *testing.T) {
	for length := 52; length <= 63; length++ {
		name := strings.Repeat("c", length)
		consumer := childOf(api.CephConsumer, name, "dataFoundation", requestMap("rbdPoolRef", "block", "filesystemRef", "files", "cephxKeyType", "aes"))
		catalog := filesystemCatalog(func(spec api.Value) api.Value {
			spec = withMDS(spec)
			return spec.With("services", api.ListValue(append(spec.Get("services").Items(), requestMap("service_type", "prometheus", "placement", requestMap("count", 1)))...))
		}, cephfsPool("block", "rbd"), filesystemOf("subvolumeGroups", api.ListValue(requestMap("name", "csi"))), consumer)
		if issues := cephcluster.Validate(consumer, catalog); len(issues) != 0 {
			t.Fatalf("name length%d: %+v", length, issues)
		}
		plan, err := NewConsumer(nil).Plan(context.Background(), lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "synthetic"}, Controller: "controller", State: compilation.NewState(catalog, catalog, nil)})
		if err != nil || len(plan.Definitions) != 1 {
			t.Fatalf("name length%d: %+v", length, diagnostics.Of(err))
		}
		block := plan.Definitions[0]
		request, err := DecodeConsumerRequest(block.Request)
		if err != nil || request.Name != name || !reconciliation.ValidSegment(block.ID) || len(block.ID) > 63 {
			t.Fatalf("name length%d changed identity: %+v %v", length, request, err)
		}
	}
}

func TestReplicaBudgetPreservesExplicitPolicyAndStretchWidths(t *testing.T) {
	for _, row := range []struct {
		name            string
		size            int
		policy, stretch bool
		want            int64
	}{{"default", 0, false, false, 2}, {"explicit", 3, false, false, 3}, {"policy", 0, true, false, 3}, {"stretch", 0, false, true, 4}} {
		t.Run(row.name, func(t *testing.T) {
			pool := childOf(api.CephPool, "volumes", "role", "rbd", "replicated", requestMap("size", row.size))
			if row.policy {
				pool = pool.WithSpec(pool.Spec().With("placementPolicyRef", api.StringValue("policy")))
			}
			catalog := planCatalog(4, 4, func(spec api.Value) api.Value {
				spec = withCorePair(spec)
				if row.stretch {
					spec = spec.WithPath(requestMap("ruleName", "stretch"), "topology", "stretch")
				}
				return spec
			}, pool, childOf(api.CephPlacementPolicy, "policy", "ruleName", "synthetic", "replicated", requestMap("size", 3)))
			_, _, width, _, ok := placementOf(catalog, pool)
			if !ok || width != row.want {
				t.Fatalf("width=%d want%d", width, row.want)
			}
		})
	}
}
