package ceph

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

func managedExportPlan(t *testing.T, gateway bool, edits ...func(api.Value) api.Value) reconciliation.BlockDefinition {
	t.Helper()
	refs := requestMap("rbdPoolRef", "block", "filesystemRef", "files", "cephxKeyType", "aes")
	children := []api.Object{cephfsPool("block", "rbd"), filesystemOf("subvolumeGroups", api.ListValue(requestMap("name", "csi")))}
	if gateway {
		refs = refs.With("objectGatewayRef", api.StringValue("objects"))
		children = append(children, objectGateway("objects", "endpoint", requestMap("fqdn", "objects.example.test")))
	}
	children = append(children, childOf(api.CephConsumer, "consumer", "dataFoundation", refs))
	change := func(spec api.Value) api.Value {
		spec = withMDS(spec)
		services := append(spec.Get("services").Items(), requestMap("service_type", "prometheus", "placement", requestMap("count", 1)))
		spec = spec.WithPath(api.ListValue(services...), "services")
		if gateway {
			spec = withPairAndHTTPRGW(spec)
			services := spec.Get("services").Items()
			for i, service := range services {
				if service.Get("service_type").Text() == "rgw" {
					services[i] = service.WithPath(api.IntegerValue("3"), "placement", "count")
				}
			}
			spec = spec.WithPath(api.ListValue(services...), "services")
		}
		for _, edit := range edits {
			spec = edit(spec)
		}
		return spec
	}
	catalog := filesystemCatalog(change, children...)
	plan, err := NewConsumer(nil).Plan(context.Background(), lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "synthetic"}, Controller: "controller", State: compilation.NewState(catalog, catalog, nil)})
	if err != nil || len(plan.Definitions) != 1 {
		t.Fatalf("export plan: %+v %v %v", plan, err, diagnostics.Of(err))
	}
	return plan.Definitions[0]
}
func TestExportRequestGolden(t *testing.T) {
	for _, gateway := range []bool{false, true} {
		block := managedExportPlan(t, gateway)
		name := "request-export"
		if gateway {
			name += "-gateway"
		}
		matchesGolden(t, name, block.Request, false)
		r, err := DecodeConsumerRequest(block.Request)
		if err != nil {
			t.Fatal(err)
		}
		if block.ID != "ceph-consumer-consumer" || block.Implementation != ConsumerImplementation || !slices.Equal(block.Exclusive, []string{"ceph-mgr/example", "ceph/example"}) {
			t.Fatalf("wrong block: %+v", block)
		}
		for _, ref := range []reconciliation.ObjectRef{{Kind: string(api.CephCluster), Object: r.Cluster}, {Kind: string(api.CephPool), Object: "block"}, {Kind: string(api.CephFilesystem), Object: "files"}} {
			if !slices.Contains(block.Requires, ref) {
				t.Fatalf("missing prerequisite %+v", ref)
			}
		}
		if !slices.Contains(block.Dependencies, "ceph-service-example-monitoring") {
			t.Fatal("monitoring not required")
		}
	}
}

