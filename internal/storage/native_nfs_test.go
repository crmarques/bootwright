package storage

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func nfsService(spec api.Value) api.Value {
	return m("service_type", "nfs", "service_id", "files", "placement", m("label", "storage", "count", 2), "spec", spec)
}

func ingressService(spec api.Value) api.Value {
	base := m("backend_service", "rgw.objects", "virtual_ip", "192.0.2.50/24")
	for _, f := range spec.Fields() {
		base = base.With(f.Name, f.Value)
	}
	return m("service_type", "ingress", "service_id", "rgw.objects", "placement", m("label", "storage"), "spec", base)
}

func TestNFSServiceRefusesAnyBinding(t *testing.T) {
	cluster, catalog := withBoundService(t, nfsService(api.MapValue()), leaves("nfs.files", "/spec/ssl_cert", "tls-a"))
	issues := Validate(cluster, catalog)
	found := issuesAt(issues, "$.spec.ceph.serviceBindings[0].serviceRef")
	if len(found) != 1 || found[0].Message != "NFS services take no secret bindings: Bootwright does not support NFS over TLS or kmip yet" || found[0].Remediation != "remove this spec.ceph.serviceBindings entry" {
		t.Fatalf("NFS binding: %#v", found)
	}
	if leaf := issuesUnder(issues, "$.spec.ceph.serviceBindings[0].secrets"); len(leaf) != 0 {
		t.Fatalf("NFS binding leaves were also checked: %#v", leaf)
	}
	issues = graphIssues(t, graphServices(), leaves("ingress.nfs.files", "/spec/ssl_cert", "tls-a"))
	found = issuesAt(issues, "$.spec.ceph.serviceBindings[0].serviceRef")
	if len(found) != 1 || found[0].Message != "the NFS service and its ingress take no secret bindings: Bootwright does not support NFS over TLS or kmip yet" || found[0].Remediation != "remove this spec.ceph.serviceBindings entry" {
		t.Fatalf("NFS ingress binding: %#v", found)
	}
	if leaf := issuesUnder(issues, "$.spec.ceph.serviceBindings[0].secrets"); len(leaf) != 0 {
		t.Fatalf("NFS ingress binding leaves were also checked: %#v", leaf)
	}
}

func TestNFSAndIngressRefusedMembersNameTheirRemedy(t *testing.T) {
	requireRefusedMembers(t, nfsService, groupRemedies(map[string][]string{
		"omit spec.<member>; NFS services serve NFSv4":    {"enable_nfsv3", "enable_nlm"},
		"omit spec.idmap_conf":                            {"idmap_conf"},
		"omit spec.<member>; NFS services serve over TCP": {"enable_rdma", "rdma_port"},
		"omit spec.<member>":                              {"ssl", "ssl_cert", "ssl_key", "ssl_ca_cert", "certificate_source", "custom_sans", "tls_ktls", "tls_debug", "tls_min_version", "tls_ciphers", "kmip_cert", "kmip_key", "kmip_ca_cert", "kmip_host_list", "cluster_qos_config", "cluster_qos_port", "enable_virtual_server", "colocation_ports", "monitoring_networks", "monitoring_ip_addrs", "ip_addrs"},
	}))
	requireRefusedMembers(t, ingressService, groupRemedies(map[string][]string{
		"remove spec.<member>; " + tlsBindingText:                                   {"ssl_cert", "ssl_key"},
		"omit spec.<member>; Bootwright never authors or reads ingress credentials": {"monitor_user", "monitor_password", "keepalived_password"},
		"omit spec.<member>; " + tlsBindingText:                                     {"generate_cert", "custom_sans"},
		"declare spec.virtual_ip instead of spec.virtual_ips_list":                  {"virtual_ips_list"},
		"omit spec.<member>": {"use_keepalived_multicast", "vrrp_interface_network", "enable_stats", "monitor_ssl", "monitor_ssl_cert", "monitor_ssl_key", "monitor_cert_source", "monitor_networks", "monitor_ip_addrs", "verify_backend_ssl_cert", "verification_ca_cert", "use_tcp_mode_over_rgw", "haproxy_qat_support", "ssl_dh_param", "ssl_ciphers", "ssl_options"},
	}))
	cluster, catalog := withMDSService(t, fixtureCatalog(), nfsService(m("enable_rdma", true)))
	found := issuesAt(Validate(cluster, catalog), "$.spec.ceph.services[3].spec.enable_rdma")
	if len(found) != 1 || found[0].Message != "NFS RDMA is not supported by Bootwright yet" {
		t.Fatalf("NFS RDMA refusal: %#v", found)
	}
}

