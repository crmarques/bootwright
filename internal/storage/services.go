package storage

import (
	"net/netip"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/managedos"
)

func (a *admission) clusterServices(ceph api.Value) {
	public := a.prefixes(ceph.Get("networks", "publicCIDRs"), "$.spec.ceph.networks.publicCIDRs")
	private := a.prefixes(ceph.Get("networks", "clusterCIDRs"), "$.spec.ceph.networks.clusterCIDRs")
	for _, left := range public {
		for _, right := range private {
			if left.Overlaps(right) {
				a.invalid("$.spec.ceph.networks", "public and replication networks must be disjoint")
			}
		}
	}
	for _, section := range ceph.Get("config").Fields() {
		for _, key := range []string{"public_network", "cluster_network", "container_image"} {
			if section.Value.Has(key) {
				a.invalid("$.spec.ceph.config", "cluster config cannot repeat a typed field")
			}
		}
	}
	services := ceph.Get("services").Items()
	keys := []string{}
	firstClass := []string{"mon", "mgr", "osd", "mds", "rgw", "nfs", "ingress", "prometheus", "grafana", "alertmanager", "node-exporter", "node_exporter", "loki", "promtail", "mgmt-gateway", "mgmt_gateway"}
	for i, service := range services {
		field := indexed("$.spec.ceph.services", i)
		keys = append(keys, textOf(service, "serviceType")+"/"+textOf(service, "serviceID"))
		if slices.Contains(firstClass, textOf(service, "serviceType")) {
			a.invalid(field+".serviceType", "generic services cannot duplicate a first-class storage service")
		}
		a.placement(a.object, service.Get("placement"), field+".placement", "", true)
	}
	a.uniqueValues(keys, "$.spec.ceph.services", "generic service type and ID pairs must be unique")
	monitoring := ceph.Get("monitoring")
	for _, service := range []string{"prometheus", "grafana", "alertmanager", "nodeExporter", "loki", "promtail"} {
		if !monitoring.Has(service) {
			continue
		}
		field := "$.spec.ceph.monitoring." + service
		if monitoring.Has("enabled") && !monitoring.Get("enabled").Bool() {
			a.invalid(field, "disabled monitoring forbids service blocks")
			continue
		}
		role := service
		if slices.Contains([]string{"nodeExporter", "loki", "promtail"}, service) {
			role = ""
		}
		a.placement(a.object, monitoring.Get(service, "placement"), field+".placement", role, role == "")
		a.serviceNetworks(a.object, monitoring.Get(service, "networks"), field+".networks")
	}
	gateway := ceph.Get("mgmtGateway")
	if gateway.Present() {
		http := fallback(gateway.Get("exposure"), "https") == "http"
		if http {
			a.forbid(gateway, "$.spec.ceph.mgmtGateway", "tls", "oauth2Proxy")
			if gateway.Get("enableAuth").Bool() {
				a.invalid("$.spec.ceph.mgmtGateway.enableAuth", "HTTP management gateway forbids authentication")
			}
		}
		if gateway.Get("enableAuth").Bool() {
			a.required(gateway, "oauth2Proxy", "$.spec.ceph.mgmtGateway")
		} else {
			a.forbid(gateway, "$.spec.ceph.mgmtGateway", "oauth2Proxy")
		}
		if gateway.Has("ingress") {
			a.ingress(a.object, gateway.Get("ingress"), "$.spec.ceph.mgmtGateway.ingress")
		}
	}
	if contains(ceph.Get("cephadm", "workarounds"), "mgmt-gateway-spec-dependency-recording") && (!gateway.Present() || fallback(gateway.Get("exposure"), "https") != "http" || gateway.Has("tls") || gateway.Has("oauth2Proxy") || gateway.Get("enableAuth").Bool()) {
		a.invalid("$.spec.ceph.cephadm.workarounds", "management-gateway workaround requires HTTP without TLS or OAuth")
	}
}