func TestExportDeclaresProducedOutputOnlyForItsFrozenIdentity(t *testing.T) {
	block := reconciliation.Block{BlockDefinition: managedExportPlan(t, false)}
	outputs, err := NewConsumer(nil).ProducedOutputs(block)
	if err != nil || !slices.Equal(outputs, []string{ConsumerOutput}) {
		t.Fatalf("produced outputs: %v %v", outputs, err)
	}
	for _, mutate := range []func(*reconciliation.Block){
		func(b *reconciliation.Block) { b.ID = "other" },
		func(b *reconciliation.Block) { b.Object = "other" },
		func(b *reconciliation.Block) { b.Kind = string(api.CephClient) },
		func(b *reconciliation.Block) { b.Implementation = Implementation },
		func(b *reconciliation.Block) { b.Request = json.RawMessage(`{}`) },
	} {
		wrong := block
		mutate(&wrong)
		if outputs, err := NewConsumer(nil).ProducedOutputs(wrong); err == nil || len(outputs) != 0 {
			t.Fatal("foreign or malformed block declared produced output")
		}
	}
}
func TestExportEvidenceGolden(t *testing.T) {
	block := managedExportPlan(t, false)
	r, err := DecodeConsumerRequest(block.Request)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for name := range r.Monitors {
		names = append(names, name)
	}
	slices.Sort(names)
	leader := names[0]
	address := r.Monitors[leader]
	principals := map[string]map[string]string{}
	for suffix, caps := range r.Principals {
		principals["client."+ConsumerPrincipalPrefix("apply-test", r.Name)+"-"+suffix] = caps
	}
	e := ConsumerEvidence{Leader: leader, Address: address, Entries: exportEntries(r, "apply-test"), Principals: principals, KeyType: r.KeyType, Version: ConsumerEvidenceVersion, Name: r.Name, Owner: "apply-test", FSID: FSIDFor("apply-test", r.Cluster), Request: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Ready: true}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "evidence-export", data, false)
	if effect, err := EvaluateExportEvidence(data, r, e.Owner, e.Request); err != nil || effect != reconciliation.EffectCompleted {
		t.Fatalf("evidence: %s %v", effect, err)
	}
	for _, mutate := range []func(*ConsumerEvidence){func(e *ConsumerEvidence) { e.Owner = "other" }, func(e *ConsumerEvidence) { e.FSID = FSIDFor("other", r.Cluster) }, func(e *ConsumerEvidence) { e.Name = "other" }, func(e *ConsumerEvidence) { e.Request = "other" }} {
		wrong := e
		mutate(&wrong)
		raw, _ := json.Marshal(wrong)
		if _, err := EvaluateExportEvidence(raw, r, e.Owner, e.Request); err == nil {
			t.Fatal("foreign evidence accepted")
		}
	}
}
func withNodeFQDNs(spec api.Value) api.Value {
	nodes := spec.Get("topology", "nodes").Items()
	for i, node := range nodes {
		nodes[i] = node.WithPath(api.StringValue(node.Get("name").Text()+".example.test"), "fqdn")
	}
	return spec.WithPath(api.ListValue(nodes...), "topology", "nodes")
}

// cephadm names each MON after its host's short name, which is what
// quorum_leader_name reports, so an FQDN-declared node's monitor is keyed by
// the part of its hostname before the first dot.
func TestExportKeysMonitorsByTheirMONIDForFQDNNodes(t *testing.T) {
	block := managedExportPlan(t, true, withNodeFQDNs)
	r, err := DecodeConsumerRequest(block.Request)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for name := range r.Monitors {
		names = append(names, name)
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"node-1", "node-2", "node-3"}) {
		t.Fatalf("monitors keyed by %v, want the MON IDs node-1..node-3", names)
	}
	owner := "apply-test"
	principals := map[string]map[string]string{}
	for suffix, caps := range r.Principals {
		principals["client."+ConsumerPrincipalPrefix(owner, r.Name)+"-"+suffix] = caps
	}
	e := ConsumerEvidence{Leader: "node-1", Address: r.Monitors["node-1"], Entries: exportEntries(r, owner), Principals: principals, KeyType: r.KeyType, RGWUser: ConsumerPrincipalPrefix(owner, r.Name) + "-ops", Version: ConsumerEvidenceVersion, Name: r.Name, Owner: owner, FSID: FSIDFor(owner, r.Cluster), Request: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Ready: true}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	if effect, err := EvaluateExportEvidence(data, r, owner, e.Request); err != nil || effect != reconciliation.EffectCompleted {
		t.Fatalf("evidence naming MON node-1: %s %v", effect, err)
	}
	produced := detailsProduced(t, syntheticDetails(r, owner))
	defer lifecycle.ClearProduced(produced)
	if err := validateExportProduced(produced, r, owner); err != nil {
		t.Fatalf("details naming MON node-1: %v", err)
	}
}

