package storage

import (
	"net/netip"

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
	if address, err := netip.ParseAddr(vip.Text()); vip.Type() == api.String && (err != nil || address.Zone() != "" || address.Is4In6()) {
		a.issue("api.invariant", field+".spec.virtual_ip", "an NFS virtual_ip is a bare address without a prefix length", "write spec.virtual_ip as the ingress virtual IP's address without a prefix length")
	}
	if service.Get("networks").Len() != 0 {
		a.issue("api.invariant", field+".spec.virtual_ip", "cephadm refuses virtual_ip together with service networks", "omit networks on this NFS service or omit spec.virtual_ip")
	}
}
