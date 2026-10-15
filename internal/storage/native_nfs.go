package storage

import (
	"math/big"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

var nfsRefusedMembers = []refusedMember{
	{names: []string{"enable_nfsv3", "enable_nlm"}, message: "NFSv3 is not supported by Bootwright yet", remedy: "omit spec.<member>; NFS services serve NFSv4"},
	{names: []string{"idmap_conf"}, message: "Kerberos and ID mapping are not supported by Bootwright yet", remedy: "omit spec.idmap_conf"},
	{names: []string{"enable_rdma", "rdma_port"}, message: "NFS RDMA is not supported by Bootwright yet", remedy: "omit spec.<member>; NFS services serve over TCP"},
	{names: []string{"ssl", "ssl_cert", "ssl_key", "ssl_ca_cert", "certificate_source", "custom_sans", "tls_ktls", "tls_debug", "tls_min_version", "tls_ciphers"}, message: "NFS over TLS is not supported by Bootwright yet", remedy: "omit spec.<member>"},
	{names: []string{"kmip_cert", "kmip_key", "kmip_ca_cert", "kmip_host_list"}, message: "kmip is not supported by Bootwright yet", remedy: "omit spec.<member>"},
	{names: []string{"cluster_qos_config", "cluster_qos_port"}, message: "NFS cluster QoS is not supported by Bootwright yet", remedy: "omit spec.<member>"},
	{names: []string{"enable_virtual_server", "colocation_ports", "monitoring_networks", "monitoring_ip_addrs", "ip_addrs"}, message: "this NFS option has no qualified effect contract", remedy: "omit spec.<member>"},
}

func (a *admission) nfsRecord(service api.Value, field string) {
	if service.Has("placement", "count_per_host") {
		a.issue("api.invariant", field+".placement.count_per_host", "cephadm refuses count_per_host for NFS services", "omit placement.count_per_host")
	}
	vip := service.Get("spec", "virtual_ip")
	if !vip.Present() {
		return
	}
	if _, ok := ParseVirtualIP(vip.Text(), false); vip.Type() == api.String && !ok {
		a.issue("api.invariant", field+".spec.virtual_ip", "an NFS virtual_ip is a bare address without a prefix length", "write spec.virtual_ip as the ingress virtual IP's address without a prefix length")
	}
	if service.Get("networks").Len() != 0 {
		a.issue("api.invariant", field+".spec.virtual_ip", "cephadm refuses virtual_ip together with service networks", "omit networks on this NFS service or omit spec.virtual_ip")
	}
}

const nfsFlagsDiffer = "enable_haproxy_protocol must be equal on the NFS service and its ingress"

func (a *admission) nfsDirect(service api.Value, field string) {
	spec := service.Get("spec")
	if spec.Has("virtual_ip") {
		a.issue("api.invariant", field+".spec.virtual_ip", "an NFS virtual_ip needs an ingress service that fronts this NFS service", "declare an ingress for this NFS service or omit spec.virtual_ip")
	}
	if spec.Get("enable_haproxy_protocol").Bool() {
		a.issue("api.invariant", field+".spec.enable_haproxy_protocol", "the HAProxy protocol needs an ingress with the same setting", "declare an ingress with enable_haproxy_protocol: true for this NFS service or omit the flag")
	}
}

func (a *admission) nfsMode(service api.Value, field string, ingress api.Value, ingressField string) {
	spec, front := service.Get("spec"), ingress.Get("spec")
	flag := spec.Get("enable_haproxy_protocol").Bool()
	hosts, _ := ResolveServicePlacement(a.object, service)
	count := service.Get("placement", "count")
	switch {
	case front.Get("keepalive_only").Bool():
		if !count.Present() || numeric(count).Cmp(big.NewInt(1)) != 0 {
			a.issue("api.invariant", field+".placement.count", "an NFS service behind a keepalive-only ingress runs exactly one daemon", "set placement.count: 1 on the NFS service")
		}
		if len(hosts) < 2 {
			a.issue("api.invariant", field+".placement", "a keepalive-only NFS service needs at least two eligible hosts to move to", "select at least two declared storage hosts in the NFS placement")
		}
		if !spec.Has("virtual_ip") {
			a.issue("api.required", field+".spec.virtual_ip", "a keepalive-only NFS service binds the ingress virtual IP", "set spec.virtual_ip to the ingress virtual IP's address")
		}
		a.nfsVIPMatches(spec, front, field)
		if flag {
			a.issue("api.invariant", field+".spec.enable_haproxy_protocol", "keepalive-only and HAProxy protocol modes are exclusive", "omit spec.enable_haproxy_protocol on the NFS service")
		}
	case front.Get("enable_haproxy_protocol").Bool():
		if !flag {
			a.issue("api.invariant", field+".spec.enable_haproxy_protocol", nfsFlagsDiffer, "set enable_haproxy_protocol: true on both")
		}
		if !count.Present() || numeric(count).Cmp(big.NewInt(int64(len(hosts)))) >= 0 {
			a.issue("api.invariant", field+".placement.count", "a multi-active NFS service needs an explicit placement count below its eligible host count", "set placement.count below the number of eligible NFS hosts")
		}
		a.nfsVIPMatches(spec, front, field)
	default:
		a.issue("api.invariant", ingressField+".spec.enable_haproxy_protocol", "an NFS ingress without the HAProxy protocol hides client addresses, so export client restrictions would not function", "set enable_haproxy_protocol: true on both the NFS service and its ingress, or set keepalive_only: true on the ingress")
		if flag {
			a.issue("api.invariant", field+".spec.enable_haproxy_protocol", nfsFlagsDiffer, "set enable_haproxy_protocol: true on both")
		}
	}
}

func (a *admission) nfsVIPMatches(spec, front api.Value, field string) {
	address, ok := ParseVirtualIP(textOf(spec, "virtual_ip"), false)
	prefix, valid := ParseVirtualIP(textOf(front, "virtual_ip"), true)
	if ok && valid && address.Addr() != prefix.Addr() {
		a.issue("api.invariant", field+".spec.virtual_ip", "the NFS virtual_ip must equal its ingress virtual IP address", "set spec.virtual_ip to the ingress virtual IP's address without a prefix length")
	}
}