func (a *admission) prefixes(values api.Value, field string) []netip.Prefix {
	prefixes := []netip.Prefix{}
	for i, value := range values.Items() {
		prefix, err := netip.ParsePrefix(value.Text())
		if err != nil {
			continue
		}
		prefix = prefix.Masked()
		for _, previous := range prefixes {
			if previous.Overlaps(prefix) {
				a.invalid(indexed(field, i), "declared network ranges must not overlap")
			}
		}
		prefixes = append(prefixes, prefix)
	}
	return prefixes
}
func declaredPublicNetworks(cluster api.Object, catalog api.Catalog) []netip.Prefix {
	out := []netip.Prefix{}
	for _, value := range cluster.Spec().Get("ceph", "networks", "publicCIDRs").Items() {
		if prefix, err := netip.ParsePrefix(value.Text()); err == nil {
			out = append(out, prefix.Masked())
		}
	}
	if len(out) != 0 {
		return out
	}
	for _, node := range cluster.Spec().Get("ceph", "topology", "nodes").Items() {
		machine, ok := catalog.Find(api.Machine, textOf(node, "machineRef"))
		if !ok {
			continue
		}
		for _, value := range machine.Spec().Get("network", "addresses").Items() {
			if prefix, err := netip.ParsePrefix(textOf(value, "address")); err == nil {
				out = append(out, prefix.Masked())
			}
		}
	}
	return out
}
func (a *admission) serviceNetworks(cluster api.Object, values api.Value, field string) {
	declared := declaredPublicNetworks(cluster, a.catalog)
	for i, value := range values.Items() {
		prefix, err := netip.ParsePrefix(value.Text())
		if err != nil {
			continue
		}
		covered := false
		for _, network := range declared {
			if network.Contains(prefix.Masked().Addr()) && network.Bits() <= prefix.Bits() {
				covered = true
			}
		}
		if len(declared) != 0 && !covered {
			a.invalid(indexed(field, i), "service network must be reachable through the cluster's declared public networks")
		}
	}
}
func (a *admission) ingress(cluster api.Object, ingress api.Value, field string) {
	ip, err := netip.ParseAddr(textOf(ingress, "address"))
	prefix, ok := ingress.Get("prefixLength").Int64()
	if err == nil && ok && (prefix < 0 || prefix > int64(ip.BitLen())) {
		a.invalid(field+".prefixLength", "ingress prefix is invalid for its address family")
	}
	declared := declaredPublicNetworks(cluster, a.catalog)
	if err == nil && len(declared) > 0 {
		covered := false
		for _, network := range declared {
			if network.Contains(ip) {
				covered = true
			}
		}
		if !covered {
			a.invalid(field+".address", "ingress VIP must be reachable through declared cluster networks")
		}
	}
	a.serviceNetworks(cluster, ingress.Get("virtualInterfaceNetworks"), field+".virtualInterfaceNetworks")
	placed := a.placement(cluster, ingress.Get("placement"), field+".placement", "ingress", false)
	if cluster.Spec().Has("ceph", "topology", "stretch") && ingress.Get("placement", "sites").Len() == 0 {
		for _, site := range cluster.Spec().Get("ceph", "topology", "stretch", "dataSites").Strings() {
			covered := false
			for _, node := range placed {
				if textOf(node, "site") == site {
					covered = true
				}
			}
			if !covered {
				a.invalid(field+".placement", "stretch ingress placement must cover both data sites unless narrowed by site")
			}
		}
	}
}