func TestExportRefusesMonitorShortNameCollisions(t *testing.T) {
	collide := func(spec api.Value) api.Value {
		nodes := spec.Get("topology", "nodes").Items()
		for i, node := range nodes {
			nodes[i] = node.WithPath(api.StringValue("ceph."+node.Get("name").Text()+".example.test"), "fqdn")
		}
		return spec.WithPath(api.ListValue(nodes...), "topology", "nodes")
	}
	refs := requestMap("rbdPoolRef", "block", "filesystemRef", "files", "cephxKeyType", "aes")
	children := []api.Object{cephfsPool("block", "rbd"), filesystemOf("subvolumeGroups", api.ListValue(requestMap("name", "csi"))), childOf(api.CephConsumer, "consumer", "dataFoundation", refs)}
	change := func(spec api.Value) api.Value {
		spec = withMDS(spec)
		services := append(spec.Get("services").Items(), requestMap("service_type", "prometheus", "placement", requestMap("count", 1)))
		return collide(spec.WithPath(api.ListValue(services...), "services"))
	}
	catalog := filesystemCatalog(change, children...)
	_, err := NewConsumer(nil).Plan(context.Background(), lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "synthetic"}, Controller: "controller", State: compilation.NewState(catalog, catalog, nil)})
	refusedWith(t, err, "api.invariant", "Ceph consumer requires monitor hosts whose short names are unique", "declare monitor hosts whose hostnames differ before the first dot")
}

func TestExportRequestRejectsExpandedCapsAndRuntimeScope(t *testing.T) {
	block := managedExportPlan(t, false)
	r, _ := DecodeConsumerRequest(block.Request)
	r.Principals["rbd-node"]["mon"] = "allow *"
	data, _ := r.Canonical()
	if _, err := DecodeConsumerRequest(data); err == nil {
		t.Fatal("widened caps accepted")
	}
	r, _ = DecodeConsumerRequest(block.Request)
	r.Scope = json.RawMessage(`{}`)
	data, _ = r.Canonical()
	if _, err := DecodeConsumerRequest(data); err == nil {
		t.Fatal("runtime scope frozen")
	}
}
func TestExportSurvivingHostRemovalRefuses(t *testing.T) {
	block := managedExportPlan(t, false)
	e := lifecycle.Execution{Block: reconciliation.Block{BlockDefinition: block}, ApplyOperation: "apply-test"}
	if _, err := NewConsumer(nil).Destroy(context.Background(), e); err == nil {
		t.Fatal("surviving hosts permitted")
	}
	if _, err := NewConsumer(nil).ObserveRemoval(context.Background(), e); err == nil {
		t.Fatal("surviving observation permitted")
	}
}

func TestExportGrantGolden(t *testing.T) {
	type shown struct {
		Plan  []string `json:"plan"`
		Scope string   `json:"scope"`
	}
	views := map[string]shown{}
	for name, gateway := range map[string]bool{"filesystem": false, "gateway": true} {
		block := managedExportPlan(t, gateway)
		refs := requestMap("rbdPoolRef", "block", "filesystemRef", "files")
		if gateway {
			refs = refs.With("objectGatewayRef", api.StringValue("objects"))
		}
		declared := api.NewCatalog([]api.Object{childOf(api.CephConsumer, "consumer", "dataFoundation", refs)})
		views[name] = shown{Plan: block.Impacts, Scope: ResourceScope(declared, api.CephConsumer, "consumer", nil)}
	}
	data, err := json.Marshal(views)
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "export-grant", data, false)
}

