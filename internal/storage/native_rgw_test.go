package storage

import (
	"fmt"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

const tlsBindingText = "bind /spec/ssl_cert and /spec/ssl_key to one tlsCertificate Secret in spec.ceph.serviceBindings"

func rgwService(spec api.Value) api.Value {
	return m("service_type", "rgw", "service_id", "objects", "placement", m("label", "storage", "count", 2), "networks", api.StringList("192.0.2.0/24"), "spec", spec)
}

func qualifiedRGWSpec() api.Value {
	return m("rgw_realm", "realm", "rgw_zonegroup", "group", "rgw_zone", "zone", "rgw_frontend_port", 8080, "rgw_frontend_type", "beast", "only_bind_port_on_networks", true, "rgw_exit_timeout_secs", 60)
}

func issuesUnder(issues []api.Issue, prefix string) []api.Issue {
	found := []api.Issue{}
	for _, issue := range issues {
		if issue.Field == prefix || strings.HasPrefix(issue.Field, prefix+".") || strings.HasPrefix(issue.Field, prefix+"[") {
			found = append(found, issue)
		}
	}
	return found
}

func requireRefusedMembers(t *testing.T, service func(api.Value) api.Value, members map[string]string) {
	t.Helper()
	for member, remedy := range members {
		cluster, catalog := withMDSService(t, fixtureCatalog(), service(m(member, "never-echo-this")))
		issues := Validate(cluster, catalog)
		found := issuesAt(issues, "$.spec.ceph.services[3].spec."+member)
		if len(found) != 1 || found[0].Code != "api.invariant" || found[0].Remediation != remedy {
			t.Fatalf("refused member %s: %#v", member, found)
		}
		if at := issuesAt(issues, "$.spec.ceph.services[3].spec"); len(at) != 0 {
			t.Fatalf("refused member %s also refused generically: %#v", member, at)
		}
		if strings.Contains(fmt.Sprint(issues), "never-echo-this") {
			t.Fatalf("refused member %s echoed its value", member)
		}
	}
}

func groupRemedies(groups map[string][]string) map[string]string {
	out := map[string]string{}
	for remedy, members := range groups {
		for _, member := range members {
			out[member] = strings.ReplaceAll(remedy, "<member>", member)
		}
	}
	return out
}

func TestRGWNativeRecordAdmitsQualifiedFields(t *testing.T) {
	cluster, catalog := withMDSService(t, fixtureCatalog(), rgwService(qualifiedRGWSpec()))
	if found := issuesUnder(Validate(cluster, catalog), "$.spec.ceph.services[3]"); len(found) != 0 {
		t.Fatalf("qualified RGW record refused: %#v", found)
	}
	requireRefusedMembers(t, rgwService, groupRemedies(map[string][]string{
		"omit spec.rgw_frontend_ssl_certificate; set spec.ssl: true and " + tlsBindingText: {"rgw_frontend_ssl_certificate"},
		"remove spec.<member>; " + tlsBindingText:                                          {"ssl_cert", "ssl_key"},
		"omit spec.<member>; " + tlsBindingText:                                            {"generate_cert", "custom_sans"},
		"omit spec.<member>; clients use path-style bucket addressing":                     {"wildcard_enabled", "zonegroup_hostnames"},
		"omit spec.<member>; B483 delivers RGW multisite":                                  {"zone_endpoints", "rgw_realm_token", "update_endpoints", "disable_multisite_sync_traffic"},
		"omit spec.data_pool_attributes; declare the pool as a StoragePool with role rgw":  {"data_pool_attributes"},
		"omit spec.rgw_frontend_extra_args":                                                {"rgw_frontend_extra_args"},
		"omit spec.<member>":                                                               {"subcluster", "rgw_user_counters_cache", "rgw_user_counters_cache_size", "rgw_bucket_counters_cache", "rgw_bucket_counters_cache_size", "d3n_cache", "qat", "concentrator", "concentrator_frontend_port", "concentrator_monitor_port", "concentrator_monitor_user", "concentrator_monitor_password"},
	}))
	signedRemedy := "omit spec.certificate_source or set it to inline, and " + tlsBindingText
	for _, tt := range []struct {
		name, field, message string
		service              api.Value
	}{
		{"cephadm-signed", ".spec.certificate_source", "cephadm-signed certificates are not supported by Bootwright yet", rgwService(qualifiedRGWSpec().With("ssl", api.BoolValue(true)).With("certificate_source", api.StringValue("cephadm-signed")))},
		{"reference", ".spec.certificate_source", "certificate manager references are not qualified", rgwService(qualifiedRGWSpec().With("ssl", api.BoolValue(true)).With("certificate_source", api.StringValue("reference")))},
		{"civetweb", ".spec.rgw_frontend_type", "only the beast frontend is qualified", rgwService(qualifiedRGWSpec().With("rgw_frontend_type", api.StringValue("civetweb")))},
		{"zero exit timeout", ".spec.rgw_exit_timeout_secs", "native numeric value is outside its supported bounds", rgwService(qualifiedRGWSpec().With("rgw_exit_timeout_secs", api.IntegerValue("0")))},
		{"zero port", ".spec.rgw_frontend_port", "native numeric value is outside its supported bounds", rgwService(qualifiedRGWSpec().With("rgw_frontend_port", api.IntegerValue("0")))},
		{"large port", ".spec.rgw_frontend_port", "native numeric value is outside its supported bounds", rgwService(qualifiedRGWSpec().With("rgw_frontend_port", api.IntegerValue("65536")))},
		{"realm without zone", ".spec.rgw_realm", "cephadm requires rgw_realm and rgw_zone together", rgwService(qualifiedRGWSpec().Without("rgw_zone"))},
		{"zone without realm", ".spec.rgw_zone", "cephadm requires rgw_realm and rgw_zone together", rgwService(qualifiedRGWSpec().Without("rgw_realm"))},
		{"colocated daemons", ".placement.count_per_host", "colocated RGW daemons need port assignments that are not qualified", rgwService(qualifiedRGWSpec()).With("placement", m("label", "storage", "count_per_host", 2))},
		{"bind without networks", ".spec.only_bind_port_on_networks", "only_bind_port_on_networks needs the service networks it binds to", rgwService(qualifiedRGWSpec()).Without("networks")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cluster, catalog := withMDSService(t, fixtureCatalog(), tt.service)
			found := issuesAt(Validate(cluster, catalog), "$.spec.ceph.services[3]"+tt.field)
			if len(found) != 1 || found[0].Message != tt.message {
				t.Fatalf("RGW value rule: %#v", found)
			}
			if tt.field == ".spec.certificate_source" && found[0].Remediation != signedRemedy {
				t.Fatalf("certificate source remedy: %q", found[0].Remediation)
			}
		})
	}
}
