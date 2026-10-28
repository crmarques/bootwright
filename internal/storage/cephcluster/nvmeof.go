package cephcluster

import (
	"math/big"
	"net/netip"
	"regexp"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

const (
	nvmeofListenerPort = int64(4420)
	nvmeofUUIDPrefix   = "nqn.2014-08.org.nvmexpress:uuid:"
	nvmeofNQNMessage   = "an NQN is nqn.yyyy-mm.<reverse domain>:<name> or nqn.2014-08.org.nvmexpress:uuid:<UUID>, of 11 to 223 printable ASCII characters, and never the discovery NQN"
	nvmeofDomainLabel  = `[A-Za-z](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?`
)

var (
	nvmeofUUIDNQN  = regexp.MustCompile(`^` + regexp.QuoteMeta(nvmeofUUIDPrefix) + `[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$`)
	nvmeofNamedNQN = regexp.MustCompile(`^nqn\.[0-9]{4}-(?:0[1-9]|1[0-2])\.` + nvmeofDomainLabel + `(?:\.` + nvmeofDomainLabel + `)*:[!-~]+$`)
)

var nvmeofRefusedMembers = []refusedMember{
	{names: []string{"server_key", "server_cert", "client_key", "client_cert", "root_ca_cert"}, message: "native documents never carry TLS material inline", remedy: "remove spec.<member>; bind gateway mTLS through spec.serviceBindings"},
	{names: []string{"encryption_key"}, message: "native documents never carry key material inline", remedy: "remove spec.encryption_key; NVMe-oF in-band authentication is not supported by Bootwright yet"},
	{names: []string{"addr", "addr_map", "discovery_addr", "discovery_addr_map"}, message: "an NVMe-oF gateway binds its host's registered address, which its subsystem listeners also use", remedy: "omit spec.<member>"},
	{names: []string{"enable_prometheus_exporter", "prometheus_port"}, message: "this NVMe-oF option has no qualified effect contract; the gateway exporter keeps its native default", remedy: "omit spec.<member>"},
}

func validNQN(nqn string) bool {
	if len(nqn) < 11 || len(nqn) > 223 {
		return false
	}
	if strings.HasPrefix(nqn, nvmeofUUIDPrefix) {
		return nvmeofUUIDNQN.MatchString(nqn)
	}
	return nvmeofNamedNQN.MatchString(nqn)
}

func (a *admission) nvmeofClusterIntrinsic(ceph api.Value) {
}

func (a *admission) nvmeofRecord(service api.Value, field string, placed []api.Value, partial bool) {
	spec := service.Get("spec")

	if group := spec.Get("group"); group.Type() == api.String && !nativeID.MatchString(group.Text()) {
		a.issue("api.invariant", field+".spec.group", "an NVMe-oF gateway group name uses the qualified name grammar [A-Za-z0-9][A-Za-z0-9_.-]*", "rename spec.group to a name that starts with a letter or digit and holds only letters, digits, '_', '.' and '-'")
	}
	if numeric(service.Get("placement", "count_per_host")).Cmp(big.NewInt(1)) > 0 {
		a.issue("api.invariant", field+".placement.count_per_host", "a host runs one gateway of an NVMe-oF group, which carries that host's subsystem listeners", "omit placement.count_per_host or set it to 1")
	}
	if partial {
		return
	}
	if a.object.Spec().Has("topology", "stretch") {
		a.deferred(field, "NVMe-oF gateways on a stretch cluster need the stretch capacity gate", "this nvmeof service from spec.services", "B487")
	}
	if len(placed) == 1 {
		a.issue("api.invariant", field+".placement", "a gateway group with one gateway raises the NVMEOF_SINGLE_GATEWAY health warning, so the cluster never reports HEALTH_OK", "select at least two declared storage hosts in this nvmeof placement")
	}
	if count := service.Get("placement", "count"); count.Present() && numeric(count).Cmp(big.NewInt(int64(len(placed)))) < 0 {
		a.issue("api.invariant", field+".placement.count", "every eligible host runs a gateway of this group, because each gateway serves its own subsystem listeners", "omit placement.count or set it to the number of eligible hosts")
	}
	a.nvmeofPool(textOf(spec, "pool"), field+".spec.pool")
}

func (a *admission) nvmeofPool(name, field string) {
	if name == "" {
		return
	}
	pool, ok := a.catalog.Find(api.CephPool, name)
	switch {
	case !ok || textOf(pool.Spec(), "clusterRef") != a.object.Name():
		a.issue("api.invariant", field, "spec.pool names no CephPool of CephCluster "+a.object.Name()+"; cephadm refuses to deploy a gateway whose pool is absent", "declare a replicated CephPool with role rbd for this cluster and set spec.pool to its name")
	case textOf(pool.Spec(), "role") != "rbd":
		a.issue("api.invariant", field, "an NVMe-oF gateway keeps its state in an RBD pool, so spec.pool must name a CephPool with role rbd", "set spec.pool to a replicated CephPool with role rbd")
	case PoolProtection(pool.Spec()) == "erasure":
		a.issue("api.invariant", field, "an NVMe-oF gateway keeps its state in omap, which erasure-coded pools do not support, so this pool must be replicated", "set spec.pool to a replicated CephPool with role rbd")
	}
}

type nvmeofGateway struct {
	field, group string
	hosts        []string
}

func (a *admission) nvmeofGraph(ceph api.Value) {
	gateways := []nvmeofGateway{}
	for i, service := range ceph.Get("services").Items() {
		if textOf(service, "service_type") != "nvmeof" {
			continue
		}
		hosts, _ := ResolveServicePlacement(a.object, service)
		gateway := nvmeofGateway{field: indexed("$.spec.services", i), group: textOf(service, "spec", "group"), hosts: hosts}
		for _, earlier := range gateways {
			if gateway.group != "" && gateway.group == earlier.group {
				a.issue("api.invariant", gateway.field+".spec.group", "the nvmeof service "+earlier.field+" already serves this gateway group", "give each nvmeof service its own spec.group")
			}
			if at := slices.IndexFunc(gateway.hosts, func(host string) bool { return slices.Contains(earlier.hosts, host) }); at >= 0 {
				a.issue("api.invariant", gateway.field+".placement", "cephadm places no gateway on a host that runs a gateway of another nvmeof service, and host "+gateway.hosts[at]+" is eligible for both this service and "+earlier.field, "select disjoint hosts for the two nvmeof services")
			}
		}
		gateways = append(gateways, gateway)
	}
}

func nvmeofListeners(cluster api.Object, catalog api.Catalog, service api.Value, field string, index int) []listener {
	out := portListener(5500, false)(cluster, catalog, service, field, index)
	hosts, _ := ResolveServicePlacement(cluster, service)
	if port, ok := listenerPort(service.Get("spec"), "discovery_port", 8009); ok {
		out = append(out, serviceListener(field+".spec.discovery_port", index, port, bindAll(hosts, netip.Addr{})))
	}
	out = append(out, labelledListener(cluster, service, field, index, nvmeofListenerPort, "the NVMe/TCP subsystem listener port (4420)"))
	return append(out, labelledListener(cluster, service, field, index, 10008, "the NVMe-oF gateway exporter (10008)"))
}

func (a *admission) nvmeofSubsystemIntrinsic() {
	spec := a.object.Spec()
	for _, key := range []string{"hosts", "namespaces"} {
		if a.object.Spec().Get(key).Len() > 128 {
			a.invalid("$.spec."+key, "the bounded NVMe-oF resource limit is 128 entries", "reduce this list to at most 128 entries")
		}
	}
	if nqn := spec.Get("nqn"); nqn.Type() == api.String && !validNQN(nqn.Text()) {
		a.issue("api.invariant", "$.spec.nqn", nvmeofNQNMessage, "write spec.nqn as the subsystem's NQN in one of those forms")
	}
	for i, host := range spec.Get("hosts").Items() {
		field := indexed("$.spec.hosts", i)
		switch {
		case host.Type() != api.String:
		case host.Text() == "*":
			a.issue("api.invariant", field, "Bootwright never allows every host to connect to a subsystem", "list each initiator's host NQN in spec.hosts")
		case !validNQN(host.Text()):
			a.issue("api.invariant", field, nvmeofNQNMessage, "write this entry as the initiator's host NQN in one of those forms")
		}
	}
	for i, listener := range spec.Get("listeners").Items() {
		if port := listener.Get("trsvcid"); port.Type() == api.Integer && numeric(port).Cmp(big.NewInt(nvmeofListenerPort)) != 0 {
			a.issue("api.invariant", indexed("$.spec.listeners", i)+".trsvcid", "cephadm reserves and opens only port 4420 for NVMe/TCP listeners on a gateway host", "omit trsvcid or set it to 4420")
		}
	}
	images := []string{}
	for i, namespace := range spec.Get("namespaces").Items() {
		image := textOf(namespace, "imageRef")
		if image != "" && slices.Contains(images, image) {
			a.issue("api.invariant", indexed("$.spec.namespaces", i)+".imageRef", "an RBD image backs at most one namespace, and an earlier namespace of this subsystem already uses this one", "back each namespace with its own CephRBDImage")
		}
		images = append(images, image)
	}
}

func (a *admission) nvmeofSubsystem(cluster api.Object) {
	spec := a.object.Spec()
	for _, key := range []string{"serviceRef", "nqn", "listeners", "hosts", "namespaces"} {
		a.required(spec, key, "$.spec")
	}
	a.nvmeofSubsystemService(cluster, spec)
	peers := []api.Object{}
	for _, other := range a.catalog.OfKind(api.CephNVMeoFSubsystem) {
		if other.Name() != a.object.Name() && textOf(other.Spec(), "clusterRef") == cluster.Name() {
			peers = append(peers, other)
		}
	}
	for _, peer := range peers {
		if spec.Has("nqn") && textOf(peer.Spec(), "nqn") == textOf(spec, "nqn") {
			a.issue("api.invariant", "$.spec.nqn", "CephNVMeoFSubsystem "+peer.Name()+" of this cluster already declares this NQN", "give each subsystem of the cluster its own NQN")
			break
		}
	}
	for i, namespace := range spec.Get("namespaces").Items() {
		field := indexed("$.spec.namespaces", i) + ".imageRef"
		image := textOf(namespace, "imageRef")
		if _, ok := a.sameOwner(api.CephRBDImage, image, field, cluster); !ok {
			continue
		}
		for _, peer := range peers {
			if slices.ContainsFunc(peer.Spec().Get("namespaces").Items(), func(other api.Value) bool { return textOf(other, "imageRef") == image }) {
				a.issue("api.invariant", field, "an RBD image backs at most one namespace, and CephNVMeoFSubsystem "+peer.Name()+" already exposes this one", "back each namespace with its own CephRBDImage")
				break
			}
		}
	}
}

func (a *admission) nvmeofSubsystemService(cluster api.Object, spec api.Value) {
	if !spec.Has("serviceRef") {
		return
	}
	service, ok := NativeService(cluster, textOf(spec, "serviceRef"))
	switch {
	case !ok:
		return
	case textOf(service, "service_type") != "nvmeof":
		a.issue("api.invariant", "$.spec.serviceRef", "spec.serviceRef must name an nvmeof service; it names a "+textOf(service, "service_type")+" service", "set spec.serviceRef to the qualified name nvmeof.<service_id> of a declared nvmeof service")
		return
	}
	eligible, issues := ResolveServicePlacement(cluster, service)
	if len(issues) != 0 {
		return
	}
	covered := []string{}
	for i, listener := range spec.Get("listeners").Items() {
		host := textOf(listener, "host")
		if !slices.Contains(eligible, host) {
			a.issue("api.invariant", indexed("$.spec.listeners", i)+".host", "a listener runs on a gateway, and this host is not eligible for the nvmeof service spec.serviceRef names", "set host to the topology node name of a host in that service's placement")
			continue
		}
		covered = append(covered, host)
	}
	for _, host := range eligible {
		if !slices.Contains(covered, host) {
			a.issue("api.invariant", "$.spec.listeners", "a namespace is reachable only through a listener on the gateway that owns its load-balancing group, and topology node "+host+" runs a gateway of this group with no listener of this subsystem", "add a listener for every host the nvmeof service spec.serviceRef names places a gateway on")
			return
		}
	}
}