func TestNFSServiceIntrinsicRules(t *testing.T) {
	services := graphServices()
	services[2] = nfsService(m("port", 2049, "monitoring_port", 9587, "virtual_ip", "192.0.2.60", "enable_haproxy_protocol", true))
	requireGraphAdmits(t, graphIssues(t, services))
	for _, tt := range []struct {
		name, field, message string
		service              api.Value
	}{
		{"count_per_host", ".placement.count_per_host", "cephadm refuses count_per_host for NFS services", nfsService(api.MapValue()).With("placement", m("label", "storage", "count_per_host", 1))},
		{"prefixed VIP", ".spec.virtual_ip", "an NFS virtual_ip is a bare address without a prefix length", nfsService(m("virtual_ip", "192.0.2.60/24"))},
		{"zoned VIP", ".spec.virtual_ip", "an NFS virtual_ip is a bare address without a prefix length", nfsService(m("virtual_ip", "fe80::1%eth0"))},
		{"mapped VIP", ".spec.virtual_ip", "an NFS virtual_ip is a bare address without a prefix length", nfsService(m("virtual_ip", "::ffff:192.0.2.60"))},
		{"VIP with networks", ".spec.virtual_ip", "cephadm refuses virtual_ip together with service networks", nfsService(m("virtual_ip", "192.0.2.60")).With("networks", api.StringList("192.0.2.0/24"))},
	} {
		t.Run(tt.name, func(t *testing.T) {
			services := graphServices()
			services[2] = tt.service
			found := issuesAt(graphIssues(t, services), nfsAt+tt.field)
			if len(found) != 1 || found[0].Message != tt.message || found[0].Code != "api.invariant" {
				t.Fatalf("NFS rule: %#v", found)
			}
			if strings.Contains(found[0].Message+found[0].Remediation, "192.0.2") {
				t.Fatalf("NFS rule echoed an authored address: %#v", found[0])
			}
		})
	}
}

func TestMgrModulesRefuseFamilyModulesAsDerived(t *testing.T) {
	catalog := fixtureCatalog()
	cluster := find(t, catalog, api.StorageCluster, "storage")
	validate := func(modules ...string) []api.Issue {
		changed := cluster.WithSpec(cluster.Spec().WithPath(api.StringList(modules...), "ceph", "mgrModules"))
		return Validate(changed, replace(catalog, changed))
	}
	for _, module := range []string{"nfs"} {
		issues := validate("cephadm", module, "rbd_support")
		found := issuesAt(issues, "$.spec.ceph.mgrModules[1]")
		if len(found) != 1 || found[0].Message != "manager module "+module+" is derived from the declared "+module+" services" || found[0].Remediation != "remove "+module+" from spec.ceph.mgrModules; Bootwright enables it for a declared "+module+" service" {
			t.Fatalf("derived module %s: %#v", module, found)
		}
		if generic := issuesAt(issues, "$.spec.ceph.mgrModules"); len(generic) != 0 {
			t.Fatalf("derived module %s also refused generically: %#v", module, generic)
		}
	}
	found := issuesAt(validate("iostat"), "$.spec.ceph.mgrModules")
	if len(found) != 1 || found[0].Message != "manager module requires its qualified service owner" {
		t.Fatalf("unowned module: %#v", found)
	}
	if found := issuesUnder(validate("cephadm", "rbd_support"), "$.spec.ceph.mgrModules"); len(found) != 0 {
		t.Fatalf("allowed modules refused: %#v", found)
	}
}
