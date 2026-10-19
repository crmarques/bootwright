package storage

import (
	"fmt"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

const (
	gatewayEndpointRemedy = "set spec.endpoint.fqdn to a name no storage node or other gateway uses, or rename the StorageObjectGateway"
	gatewayIngressRemedy  = "an ingress service in its StorageCluster's spec.ceph.services whose spec.backend_service is the rgw service that spec.serviceRef names, with /spec/ssl_cert and /spec/ssl_key bound in spec.ceph.serviceBindings"
)

func gatewayCatalog(t *testing.T, services ...api.Value) api.Catalog {
	t.Helper()
	_, catalog := withMDSService(t, fixtureCatalog(), rgwService(qualifiedRGWSpec()))
	if len(services) != 0 {
		_, catalog = withGraph(t, catalog, services)
	}
	return catalog
}

func moreRGW() api.Value {
	return rgwService(m("rgw_frontend_port", 8081)).With("service_id", api.StringValue("more"))
}

func secondGateway(pairs ...any) api.Object {
	spec := m("clusterRef", "storage", "serviceRef", "rgw.more", "realm", "realm-2", "zoneGroup", "group-2", "zone", "zone-2")
	for i := 0; i < len(pairs); i += 2 {
		spec = spec.With(pairs[i].(string), v(pairs[i+1]))
	}
	return object(api.StorageObjectGateway, "second", spec)
}

func editGateway(t *testing.T, catalog api.Catalog, pairs ...any) (api.Object, api.Catalog) {
	t.Helper()
	gateway := find(t, catalog, api.StorageObjectGateway, "objects")
	spec := gateway.Spec()
	for i := 0; i < len(pairs); i += 2 {
		spec = spec.With(pairs[i].(string), v(pairs[i+1]))
	}
	changed := gateway.WithSpec(spec)
	return changed, replace(catalog, changed)
}

func withoutObject(catalog api.Catalog, kind api.Kind, name string) api.Catalog {
	kept := []api.Object{}
	for _, object := range catalog.Objects() {
		if object.Kind() != kind || object.Name() != name {
			kept = append(kept, object)
		}
	}
	return api.NewCatalog(kept)
}

func exactlyOne(t *testing.T, issues []api.Issue, field, message, remedy string) {
	t.Helper()
	found := issuesAt(issues, field)
	if len(found) != 1 || found[0].Code != "api.invariant" || found[0].Message != message || (remedy != "" && found[0].Remediation != remedy) {
		t.Fatalf("issues at %s: %#v (all %#v)", field, found, issues)
	}
}

func TestGatewayRequiresBoundRGWService(t *testing.T) {
	catalog := gatewayCatalog(t)
	if issues := Validate(find(t, catalog, api.StorageObjectGateway, "objects"), catalog); len(issues) != 0 {
		t.Fatalf("bound gateway refused: %#v", issues)
	}
	gateway, changed := editGateway(t, catalog, "serviceRef", "rgw.absent")
	exactlyOne(t, Validate(gateway, changed), "$.spec.serviceRef", "spec.serviceRef resolves to no declared native service of StorageCluster storage", "add a spec.ceph.services entry with service_type rgw to StorageCluster storage and set spec.serviceRef to its qualified name rgw.<service_id>")
	gateway, changed = editGateway(t, gatewayCatalog(t, mdsService("files")), "serviceRef", "mds.files")
	exactlyOne(t, Validate(gateway, changed), "$.spec.serviceRef", "spec.serviceRef must name an rgw service; it names a mds service", "set spec.serviceRef to the qualified name rgw.<service_id> of a declared rgw service")
	second := secondGateway("serviceRef", "rgw.objects")
	catalog = replace(catalog, second)
	const remedy = "bind a different rgw service, or remove one of the two gateways"
	exactlyOne(t, Validate(second, catalog), "$.spec.serviceRef", "this rgw service is already bound by StorageObjectGateway objects; one gateway executes each rgw service", remedy)
	exactlyOne(t, Validate(find(t, catalog, api.StorageObjectGateway, "objects"), catalog), "$.spec.serviceRef", "this rgw service is already bound by StorageObjectGateway second; one gateway executes each rgw service", remedy)
}

func TestRGWServiceWithoutGatewayRefuses(t *testing.T) {
	const field = "$.spec.ceph.services[3].service_id"
	cluster, catalog := withMDSService(t, withoutObject(fixtureCatalog(), api.StorageObjectGateway, "objects"), rgwService(qualifiedRGWSpec()))
	exactlyOne(t, Validate(cluster, catalog), field, "RGW service is not bound by a StorageObjectGateway of this cluster, so cephadm would deploy it before its realm, zone group and zone exist", "declare a StorageObjectGateway of this cluster whose spec.serviceRef names this service, or remove the RGW service")
	cluster, catalog = withMDSService(t, fixtureCatalog(), rgwService(qualifiedRGWSpec()))
	if found := issuesAt(Validate(cluster, catalog), field); len(found) != 0 {
		t.Fatalf("bound rgw service refused: %#v", found)
	}
	if found := issuesAt(ValidatePartial(partialMDS(rgwService(qualifiedRGWSpec())), api.Catalog{}), field); len(found) != 0 {
		t.Fatalf("partial rgw service required its gateway: %#v", found)
	}
}

func TestGatewayRealmZoneUniqueness(t *testing.T) {
	catalog := gatewayCatalog(t, moreRGW())
	for _, row := range []struct {
		field, label string
		pairs        []any
	}{
		{"$.spec.realm", "realm", []any{"realm", "realm"}},
		{"$.spec.zoneGroup", "zone group", []any{"zoneGroup", "group"}},
		{"$.spec.zone", "zone", []any{"zone", "zone"}},
	} {
		second := secondGateway(row.pairs...)
		issues := Validate(second, replace(catalog, second))
		exactlyOne(t, issues, row.field, row.label+" is already owned by StorageObjectGateway objects of this cluster", "choose a "+row.label+" no other gateway of StorageCluster storage uses; peer zones of one realm are not realized by Bootwright yet")
		if len(issues) != 1 {
			t.Fatalf("%s collision raised more than its own refusal: %#v", row.label, issues)
		}
	}
	other := gatewayCatalog(t)
	cluster := find(t, other, api.StorageCluster, "storage")
	services := cluster.Spec().Get("ceph", "services").Items()
	services[3] = rgwService(qualifiedRGWSpec().With("rgw_zone", api.StringValue("other")))
	other = replace(other, cluster.WithSpec(cluster.Spec().WithPath(api.ListValue(services...), "ceph", "services")))
	exactlyOne(t, Validate(find(t, other, api.StorageObjectGateway, "objects"), other), "$.spec.zone", "spec.zone disagrees with spec.rgw_zone of the bound rgw service", "make spec.rgw_zone of the rgw service equal spec.zone, or omit spec.rgw_zone there so the gateway supplies it")
}

func TestGatewayPoolRolesAndProtection(t *testing.T) {
	const omap = "RGW keeps bucket index and multipart entries in omap, which erasure-coded pools do not support, so this pool must be replicated"
	catalog := gatewayCatalog(t, moreRGW())
	for _, pool := range []api.Object{
		object(api.StoragePool, "rgw-ec", m("clusterRef", "storage", "type", "erasure", "role", "rgw", "erasure", chunks(2, 2))),
		object(api.StoragePool, "rgw-a", m("clusterRef", "storage", "type", "replicated", "role", "rgw")),
	} {
		catalog = replace(catalog, pool)
	}
	for _, key := range []string{"indexPoolRef", "dataExtraPoolRef"} {
		gateway, changed := editGateway(t, catalog, "pools", m(key, "rgw-ec"))
		exactlyOne(t, Validate(gateway, changed), "$.spec.pools."+key, omap, "reference a replicated StoragePool with role rgw, or omit spec.pools."+key+" to derive the zone's own replicated pool")
	}
	gateway, changed := editGateway(t, catalog, "pools", m("dataPoolRef", "rgw-ec"))
	noIssues(t, Validate(gateway, changed))
	gateway, changed = editGateway(t, catalog, "pools", m("dataPoolRef", "data"))
	exactlyOne(t, Validate(gateway, changed), "$.spec.pools.dataPoolRef", "referenced pool does not have the consumer's required role", "")
	gateway, changed = editGateway(t, catalog, "pools", m("dataPoolRef", "rgw-a"))
	second := secondGateway("pools", m("indexPoolRef", "rgw-a"))
	changed = replace(changed, second)
	exactlyOne(t, Validate(gateway, changed), "$.spec.pools.dataPoolRef", "StoragePool rgw-a is already bound by StorageObjectGateway second", "reference a pool no other gateway binds, or omit spec.pools.dataPoolRef to derive the zone's own pool")
	exactlyOne(t, Validate(second, changed), "$.spec.pools.indexPoolRef", "StoragePool rgw-a is already bound by StorageObjectGateway objects", "reference a pool no other gateway binds, or omit spec.pools.indexPoolRef to derive the zone's own pool")
	gateway, changed = editGateway(t, catalog, "pools", m("indexPoolRef", "rgw-a", "dataPoolRef", "rgw-a"))
	for name, issues := range map[string][]api.Issue{"partial": ValidatePartial(gateway, api.Catalog{}), "complete": Validate(gateway, changed)} {
		exactlyOne(t, issues, "$.spec.pools.dataPoolRef", "one StoragePool cannot hold two zone placement roles", "reference a different rgw StoragePool, or omit spec.pools.dataPoolRef to derive the zone's own pool")
		if len(issues) != 1 {
			t.Fatalf("%s pool roles raised more than one refusal: %#v", name, issues)
		}
	}
}

func TestGatewayDerivedPoolNameCollidesWithDeclaredPool(t *testing.T) {
	for _, derived := range []string{"zone.rgw.meta", "zone.rgw.buckets.index", "zone.rgw.buckets.non-ec", ".rgw.root"} {
		if api.ValidLexical("name", derived) {
			t.Fatalf("a StoragePool name admits the derived zone pool name %s", derived)
		}
	}
}

func TestGatewayDerivedPoolProtectionNeedsHosts(t *testing.T) {
	const pair = "osd_pool_default_size"
	const message = "the zone pools this gateway derives use the cluster's default replica size %s, which its %d declared OSD-bearing hosts cannot hold under the replicated-pool host rule"
	defaultsRemedy := "add OSD-bearing hosts to StorageCluster storage, or set osd_pool_default_size and osd_pool_default_min_size to \"2\" under its spec.ceph.config.global"
	for _, row := range []struct {
		name         string
		hosts        int
		ibm, config  bool
		size, remedy string
	}{
		{name: "ibm defaults", hosts: 3, ibm: true, size: "3", remedy: defaultsRemedy},
		{name: "ibm pair", hosts: 3, ibm: true, config: true},
		{name: "oss three", hosts: 3},
		{name: "oss two", hosts: 2, size: "3", remedy: defaultsRemedy},
		{name: "ibm pair two", hosts: 2, ibm: true, config: true, size: "2", remedy: "add OSD-bearing hosts to StorageCluster storage"},
	} {
		catalog := clusterOf(t, row.hosts, row.ibm)
		_, catalog = withMDSService(t, catalog, rgwService(qualifiedRGWSpec()))
		if row.config {
			cluster := find(t, catalog, api.StorageCluster, "storage")
			catalog = replace(catalog, cluster.WithSpec(cluster.Spec().WithPath(m(pair, "2", "osd_pool_default_min_size", "2"), "ceph", "config", "global")))
		}
		issues := Validate(find(t, catalog, api.StorageObjectGateway, "objects"), catalog)
		if row.size == "" {
			if found := issuesAt(issues, "$.spec.zone"); len(found) != 0 {
				t.Fatalf("%s: protected derived pools refused: %#v", row.name, found)
			}
			continue
		}
		exactlyOne(t, issues, "$.spec.zone", fmt.Sprintf(message, row.size, row.hosts), row.remedy)
	}
}

func TestGatewayRetiredFieldsNameReplacements(t *testing.T) {
	for field, replacement := range map[string]string{
		"$.spec.serviceID":          "spec.serviceRef naming the rgw.<service_id> entry of its StorageCluster's spec.ceph.services",
		"$.spec.placement":          "the placement of the rgw service that spec.serviceRef names",
		"$.spec.frontendPort":       "spec.rgw_frontend_port of the rgw service that spec.serviceRef names",
		"$.spec.endpoint.dnsLabel":  "$.spec.endpoint.fqdn, a full DNS name that defaults to <gateway>.<cluster>.<storage domain>",
		"$.spec.endpoint.scheme":    gatewayIngressRemedy,
		"$.spec.endpoint.port":      gatewayIngressRemedy,
		"$.spec.endpoint.tls":       gatewayIngressRemedy,
		"$.spec.endpoint.ingresses": gatewayIngressRemedy,
	} {
		issue, ok := RetiredField(api.StorageObjectGateway, field)
		if !ok || issue.Code != "api.field" || issue.Field != field || issue.Remediation != "replace "+field+" with "+replacement {
			t.Fatalf("retired %s: %#v %v", field, issue, ok)
		}
	}
	issue, ok := RetiredField(api.StorageObjectGateway, "$.spec.config")
	if !ok || issue.Code != "api.field" || issue.Remediation != "remove $.spec.config: per-service RGW configuration options have no qualified effect contract" {
		t.Fatalf("removed config: %#v %v", issue, ok)
	}
	for _, field := range []string{"$.spec.realm", "$.spec.pools", "$.spec.endpoint.fqdn"} {
		if issue, ok := RetiredField(api.StorageObjectGateway, field); ok {
			t.Fatalf("current field %s reported retired: %#v", field, issue)
		}
	}
}

func gatewayDomainCatalog(domains api.Value) api.Catalog {
	catalog := withoutObject(fixtureCatalog(), api.Environment, "environment")
	if domains.Present() {
		catalog = replace(catalog, object(api.Environment, "environment", m("domains", domains)))
	}
	return catalog
}

func TestGatewayEndpointFQDNDefault(t *testing.T) {
	gateway := object(api.StorageObjectGateway, "objects", m("clusterRef", "storage", "serviceRef", "rgw.objects", "realm", "realm", "zoneGroup", "group", "zone", "zone", "endpoint", m()))
	for want, domains := range map[string]api.Value{
		"objects.storage.storage.example.test":  m("base", "example.test", "clusters", "clusters.example.test", "storageClusters", "storage.example.test"),
		"objects.storage.clusters.example.test": m("base", "example.test", "clusters", "clusters.example.test"),
		"objects.storage.example.test":          m("base", "example.test"),
	} {
		catalog := gatewayDomainCatalog(domains)
		once, _ := Normalize(gateway, catalog)
		twice, _ := Normalize(once, catalog)
		if got := textOf(once.Spec(), "endpoint", "fqdn"); got != want || !twice.Spec().Equal(once.Spec()) {
			t.Fatalf("derived endpoint %q, want %q (idempotent %v)", got, want, twice.Spec().Equal(once.Spec()))
		}
	}
	catalog := gatewayDomainCatalog(m("base", "example.test"))
	authored := gateway.WithSpec(gateway.Spec().With("endpoint", m("fqdn", "s3.example.test")))
	if normalized, _ := Normalize(authored, catalog); !normalized.Spec().Equal(authored.Spec()) {
		t.Fatal("authored endpoint FQDN was replaced")
	}
	bare := gateway.WithSpec(gateway.Spec().Without("endpoint"))
	if normalized, _ := Normalize(bare, catalog); normalized.Spec().Has("endpoint") {
		t.Fatal("omitted endpoint was created")
	}
	_, catalog = withMDSService(t, gatewayDomainCatalog(api.Value{}), rgwService(qualifiedRGWSpec()))
	unnamed, _ := Normalize(gateway, catalog)
	exactlyOne(t, Validate(unnamed, replace(catalog, unnamed)), "$.spec.endpoint.fqdn", "an endpoint needs an FQDN and no Environment domain derives one", "set spec.endpoint.fqdn")
}

func TestGatewayEndpointFQDNUniqueAcrossNodesAndGateways(t *testing.T) {
	catalog := gatewayCatalog(t, moreRGW())
	named, _ := Normalize(object(api.StorageObjectGateway, "node-1", secondGateway("endpoint", m()).Spec()), catalog)
	if textOf(named.Spec(), "endpoint", "fqdn") != "node-1.storage.example.test" {
		t.Fatalf("derived endpoint %q", textOf(named.Spec(), "endpoint", "fqdn"))
	}
	exactlyOne(t, Validate(named, replace(catalog, named)), "$.spec.endpoint.fqdn", "the endpoint FQDN equals the hostname of storage node node-1 of StorageCluster storage", gatewayEndpointRemedy)
	gateway, shared := editGateway(t, catalog, "endpoint", m("fqdn", "s3.example.test"))
	second := secondGateway("endpoint", m("fqdn", "s3.example.test"))
	shared = replace(shared, second)
	exactlyOne(t, Validate(gateway, shared), "$.spec.endpoint.fqdn", "the endpoint FQDN equals the endpoint FQDN of StorageObjectGateway second", gatewayEndpointRemedy)
	exactlyOne(t, Validate(second, shared), "$.spec.endpoint.fqdn", "the endpoint FQDN equals the endpoint FQDN of StorageObjectGateway objects", gatewayEndpointRemedy)
	foreign := object(api.StorageCluster, "other", m("type", "ceph", "ceph", m("topology", m("nodes", api.ListValue(m("name", "far", "fqdn", "s3.example.test", "machineRef", "machine-1"))))))
	gateway, changed := editGateway(t, replace(catalog, foreign), "endpoint", m("fqdn", "s3.example.test"))
	issues := Validate(gateway, changed)
	exactlyOne(t, issues, "$.spec.endpoint.fqdn", "the endpoint FQDN equals the hostname of storage node far of StorageCluster other", gatewayEndpointRemedy)
	if strings.Contains(issues[0].Message, "s3.example.test") {
		t.Fatalf("diagnostic echoed the endpoint FQDN: %#v", issues[0])
	}
}