func (a *admission) gateway(cluster api.Object) {
	spec := a.object.Spec()
	a.placement(cluster, spec.Get("placement"), "$.spec.placement", "rgw", false)
	present := 0
	for _, key := range []string{"realm", "zoneGroup", "zone"} {
		if spec.Has(key) {
			present++
		}
	}
	if present != 0 && present != 3 {
		a.invalid("$.spec.realm", "realm, zone group and zone must be declared together")
	}
	for _, config := range spec.Get("config").Fields() {
		if config.Name == "rgw_frontend_port" {
			a.invalid("$.spec.config", "gateway config cannot own the typed frontend port")
		}
		for _, section := range []string{"global", "client.rgw", "client.rgw." + textOf(spec, "serviceID"), "rgw", textOf(spec, "serviceID")} {
			if cluster.Spec().Get("ceph", "config", section).Has(config.Name) {
				a.invalid("$.spec.config", "gateway config duplicates a same-service cluster config key")
			}
		}
	}
	endpoint := spec.Get("endpoint")
	if !endpoint.Present() {
		return
	}
	if endpoint.Len() == 0 || endpoint.Get("ingresses").Len() == 0 {
		a.invalid("$.spec.endpoint.ingresses", "public endpoint requires at least one ingress")
	}
	if fallback(endpoint.Get("scheme"), "https") == "https" {
		a.required(endpoint, "tls", "$.spec.endpoint")
	} else if textOf(endpoint, "scheme") == "http" {
		a.forbid(endpoint, "$.spec.endpoint", "tls")
	}
	for i, value := range endpoint.Get("ingresses").Items() {
		a.ingress(cluster, value, indexed("$.spec.endpoint.ingresses", i))
	}
}
func (a *admission) nfs(cluster api.Object) {
	spec := a.object.Spec()
	a.placement(cluster, spec.Get("placement"), "$.spec.placement", "", true)
	if spec.Get("ingresses").Len() > 0 && numeric(spec.Get("port")).Int64() == 2049 {
		a.invalid("$.spec.port", "ingress-fronted NFS cannot use backend port 2049")
	}
	for i, value := range spec.Get("ingresses").Items() {
		a.ingress(cluster, value, indexed("$.spec.ingresses", i))
	}
	for i, export := range spec.Get("exports").Items() {
		field := indexed("$.spec.exports", i)
		if export.Has("filesystemRef") == export.Has("bucket") {
			a.invalid(field, "an NFS export requires exactly one filesystem or bucket")
		}
		if export.Has("filesystemRef") {
			a.sameOwner(api.StorageFilesystem, textOf(export, "filesystemRef"), field+".filesystemRef", cluster)
		}
		for _, key := range []string{"pseudo", "path"} {
			if value := export.Get(key); value.Type() == api.String && !cleanAbsolute(value.Text()) {
				a.issue("api.value", field+"."+key, "NFS path must be clean and absolute", "use one clean absolute path")
			}
		}
		seen := map[string]bool{}
		for j, client := range export.Get("clients").Items() {
			canonical := ""
			if prefix, err := netip.ParsePrefix(client.Text()); err == nil {
				canonical = prefix.Masked().String()
			} else if ip, err := netip.ParseAddr(client.Text()); err == nil {
				canonical = ip.String()
			}
			if canonical == "" {
				a.issue("api.value", indexed(field+".clients", j), "NFS client must be an IP or CIDR", "use a literal IP address or CIDR")
			} else if seen[canonical] {
				a.invalid(field+".clients", "NFS clients must be unique after canonicalization")
			}
			seen[canonical] = true
		}
	}
}

func (a *admission) cephEntitlement() {
	spec := a.object.Spec()
	product := textOf(spec, "type")
	if product == "redhat-ceph" {
		for _, issue := range managedos.ValidateRHSM(a.object, true) {
			a.issue(issue.Code, issue.Field, issue.Message, issue.Remediation)
		}
	} else {
		a.forbid(spec, "$.spec", "rhsm")
		if !spec.Get("license", "accept").Bool() {
			a.invalid("$.spec.license.accept", "IBM storage entitlement requires explicit license acceptance")
		}
	}
	a.required(spec, "registry", "$.spec")
	if spec.Has("registry") {
		a.required(spec.Get("registry"), "credentialsRef", "$.spec.registry")
	}
}

func cephProduct(object api.Object) bool {
	return object.Kind() == api.Entitlement && slices.Contains([]string{"redhat-ceph", "ibm-storage-ceph"}, textOf(object.Spec(), "type"))
}
func normalizeCephEntitlement(object api.Object) api.Object {
	spec := object.Spec()
	registry := "registry.redhat.io"
	if textOf(spec, "type") == "ibm-storage-ceph" {
		registry = "cp.icr.io/cp"
	}
	if spec.Has("registry") {
		spec = spec.With("registry", spec.Get("registry").Default("url", api.StringValue(registry)))
	}
	return managedos.NormalizeRHSM(object.WithSpec(spec))
}