func syntheticDetails(r ConsumerRequest, owner string) []exportEntry {
	prefix := ConsumerPrincipalPrefix(owner, r.Name)
	keyBytes := make([]byte, 28)
	keyBytes[0] = 1
	keyBytes[10] = 16
	key := base64.StdEncoding.EncodeToString(keyBytes)
	entries := []exportEntry{}
	for name, kind := range exportEntries(r, owner) {
		entries = append(entries, exportEntry{Name: name, Kind: kind, Data: map[string]string{}})
	}
	for i := range entries {
		entry := &entries[i]
		switch entry.Name {
		case "external-cluster-user-command":
			entry.Data = map[string]string{"args": "bootwright-managed-export"}
		case "rook-ceph-mon-endpoints":
			entry.Data = map[string]string{"data": "node-1=" + r.Monitors["node-1"] + ":3300", "maxMonId": "0", "mapping": "{}"}
		case "rook-ceph-mon":
			entry.Data = map[string]string{"admin-secret": "admin-secret", "mon-secret": "mon-secret", "fsid": FSIDFor(owner, r.Cluster)}
		case "monitoring-endpoint":
			entry.Data = map[string]string{"MonitoringEndpoint": strings.Join(r.MonitoringHosts, ","), "MonitoringPort": strconv.Itoa(r.MonitoringPort)}
		case "ceph-rbd", "cephfs":
			kind := "rbd"
			entry.Data["pool"] = r.Pool
			if entry.Name == "cephfs" {
				kind = "cephfs"
				entry.Data["pool"] = r.DefaultDataPool
				entry.Data["fsName"] = r.Filesystem
			}
			for field, role := range map[string]string{"provisioner": "provisioner", "controller-expand": "provisioner", "node-stage": "node"} {
				entry.Data["csi.storage.k8s.io/"+field+"-secret-name"] = "rook-csi-" + kind + "-" + role + "-" + prefix
			}
		case "ceph-rgw":
			entry.Data = map[string]string{"endpoint": fmt.Sprintf("%s:%d", r.Gateway.FQDN, r.Gateway.Port), "poolPrefix": r.Gateway.Zone}
		case "rgw-admin-ops-user":
			entry.Data = map[string]string{"accessKey": "synthetic-access", "secretKey": "synthetic-secret"}
		case "ceph-rgw-tls-cert":
			entry.Data = map[string]string{"cert": "synthetic-certificate"}
		default:
			id := map[string]string{"rook-ceph-operator-creds": "client." + prefix + "-health", "rook-csi-rbd-node-" + prefix: prefix + "-rbd-node", "rook-csi-rbd-provisioner-" + prefix: prefix + "-rbd-prov",
				"rook-csi-cephfs-node-" + prefix: prefix + "-fs-node", "rook-csi-cephfs-provisioner-" + prefix: prefix + "-fs-prov"}[entry.Name]
			entry.Data = map[string]string{"userID": id, "userKey": key}
		}
	}
	slices.SortFunc(entries, func(a, b exportEntry) int { return strings.Compare(a.Name, b.Name) })
	return entries
}
func detailsProduced(t *testing.T, entries []exportEntry) []lifecycle.Produced {
	t.Helper()
	data, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	return []lifecycle.Produced{{Name: ConsumerOutput, Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: data})}}
}

// ocs-operator's snapshot classes pick the details Secret whose name contains
// rook-csi-<rbd|cephfs>-provisioner, so the four CSI Secrets carry Rook's names.
func TestExportDetailsNameTheCSISecretsAsOCSOperatorLooksThemUp(t *testing.T) {
	r, _ := DecodeConsumerRequest(managedExportPlan(t, false).Request)
	owner := "apply-test"
	prefix := ConsumerPrincipalPrefix(owner, r.Name)
	renamed := map[string]string{"rook-csi-rbd-node-" + prefix: "rook-" + prefix + "-rbd-node", "rook-csi-rbd-provisioner-" + prefix: "rook-" + prefix + "-rbd-prov",
		"rook-csi-cephfs-node-" + prefix: "rook-" + prefix + "-fs-node", "rook-csi-cephfs-provisioner-" + prefix: "rook-" + prefix + "-fs-prov"}
	good := syntheticDetails(r, owner)
	for name := range renamed {
		if !slices.ContainsFunc(good, func(e exportEntry) bool { return e.Name == name && e.Kind == "Secret" }) {
			t.Fatalf("details lack the CSI Secret %s", name)
		}
	}
	produced := detailsProduced(t, good)
	defer lifecycle.ClearProduced(produced)
	if err := validateExportProduced(produced, r, owner); err != nil {
		t.Fatal(err)
	}
	old := syntheticDetails(r, owner)
	for i := range old {
		if previous, ok := renamed[old[i].Name]; ok {
			old[i].Name = previous
		}
		for key, value := range old[i].Data {
			if previous, ok := renamed[value]; ok && strings.HasSuffix(key, "-secret-name") {
				old[i].Data[key] = previous
			}
		}
	}
	stale := detailsProduced(t, old)
	defer lifecycle.ClearProduced(stale)
	if err := validateExportProduced(stale, r, owner); err == nil {
		t.Fatal("details under the former rook-<prefix>-<suffix> names validated")
	}
}

