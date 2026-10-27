package cephcluster

import (
	"math/big"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

var gatewayPoolKeys = []string{"indexPoolRef", "dataPoolRef", "dataExtraPoolRef"}

var gatewayOwnedNames = []struct{ field, member, label string }{
	{field: "realm", member: "rgw_realm", label: "realm"},
	{field: "zoneGroup", member: "rgw_zonegroup", label: "zone group"},
	{field: "zone", member: "rgw_zone", label: "zone"},
}

func (a *admission) gateway(cluster api.Object) {
	spec := a.object.Spec()
	for _, key := range []string{"serviceRef", "realm", "zoneGroup", "zone"} {
		a.required(spec, key, "$.spec")
	}
	a.gatewayService(cluster, spec)
	a.gatewayOwnership(cluster, spec)
	a.gatewayPools(cluster, spec.Get("pools"))
	a.gatewayDerivedPools(cluster)
	if spec.Get("endpoint").Present() {
		a.gatewayEndpoint(spec.Get("endpoint"))
	}
}

func (a *admission) peerGateways(cluster api.Object) []api.Object {
	out := []api.Object{}
	for _, other := range a.catalog.OfKind(api.CephObjectGateway) {
		if other.Name() != a.object.Name() && textOf(other.Spec(), "clusterRef") == cluster.Name() {
			out = append(out, other)
		}
	}
	return out
}

func (a *admission) gatewayService(cluster api.Object, spec api.Value) {
	ref := textOf(spec, "serviceRef")
	if !spec.Has("serviceRef") {
		return
	}
	service, ok := NativeService(cluster, ref)
	switch {
	case !ok:
		a.issue("api.invariant", "$.spec.serviceRef", "spec.serviceRef resolves to no declared native service of CephCluster "+cluster.Name(), "add a spec.services entry with service_type rgw to CephCluster "+cluster.Name()+" and set spec.serviceRef to its qualified name rgw.<service_id>")
	case textOf(service, "service_type") != "rgw":
		a.issue("api.invariant", "$.spec.serviceRef", "spec.serviceRef must name an rgw service; it names a "+textOf(service, "service_type")+" service", "set spec.serviceRef to the qualified name rgw.<service_id> of a declared rgw service")
	default:
		for _, owned := range gatewayOwnedNames {
			member := service.Get("spec", owned.member)
			if member.Present() && spec.Has(owned.field) && member.Text() != textOf(spec, owned.field) {
				a.issue("api.invariant", "$.spec."+owned.field, "spec."+owned.field+" disagrees with spec."+owned.member+" of the bound rgw service", "make spec."+owned.member+" of the rgw service equal spec."+owned.field+", or omit spec."+owned.member+" there so the gateway supplies it")
			}
		}
	}
	for _, other := range a.peerGateways(cluster) {
		if textOf(other.Spec(), "serviceRef") == ref {
			a.issue("api.invariant", "$.spec.serviceRef", "this rgw service is already bound by CephObjectGateway "+other.Name()+"; one gateway executes each rgw service", "bind a different rgw service, or remove one of the two gateways")
			return
		}
	}
}

func (a *admission) gatewayOwnership(cluster api.Object, spec api.Value) {
	for _, owned := range gatewayOwnedNames {
		value := textOf(spec, owned.field)
		if value == "" {
			continue
		}
		for _, other := range a.peerGateways(cluster) {
			if textOf(other.Spec(), owned.field) == value {
				a.issue("api.invariant", "$.spec."+owned.field, owned.label+" is already owned by CephObjectGateway "+other.Name()+" of this cluster", "choose a "+owned.label+" no other gateway of CephCluster "+cluster.Name()+" uses; peer zones of one realm are not realized by Bootwright yet")
				break
			}
		}
	}
}

func (a *admission) gatewayPools(cluster api.Object, pools api.Value) {
	for _, key := range gatewayPoolKeys {
		name := textOf(pools, key)
		if name == "" {
			continue
		}
		field := "$.spec.pools." + key
		a.poolRole(name, field, "rgw", cluster)
		if pool, ok := a.catalog.Find(api.CephPool, name); ok && key != "dataPoolRef" && textOf(pool.Spec(), "clusterRef") == cluster.Name() && PoolProtection(pool.Spec()) == "erasure" {
			a.issue("api.invariant", field, "RGW keeps bucket index and multipart entries in omap, which erasure-coded pools do not support, so this pool must be replicated", "reference a replicated CephPool with role rgw, or omit spec.pools."+key+" to derive the zone's own replicated pool")
		}
		for _, other := range a.peerGateways(cluster) {
			if gatewayBindsPool(other, name) {
				a.issue("api.invariant", field, "CephPool "+name+" is already bound by CephObjectGateway "+other.Name(), "reference a pool no other gateway binds, or omit spec.pools."+key+" to derive the zone's own pool")
				break
			}
		}
	}
}

func gatewayBindsPool(gateway api.Object, pool string) bool {
	for _, key := range gatewayPoolKeys {
		if textOf(gateway.Spec(), "pools", key) == pool {
			return true
		}
	}
	return false
}

func (a *admission) gatewayPoolRoles(pools api.Value) {
	for j, later := range gatewayPoolKeys {
		for _, earlier := range gatewayPoolKeys[:j] {
			if name := textOf(pools, later); name != "" && name == textOf(pools, earlier) {
				a.issue("api.invariant", "$.spec.pools."+later, "one CephPool cannot hold two zone placement roles", "reference a different rgw CephPool, or omit spec.pools."+later+" to derive the zone's own pool")
				break
			}
		}
	}
}

func (a *admission) gatewayDerivedPools(cluster api.Object) {
	if cluster.Spec().Get("topology", "nodes").Len() == 0 {
		return
	}
	size, _ := replicatedDefaults(cluster)
	remedy := "add OSD-bearing hosts to CephCluster " + cluster.Name() + ", or set osd_pool_default_size and osd_pool_default_min_size to \"2\" under its spec.config.global"
	global := cluster.Spec().Get("config", "global")
	if global.Has("osd_pool_default_size") && global.Has("osd_pool_default_min_size") {
		if configured, ok := new(big.Int).SetString(textOf(global, "osd_pool_default_size"), 10); ok {
			size, remedy = configured, "add OSD-bearing hosts to CephCluster "+cluster.Name()
		}
	}
	hosts := big.NewInt(osdHostCount(cluster))
	ibm := api.CephDistribution(cluster.Spec()) == "ibm" && !cluster.Spec().Has("topology", "stretch")
	if size.Cmp(hosts) > 0 || ibm && size.Cmp(hosts) == 0 {
		a.issue("api.invariant", "$.spec.zone", "the zone pools this gateway derives use the cluster's default replica size "+size.String()+", which its "+hosts.String()+" declared OSD-bearing hosts cannot hold under the replicated-pool host rule", remedy)
	}
}

func (a *admission) gatewayEndpoint(endpoint api.Value) {
	const field, remedy = "$.spec.endpoint.fqdn", "set spec.endpoint.fqdn to a name no storage node or other gateway uses, or rename the CephObjectGateway"
	fqdn := textOf(endpoint, "fqdn")
	if fqdn == "" {
		a.issue("api.invariant", field, "an endpoint needs an FQDN and no Environment domain derives one", "set spec.endpoint.fqdn")
		return
	}
	if !api.ValidLexical("dns", fqdn) {
		a.issue("api.invariant", field, "the derived endpoint FQDN is not a valid DNS name", "set spec.endpoint.fqdn to a valid DNS name or shorten the gateway, cluster or domain name")
		return
	}
	for _, cluster := range a.catalog.OfKind(api.CephCluster) {
		for _, node := range cluster.Spec().Get("topology", "nodes").Items() {
			if strings.EqualFold(NativeHostname(node), fqdn) {
				a.issue("api.invariant", field, "the endpoint FQDN equals the hostname of storage node "+textOf(node, "name")+" of CephCluster "+cluster.Name(), remedy)
			}
		}
	}
	for _, other := range a.catalog.OfKind(api.CephObjectGateway) {
		if other.Name() != a.object.Name() && strings.EqualFold(textOf(other.Spec(), "endpoint", "fqdn"), fqdn) {
			a.issue("api.invariant", field, "the endpoint FQDN equals the endpoint FQDN of CephObjectGateway "+other.Name(), remedy)
		}
	}
}

func (a *admission) rgwBinding(field string, service api.Value) {
	name := NativeServiceName(service)
	for _, gateway := range a.catalog.OfKind(api.CephObjectGateway) {
		if textOf(gateway.Spec(), "clusterRef") == a.object.Name() && textOf(gateway.Spec(), "serviceRef") == name {
			return
		}
	}
	a.issue("api.invariant", field+".service_id", "RGW service is not bound by a CephObjectGateway of this cluster, so cephadm would deploy it before its realm, zone group and zone exist", "declare a CephObjectGateway of this cluster whose spec.serviceRef names this service, or remove the RGW service")
}

func (a *admission) objectUser(cluster api.Object) {
	spec, name := a.object.Spec(), a.object.Name()
	if name == "dashboard" {
		a.issue("api.invariant", "$.metadata.name", "the RGW user ID dashboard is reserved: the mgr dashboard module creates a user of that ID in every realm", "rename the CephObjectUser; its metadata.name is its native RGW user ID")
	}
	if spec.Has("objectGatewayRef") {
		a.sameOwner(api.CephObjectGateway, textOf(spec, "objectGatewayRef"), "$.spec.objectGatewayRef", cluster)
	}
	if display := spec.Get("displayName"); display.Present() && (len(display.Text()) > 256 || !NativeVersion(display.Text())) {
		a.issue("api.invariant", "$.spec.displayName", "an RGW display name is at most 256 bytes of text without control characters", "shorten spec.displayName and remove its control characters, or omit it to use metadata.name")
	} else if display.Present() && strings.HasPrefix(display.Text(), "-") {
		a.issue("api.invariant", "$.spec.displayName", "CephObjectUser "+name+" has a display name that begins with -, which radosgw-admin reads as an option instead of the display name", "start spec.displayName with another character, or omit it to use metadata.name")
	}
}

func (a *admission) objectBucket(cluster api.Object) {
	spec, name := a.object.Spec(), a.object.Name()
	if len(name) < 3 {
		a.issue("api.invariant", "$.metadata.name", "an S3 bucket name has at least 3 characters", "lengthen metadata.name to at least 3 characters; it is the native bucket name")
	}
	gateway := textOf(spec, "objectGatewayRef")
	if spec.Has("objectGatewayRef") {
		a.sameOwner(api.CephObjectGateway, gateway, "$.spec.objectGatewayRef", cluster)
	}
	a.bucketPolicyShape(name)
	if !spec.Has("ownerRef") {
		return
	}
	if owner, ok := a.sameOwner(api.CephObjectUser, textOf(spec, "ownerRef"), "$.spec.ownerRef", cluster); ok && textOf(owner.Spec(), "objectGatewayRef") != gateway {
		a.issue("api.invariant", "$.spec.ownerRef", "CephObjectBucket "+name+" is owned by CephObjectUser "+owner.Name()+" of another CephObjectGateway, whose zone cannot hold this bucket", "set spec.ownerRef to a CephObjectUser whose spec.objectGatewayRef is "+gateway+", or set spec.objectGatewayRef to that user's gateway")
	}
}

func (a *admission) bucketPolicyShape(name string) {
	policy := a.object.Spec().Get("policy")
	if policy.Present() && !policyDocument(policy) {
		a.issue("api.invariant", "$.spec.policy", "CephObjectBucket "+name+" declares a policy without a Version string or without a Statement holding at least one statement, which the native IAM policy grammar requires and which RGW would store as a policy that grants nothing", "write spec.policy as a mapping with a Version string, such as 2012-10-17, and a Statement holding one mapping or a list of at least one")
	}
}

func policyDocument(policy api.Value) bool {
	statement := policy.Get("Statement")
	return policy.Get("Version").Type() == api.String && policy.Get("Version").Text() != "" && statement.Len() > 0
}
