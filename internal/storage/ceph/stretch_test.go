package ceph

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/trust"
)

func stretchRequest(t *testing.T) Request {
	t.Helper()
	catalog, cluster := tiebreakerCatalog(nil)
	r, err := baseRequest(catalog, cluster, "synthetic")
	if err != nil {
		t.Fatalf("%+v", diagnostics.Of(err))
	}
	r.Controller, r.Owner = "controller", "apply-original"
	r.FSID = FSIDFor(r.Owner, r.Cluster)
	return r
}

func TestAStretchClusterPlansItsTopologyHostLocationsAndMONCrushLocations(t *testing.T) {
	r := stretchRequest(t)
	want := Stretch{DataSites: []string{"site-a", "site-b"}, FailureDomain: "datacenter", Rule: "stretch-rule", Tiebreaker: "tie", TiebreakerSite: "site-c"}
	if r.Stretch == nil || !slices.Equal(r.Stretch.DataSites, want.DataSites) || r.Stretch.FailureDomain != want.FailureDomain || r.Stretch.Rule != want.Rule || r.Stretch.Tiebreaker != want.Tiebreaker || r.Stretch.TiebreakerSite != want.TiebreakerSite {
		t.Fatalf("stretch = %+v", r.Stretch)
	}
	for _, h := range r.Hosts {
		site := map[string]string{"node-1": "site-a", "node-2": "site-a", "node-3": "site-b", "node-4": "site-b"}[h.Name]
		if h.Name == "tie" && len(h.Location) != 0 || h.Name != "tie" && (len(h.Location) != 2 || h.Location["datacenter"] != site || h.Location["root"] != "default") {
			t.Fatalf("host %s location %v", h.Name, h.Location)
		}
	}
	var mon struct {
		Spec struct {
			CrushLocations map[string][]string `json:"crush_locations"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(r.Services[0], &mon); err != nil || !slices.Equal(mon.Spec.CrushLocations["tie"], []string{"datacenter=site-c"}) || !slices.Equal(mon.Spec.CrushLocations["node-3"], []string{"datacenter=site-b"}) || len(mon.Spec.CrushLocations) != 5 {
		t.Fatalf("mon service %s: %v", r.Services[0], err)
	}
	data, err := r.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "request-cluster-stretch", data, false)
	if decoded, err := DecodeRequest(data); err != nil || decoded.Stretch == nil {
		t.Fatalf("%+v %v", decoded.Stretch, err)
	}
}

func TestAStretchMONServiceTakesOnlyTheDerivedCrushLocations(t *testing.T) {
	for name, test := range map[string]struct {
		locations api.Value
		admitted  bool
	}{
		"derived":   {requestMap("node-1", api.StringList("datacenter=site-a"), "node-2", api.StringList("datacenter=site-a"), "node-3", api.StringList("datacenter=site-b"), "node-4", api.StringList("datacenter=site-b"), "tie", api.StringList("datacenter=site-c")), true},
		"other":     {requestMap("node-1", api.StringList("datacenter=site-b")), false},
		"undefined": {api.Value{}, true},
	} {
		t.Run(name, func(t *testing.T) {
			catalog, cluster := tiebreakerCatalog(func(m, c api.Value) (api.Value, api.Value) {
				if !test.locations.Present() {
					return m, c
				}
				services := c.Get("services").Items()
				services[0] = services[0].WithPath(test.locations, "spec", "crush_locations")
				return m, c.WithPath(api.ListValue(services...), "services")
			})
			_, err := baseRequest(catalog, cluster, "synthetic")
			if (err == nil) != test.admitted {
				t.Fatalf("admitted %v: %+v", err == nil, diagnostics.Of(err))
			}
		})
	}
}

func TestAStretchPoolAndZonePoolTakeTheStretchRuleAtFourAndTwo(t *testing.T) {
	r := stretchRequest(t)
	catalog, _ := tiebreakerCatalog(nil)
	pool := childOf(api.CephPool, "volumes", "role", "rbd", "replicated", requestMap("size", 4, "minSize", 2))
	p, err := poolOf(api.NewCatalog(append(catalog.Objects(), pool)), pool, r)
	if err != nil || p.CrushRule != "stretch-rule" || p.Size != 4 || p.MinSize != 2 {
		t.Fatalf("pool %+v %+v", p, diagnostics.Of(err))
	}
	if size, minimum := zonePoolReplicas(r); size != 4 || minimum != 2 {
		t.Fatalf("zone pools %d/%d", size, minimum)
	}
	rule, _, width, _, ok := placementOf(api.NewCatalog(append(catalog.Objects(), pool)), pool)
	if !ok || rule != "stretch-rule" || width != 4 {
		t.Fatalf("PG budget placement %s %d", rule, width)
	}
}

func TestAStretchClusterDeadlineAddsItsTwoActivationCallsAndOneReadinessWait(t *testing.T) {
	if applyDeadline(7, true) != 21120*time.Second || applyDeadline(8, true) != 21780*time.Second || maxStretchHosts != 7 || hostBound(false) != maxClusterHosts {
		t.Fatalf("7 hosts take %s, 8 take %s, bound %d", applyDeadline(7, true), applyDeadline(8, true), maxStretchHosts)
	}
	r := stretchRequest(t)
	if deadline, err := cephDeadline(r, "apply"); err != nil || deadline != applyDeadline(len(r.Hosts), true) {
		t.Fatalf("deadline %s %v", deadline, err)
	}
	r.Hosts = slices.Repeat(r.Hosts[:1], 8)
	_, err := cephDeadline(r, "apply")
	refusedWith(t, err, "lifecycle.unsupported", "CephCluster example declares 8 Ceph hosts, whose cluster run deadline of 6h3m0s is past the 6h0m0s every adapter run is held to",
		"declare at most 7 entries in spec.topology.nodes of CephCluster example; B79 tracks larger clusters")
}

func stretchEvidence(r Request, stage string, enabled bool) Evidence {
	e := readyEvidence(r)
	mons, count := servicePlacement(r, "mon")
	e.Monitors, e.OSDs = mons[:count], []OSDEvidence{}
	for i, h := range r.Hosts {
		for _, d := range h.Devices {
			e.OSDs = append(e.OSDs, OSDEvidence{Host: h.Name, Path: d.Path, ID: i, UUID: "osd-" + h.Name})
		}
	}
	e.MonAddrs = e.MonAddrs[:0]
	e.Stretch = &StretchEvidence{Stage: stage, Enabled: enabled}
	if stage != "verified" || !enabled {
		e.Ready, e.Partial = false, true
	}
	return e
}

func TestStretchReadinessNeedsTheVerifiedStageAndObservedStretchMode(t *testing.T) {
	r := stretchRequest(t)
	data := evidenceBytes(t, stretchEvidence(r, "verified", true))
	matchesGolden(t, "evidence-stretch-ready", data, false)
	if effect, err := EvaluateEvidence(data, r, "digest", false); err != nil || effect != reconciliation.EffectCompleted {
		t.Fatalf("ready stretch evidence: %s %v", effect, err)
	}
	for _, test := range []struct {
		stage   string
		enabled bool
	}{{"", false}, {"located", false}, {"rule-present", false}, {"election-connectivity", false}, {"stretch-enabled", true}, {"verified", false}} {
		if effect, err := EvaluateEvidence(evidenceBytes(t, stretchEvidence(r, test.stage, test.enabled)), r, "digest", false); err != nil || effect != reconciliation.EffectPartial {
			t.Fatalf("stage %q enabled %v: %s %v, want partial realization", test.stage, test.enabled, effect, err)
		}
	}
	claimed := stretchEvidence(r, "rule-present", false)
	claimed.Ready, claimed.Partial = true, false
	missing := stretchEvidence(r, "verified", true)
	missing.Stretch = nil
	unknown := stretchEvidence(r, "tiebreaker-located", true)
	plain := readyEvidence(runtimeRequest())
	plain.Stretch = &StretchEvidence{Stage: "verified", Enabled: true}
	for name, test := range map[string]struct {
		e Evidence
		r Request
	}{"ready before verified": {claimed, r}, "no stretch member": {missing, r}, "unknown stage": {unknown, r}, "a member outside stretch": {plain, runtimeRequest()}} {
		if effect, err := EvaluateEvidence(evidenceBytes(t, test.e), test.r, "digest", false); err == nil || effect != reconciliation.EffectUnknown {
			t.Fatalf("%s: %s %v", name, effect, err)
		}
	}
}

func TestEveryStretchRefusalNamesItsRemedyAndTheClusterBlockMapsIt(t *testing.T) {
	refusals := adapterRefusals()
	for token, want := range stretchRefusals() {
		got, ok := refusals[token]
		if !ok || diagnostics.Of(got)[0].Message != diagnostics.Of(want)[0].Message || diagnostics.Of(want)[0].Remediation == "" {
			t.Fatalf("%s: %v", token, got)
		}
	}
	for _, token := range []string{"stretch-intent", "stretch-interrupted", "stretch-foreign", "stretch-drift"} {
		if d := diagnostics.Of(stretchRefusals()[token])[0]; !strings.Contains(d.Message+d.Remediation, "never") {
			t.Fatalf("%s does not say what Bootwright never does: %+v", token, d)
		}
	}
	if !strings.Contains(diagnostics.Of(stretchRefusals()["stretch-intent"])[0].Remediation, "never runs mon disable_stretch_mode") {
		t.Fatal("the frozen-intent remedy offers disable_stretch_mode as an inverse")
	}
	if _, ok := hostRefusals()["stretch-intent"]; !ok {
		t.Fatal("a host block cannot name a changed stretch intent")
	}
}

// withStretchTiebreaker turns cluster example of any test catalog into a
// stretch cluster with a provided tiebreaker whose host key Secret is pinned.
func withStretchTiebreaker(catalog api.Catalog, fips bool) api.Catalog {
	objects := catalog.Objects()
	for i, object := range objects {
		if object.Kind() != api.CephCluster || object.Name() != "example" {
			continue
		}
		spec := object.Spec()
		nodes := spec.Get("topology", "nodes").Items()
		for j := range nodes {
			nodes[j] = nodes[j].With("site", api.StringValue([]string{"site-a", "site-b"}[j%2]))
		}
		nodes = append(nodes, requestMap("name", "tie", "machineRef", "tie", "site", "site-c", "labels", api.StringList("mon")))
		spec = spec.WithPath(api.ListValue(nodes...), "topology", "nodes").
			WithPath(requestMap("failureDomain", "datacenter", "ruleName", "stretch-rule", "dataSites", api.StringList("site-a", "site-b"), "tiebreaker", requestMap("node", "tie", "site", "site-c")), "topology", "stretch").
			WithPath(api.StringList(append(spec.Get("networks", "publicCIDRs").Strings(), "203.0.113.0/24")...), "networks", "publicCIDRs")
		if fips {
			spec = spec.WithPath(api.BoolValue(true), "security", "fips", "enabled")
		}
		objects[i] = object.WithSpec(spec)
	}
	tie := requestMap("os", requestMap("provided", true), "network", requestMap("addresses", api.ListValue(requestMap("name", "ip", "address", "203.0.113.10"))),
		"access", requestMap("ssh", requestMap("addressRef", "ip", "user", "operator", "knownHostsRef", "tie-known-hosts", "auth", requestMap("privateKeyRef", "fleet-key"))))
	return api.NewCatalog(append(objects, api.NewObject(api.Machine, "tie", api.Value{}, tie)))
}

func TestTheSMBConsumerAndReplicationBlocksBindAProvidedTiebreakersPinnedHostKey(t *testing.T) {
	input := func(catalog api.Catalog) lifecycle.PlanInput {
		return lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "synthetic"}, Controller: "controller", State: compilation.NewState(catalog, catalog, nil)}
	}
	for name, plan := range map[string]func() (lifecycle.CapabilityPlan, error){
		"smb": func() (lifecycle.CapabilityPlan, error) {
			return NewSMB(nil).Plan(context.Background(), input(withStretchTiebreaker(smbGraph(t), false)))
		},
		"consumer": func() (lifecycle.CapabilityPlan, error) {
			children := []api.Object{cephfsPool("block", "rbd"), filesystemOf("subvolumeGroups", api.ListValue(requestMap("name", "csi"))),
				childOf(api.CephConsumer, "consumer", "dataFoundation", requestMap("rbdPoolRef", "block", "filesystemRef", "files", "cephxKeyType", "aes"))}
			return NewConsumer(nil).Plan(context.Background(), input(withStretchTiebreaker(filesystemCatalog(withMDS, children...), false)))
		},
		"replication": func() (lifecycle.CapabilityPlan, error) {
			return NewReplication(nil).Plan(context.Background(), input(withStretchTiebreaker(replicationCatalog("image"), false)))
		},
	} {
		t.Run(name, func(t *testing.T) {
			p, err := plan()
			if err != nil || len(p.Definitions) == 0 {
				t.Fatalf("%+v", diagnostics.Of(err))
			}
			if !slices.Contains(p.Secrets, "tie-known-hosts") {
				t.Fatalf("the %s plan binds no tiebreaker known-hosts Secret: %v", name, p.Secrets)
			}
		})
	}
}

func TestEveryBlockThatDialsAFIPSTiebreakerRefusesItsWeakPinnedHostKey(t *testing.T) {
	tie := Host{Name: "tie", Machine: "tie", Address: tieAddress, Port: 22, KnownHostsRef: "tie-known-hosts", Provided: true, SSHKeyRef: "fleet-key"}
	weak := trust.HostKey{Type: "ssh-ed25519", PublicKey: wire([]byte("ssh-ed25519"), make([]byte, 32))}
	for _, key := range []trust.HostKey{weak, rsaKey(2048)} {
		e := lifecycle.Execution{Material: map[string]secrets.Material{"tie-known-hosts": secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(key.Line(tieAddress, 22))})}}
		scope := ClusterScope{Bootstrap: "tie", Security: &Security{FIPS: true, CephXKeyType: "aes"}, Hosts: []Host{tie}}
		admitted := key.Type == "ssh-rsa"
		for name, bind := range map[string]func() error{
			"smb and consumer": func() error {
				_, _, cleanup, err := smbMaterial(e, SMBRequest{}, scope, "observe", nil)
				cleanup()
				return err
			},
			"replication": func() error {
				_, _, cleanup, err := replicationMaterial(e, []ClusterScope{scope}, nil)
				cleanup()
				return err
			},
			"cluster": func() error {
				_, _, cleanup, err := boundMaterial(e, Request{Security: scope.Security, Hosts: scope.Hosts, ClusterKeyRef: "cluster-key"}, "observe", nil)
				cleanup()
				return err
			},
		} {
			err := bind()
			if admitted && err != nil && strings.Contains(fmt.Sprint(diagnostics.Of(err)), "FIPS Ceph refuses") || !admitted && (err == nil || !strings.Contains(diagnostics.Of(err)[0].Message, "FIPS Ceph refuses the ssh-ed25519 host key")) {
				t.Fatalf("%s with a %s key: %+v", name, key.Type, diagnostics.Of(err))
			}
		}
	}
}

func TestAStretchHostRequestCarriesTheStretchScopeGolden(t *testing.T) {
	r := stretchRequest(t)
	catalog, _ := tiebreakerCatalog(nil)
	packages, err := packageSources(catalog, r, "synthetic")
	if err != nil {
		t.Fatal(err)
	}
	r.Packages = packages
	data, err := HostRequest{Version: HostVersion, Host: "tie", Cluster: scopeOf(r)}.canonical()
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "request-host-stretch", data, false)
	if decoded, err := DecodeHostRequest(data); err != nil || decoded.Cluster.Stretch == nil || decoded.Cluster.Stretch.Tiebreaker != "tie" {
		t.Fatalf("%+v %v", decoded.Cluster.Stretch, err)
	}
}

func stretchGatewayCatalog(t *testing.T, ingresses bool, extra ...api.Value) api.Catalog {
	t.Helper()
	catalog, _ := tiebreakerCatalog(func(m, c api.Value) (api.Value, api.Value) {
		services := append(append(c.Get("services").Items(), extra...), requestMap("service_type", "rgw", "service_id", "objects", "placement", requestMap("hosts", api.StringList("node-1", "node-2", "node-3", "node-4"))))
		if ingresses {
			for i, site := range [][]string{{"node-1", "node-2"}, {"node-3", "node-4"}} {
				services = append(services, requestMap("service_type", "ingress", "service_id", fmt.Sprintf("rgw.objects-%d", i), "placement", requestMap("hosts", api.StringList(site...)),
					"spec", requestMap("backend_service", "rgw.objects", "virtual_ip", fmt.Sprintf("192.0.2.%d/24", 50+i), "frontend_port", 443, "monitor_port", 1967, "ssl", true, "first_virtual_router_id", 50+i)))
			}
			c = c.WithPath(api.ListValue(tlsBinding("ingress.rgw.objects-0", "gateway-tls"), tlsBinding("ingress.rgw.objects-1", "gateway-tls")), "serviceBindings")
		}
		services[1] = requestMap("service_type", "mgr", "placement", requestMap("hosts", api.StringList("node-1", "node-2", "node-3", "node-4")))
		services[2] = services[2].WithPath(api.StringList("/dev/vdb", "/dev/vdc"), "spec", "data_devices", "paths")
		return m, c.WithPath(api.ListValue(services...), "services")
	})
	objects := slices.DeleteFunc(catalog.Objects(), func(o api.Object) bool { return o.Kind() == api.InfraProvider })
	disks := api.ListValue(requestMap("name", "data", "sizeGiB", 1), requestMap("name", "more", "sizeGiB", 1))
	objects = append(objects, api.NewObject(api.InfraProvider, "hypervisor", api.Value{}, requestMap("libvirt", requestMap("machineProfiles", api.ListValue(requestMap("name", "guest", "dataDisks", disks))))))
	return api.NewCatalog(append(objects, objectGateway("objects", "endpoint", requestMap("fqdn", "s3.example.test"))))
}

func TestAStretchGatewayFreezesOneIngressPerDataSiteAndResolvesToEveryFrontDoor(t *testing.T) {
	catalog := stretchGatewayCatalog(t, true)
	r := gatewayOf(t, gatewayBlock(t, catalog))
	if r.Stretch == nil || len(r.Gateway.Documents) != 3 || gatewayTLSSecret(r.Gateway) != "gateway-tls" || len(r.Gateway.Bindings) != 4 {
		t.Fatalf("the stretch gateway froze %d documents and bindings %+v", len(r.Gateway.Documents), r.Gateway.Bindings)
	}
	for i, want := range []string{"ingress.rgw.objects-0", "ingress.rgw.objects-1"} {
		var document struct {
			ServiceID string `json:"service_id"`
		}
		if json.Unmarshal(r.Gateway.Documents[i+1], &document) != nil || "ingress."+document.ServiceID != want {
			t.Fatalf("ingress document %d is %s", i+1, r.Gateway.Documents[i+1])
		}
	}
	gateway, _ := catalog.Find(api.CephObjectGateway, "objects")
	cluster, _ := catalog.Find(api.CephCluster, "example")
	exported, err := exportGateway(catalog, cluster, gateway, r)
	if err != nil || !slices.Equal(exported.Addresses, []string{"192.0.2.50", "192.0.2.51"}) || exported.Port != 443 || !exported.TLS || exported.CertificateRef != "gateway-tls" {
		t.Fatalf("the export reaches %+v (%v)", exported, diagnostics.Of(err))
	}
	direct := stretchGatewayCatalog(t, false)
	gateway, _ = direct.Find(api.CephObjectGateway, "objects")
	cluster, _ = direct.Find(api.CephCluster, "example")
	exported, err = exportGateway(direct, cluster, gateway, gatewayOf(t, gatewayBlock(t, direct)))
	if err != nil || len(exported.Addresses) != 4 || exported.TLS {
		t.Fatalf("a direct stretch gateway exports %+v (%v)", exported, diagnostics.Of(err))
	}
}

func TestAStretchConsumerExportCarriesTheTiebreakerMONAndEveryFrontDoor(t *testing.T) {
	catalog := stretchGatewayCatalog(t, true, mdsRequestService("files"))
	pool := func(name, role string) api.Object {
		return childOf(api.CephPool, name, "role", role, "replicated", requestMap("size", 4, "minSize", 2))
	}
	objects := append(catalog.Objects(), pool("fs-meta", "cephfsMetadata"), pool("fs-data", "cephfsData"), pool("block", "rbd"), filesystemOf(),
		childOf(api.CephConsumer, "consumer", "dataFoundation", requestMap("rbdPoolRef", "block", "filesystemRef", "files", "cephxKeyType", "aes", "objectGatewayRef", "objects")))
	state := api.NewCatalog(objects)
	plan, err := NewConsumer(nil).Plan(context.Background(), lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "synthetic"}, Controller: "controller",
		State: compilation.NewState(state, state, nil)})
	if err != nil || len(plan.Definitions) != 1 {
		t.Fatalf("the stretch consumer did not plan: %+v", diagnostics.Of(err))
	}
	r, err := DecodeConsumerRequest(plan.Definitions[0].Request)
	if err != nil || r.Monitors["tie"] != tieAddress || len(r.Monitors) != 5 || len(r.MonitoringHosts) != 4 || slices.Contains(r.MonitoringHosts, tieAddress) || r.Gateway == nil || !slices.Equal(r.Gateway.Addresses, []string{"192.0.2.50", "192.0.2.51"}) {
		t.Fatalf("the stretch export %+v (%v)", r, err)
	}
}