func TestExportProducedDetailsValidator(t *testing.T) {
	block := managedExportPlan(t, true)
	r, _ := DecodeConsumerRequest(block.Request)
	owner := "apply-test"
	good := detailsProduced(t, syntheticDetails(r, owner))
	defer lifecycle.ClearProduced(good)
	if err := validateExportProduced(good, r, owner); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		edit func([]exportEntry) []exportEntry
	}{
		{"empty key", func(es []exportEntry) []exportEntry {
			for i := range es {
				if es[i].Name == "rook-ceph-operator-creds" {
					es[i].Data["userKey"] = ""
				}
			}
			return es
		}},
		{"null data", func(es []exportEntry) []exportEntry { es[0].Data = nil; return es }},
		{"unexpected entry", func(es []exportEntry) []exportEntry {
			return append(es, exportEntry{Name: "unexpected", Kind: "Secret", Data: map[string]string{"value": "private-sentinel"}})
		}},
		{"missing monitoring", func(es []exportEntry) []exportEntry {
			return slices.DeleteFunc(es, func(e exportEntry) bool { return e.Name == "monitoring-endpoint" })
		}},
		{"wrong endpoint", func(es []exportEntry) []exportEntry {
			for i := range es {
				if es[i].Name == "ceph-rgw" {
					es[i].Data["endpoint"] = "other.example.test:80"
				}
			}
			return es
		}},
		{"foreign leader", func(es []exportEntry) []exportEntry {
			for i := range es {
				if es[i].Name == "rook-ceph-mon-endpoints" {
					es[i].Data["data"] = "foreign=192.0.2.1:3300"
				}
			}
			return es
		}},
		{"wrong leader address", func(es []exportEntry) []exportEntry {
			for i := range es {
				if es[i].Name == "rook-ceph-mon-endpoints" {
					es[i].Data["data"] = "node-1=192.0.2.99:3300"
				}
			}
			return es
		}},
		{"unexpected tls", func(es []exportEntry) []exportEntry {
			return append(es, exportEntry{Name: "ceph-rgw-tls-cert", Kind: "Secret", Data: map[string]string{"cert": "private-sentinel"}})
		}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			produced := detailsProduced(t, tt.edit(syntheticDetails(r, owner)))
			defer lifecycle.ClearProduced(produced)
			err := validateExportProduced(produced, r, owner)
			if err == nil || strings.Contains(err.Error(), "private-sentinel") {
				t.Fatalf("unsafe validator result: %v", err)
			}
		})
	}
}

func TestExportTLSRequestGolden(t *testing.T) {
	block := managedExportPlan(t, true)
	r, err := DecodeConsumerRequest(block.Request)
	if err != nil {
		t.Fatal(err)
	}
	r.Gateway.TLS = true
	r.Gateway.Port = 443
	r.Gateway.CertificateRef = "gateway-tls"
	r.KeyType = "aes256k"
	r.PhysicalHosts = true
	data, err := r.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "request-export-tls", data, false)
	if _, err := DecodeConsumerRequest(data); err != nil {
		t.Fatal(err)
	}
	block.Request = data
	if err := NewConsumer(nil).ProveBinding(context.Background(), "synthetic", reconciliation.Block{BlockDefinition: block}, nil); err == nil {
		t.Fatal("missing gateway certificate accepted")
	}
	e := lifecycle.Execution{Block: reconciliation.Block{BlockDefinition: block}, ApplyOperation: "apply-test", RetiredWithHosts: true}
	if _, err := NewConsumer(nil).Destroy(context.Background(), e); err == nil {
		t.Fatal("physical export retirement accepted")
	}
}
