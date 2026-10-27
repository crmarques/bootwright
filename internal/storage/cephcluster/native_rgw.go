package cephcluster

import (
	"math/big"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

const tlsBindingRemedy = "bind /spec/ssl_cert and /spec/ssl_key to one tlsCertificate Secret in spec.serviceBindings"

var rgwRefusedMembers = []refusedMember{
	{names: []string{"rgw_frontend_ssl_certificate"}, message: "cephadm migrates the legacy combined certificate into ssl_cert and ssl_key, so the stored service would not match this document", remedy: "omit spec.rgw_frontend_ssl_certificate; set spec.ssl: true and " + tlsBindingRemedy},
	{names: []string{"ssl_cert", "ssl_key"}, message: "native documents never carry TLS material inline", remedy: "remove spec.<member>; " + tlsBindingRemedy},
	{names: []string{"generate_cert", "custom_sans"}, message: "cephadm-signed certificates are not supported by Bootwright yet", remedy: "omit spec.<member>; " + tlsBindingRemedy},
	{names: []string{"wildcard_enabled", "zonegroup_hostnames"}, message: "RGW virtual-hosted style is not supported by Bootwright yet", remedy: "omit spec.<member>; clients use path-style bucket addressing"},
	{names: []string{"zone_endpoints", "rgw_realm_token", "update_endpoints", "disable_multisite_sync_traffic"}, message: "RGW multisite options belong to B483", remedy: "omit spec.<member>; B483 delivers RGW multisite"},
	{names: []string{"data_pool_attributes"}, message: "CephPool objects own the zone's pool attributes", remedy: "omit spec.data_pool_attributes; declare the pool as a CephPool with role rgw"},
	{names: []string{"rgw_frontend_extra_args"}, message: "frontend arguments carry capabilities with no qualified effect contract", remedy: "omit spec.rgw_frontend_extra_args"},
	{names: []string{"subcluster", "rgw_user_counters_cache", "rgw_user_counters_cache_size", "rgw_bucket_counters_cache", "rgw_bucket_counters_cache_size", "d3n_cache", "qat", "concentrator", "concentrator_frontend_port", "concentrator_monitor_port", "concentrator_monitor_user", "concentrator_monitor_password"}, message: "this RGW option has no qualified effect contract", remedy: "omit spec.<member>"},
}

func (a *admission) rgwRecord(service api.Value, field string, partial bool) {
	spec := service.Get("spec")
	a.certificateSource(spec, field)
	if textOf(spec, "rgw_frontend_type") == "civetweb" {
		a.issue("api.invariant", field+".spec.rgw_frontend_type", "only the beast frontend is qualified", "set spec.rgw_frontend_type to beast or omit it")
	}
	for _, pair := range [][2]string{{"rgw_realm", "rgw_zone"}, {"rgw_zone", "rgw_realm"}} {
		if spec.Has(pair[0]) && !spec.Has(pair[1]) {
			a.issue("api.invariant", field+".spec."+pair[0], "cephadm requires rgw_realm and rgw_zone together", "declare both spec.rgw_realm and spec.rgw_zone, or omit both")
		}
	}
	if numeric(service.Get("placement", "count_per_host")).Cmp(big.NewInt(1)) > 0 {
		a.issue("api.invariant", field+".placement.count_per_host", "colocated RGW daemons need port assignments that are not qualified", "omit placement.count_per_host or set it to 1")
	}
	if spec.Get("only_bind_port_on_networks").Bool() && service.Get("networks").Len() == 0 {
		a.issue("api.invariant", field+".spec.only_bind_port_on_networks", "only_bind_port_on_networks needs the service networks it binds to", "declare networks on this service or omit spec.only_bind_port_on_networks")
	}
	if !partial {
		a.tlsBindingRules(service, field, !frontedByIngress(a.object, service))
	}
}

func (a *admission) certificateSource(spec api.Value, field string) {
	sourceRemedy := "omit spec.certificate_source or set it to inline, and " + tlsBindingRemedy
	switch textOf(spec, "certificate_source") {
	case "cephadm-signed":
		a.issue("api.invariant", field+".spec.certificate_source", "cephadm-signed certificates are not supported by Bootwright yet", sourceRemedy)
	case "reference":
		a.issue("api.invariant", field+".spec.certificate_source", "certificate manager references are not qualified", sourceRemedy)
	}
}

func (a *admission) tlsBindingRules(service api.Value, field string, terminates bool) {
	spec := service.Get("spec")
	if spec.Has("certificate_source") && !spec.Get("ssl").Bool() {
		a.issue("api.invariant", field+".spec.certificate_source", "cephadm ignores certificate_source unless ssl is true", "set spec.ssl: true or omit spec.certificate_source")
	}
	if terminates && spec.Get("ssl").Bool() && !serviceBindsLeaves(a.object, service, "/spec/ssl_cert", "/spec/ssl_key") {
		a.issue("api.invariant", field+".spec.ssl", "TLS needs the /spec/ssl_cert and /spec/ssl_key bindings of one tlsCertificate Secret", "add a spec.serviceBindings entry for this service that binds /spec/ssl_cert and /spec/ssl_key to one tlsCertificate Secret; cephadm-signed certificates are not supported by Bootwright yet")
	}
}

func (a *admission) rgwBehindIngress(service api.Value, field string) {
	if hosts, _ := ResolveServicePlacement(a.object, service); len(hosts) < 2 {
		a.issue("api.invariant", field+".placement", "an RGW service behind an ingress needs at least two eligible hosts", "select at least two declared storage hosts in the RGW placement")
	}
	if count := service.Get("placement", "count"); count.Present() && numeric(count).Cmp(big.NewInt(2)) < 0 {
		a.issue("api.invariant", field+".placement.count", "an RGW service behind an ingress runs at least two daemons", "set placement.count to 2 or more, or omit it")
	}
	if service.Get("spec", "ssl").Bool() {
		a.issue("api.invariant", field+".spec.ssl", "TLS terminates at the ingress, not at the RGW daemons", "set spec.ssl: false on the RGW service and bind the certificate to its ingress")
	}
}
