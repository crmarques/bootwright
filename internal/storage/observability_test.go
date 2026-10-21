package storage

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func withServices(t *testing.T, catalog api.Catalog, services ...api.Value) (api.Object, api.Catalog) {
	t.Helper()
	cluster := find(t, catalog, api.StorageCluster, "storage")
	all := append(cluster.Spec().Get("ceph", "services").Items(), services...)
	changed := cluster.WithSpec(cluster.Spec().WithPath(api.ListValue(all...), "ceph", "services"))
	return changed, replace(catalog, changed)
}

func b479Service(serviceType string, spec ...any) api.Value {
	service := m("service_type", serviceType, "placement", m("label", "mgr"))
	if len(spec) != 0 {
		service = service.With("spec", m(spec...))
	}
	return service
}

func b479Minimal() map[string]api.Value {
	return map[string]api.Value{
		"prometheus":    b479Service("prometheus"),
		"alertmanager":  b479Service("alertmanager"),
		"grafana":       b479Service("grafana"),
		"node-exporter": b479Service("node-exporter"),
		"ceph-exporter": b479Service("ceph-exporter"),
		"snmp-gateway":  b479Service("snmp-gateway", "snmp_version", "V2c", "snmp_destination", "192.0.2.9:162"),
		"mgmt-gateway":  b479Service("mgmt-gateway", "port", 9443),
		"oauth2-proxy":  oauth2Proxy(),
		"loki":          b479Service("loki"),
		"alloy":         b479Service("alloy"),
		"promtail":      b479Service("promtail"),
	}
}

func oauth2Proxy(extra ...any) api.Value {
	return b479Service("oauth2-proxy", append([]any{"provider_display_name", "IdP", "client_id", "ceph", "oidc_issuer_url", "https://idp.example.test/realms/ceph"}, extra...)...)
}

func authGateway(extra ...any) api.Value {
	return b479Service("mgmt-gateway", append([]any{"port", 9443, "enable_auth", true}, extra...)...)
}

func addedServiceIssues(issues []api.Issue) []api.Issue {
	found := []api.Issue{}
	for _, issue := range issues {
		rest, ok := strings.CutPrefix(issue.Field, "$.spec.ceph.services[")
		index, _, _ := strings.Cut(rest, "]")
		if n, err := strconv.Atoi(index); ok && err == nil && n >= 3 {
			found = append(found, issue)
		}
	}
	return found
}

func TestB479UnknownSpecMembersRefuse(t *testing.T) {
	for serviceType, service := range b479Minimal() {
		spec := service.Get("spec")
		if !spec.Present() {
			spec = m()
		}
		cluster, catalog := withServices(t, fixtureCatalog(), service.With("spec", spec.With("unknown_member", api.StringValue("x"))))
		full := issuesAt(Validate(cluster, catalog), "$.spec.ceph.services[3].spec")
		partial := issuesAt(ValidatePartial(cluster, catalog), "$.spec.ceph.services[3].spec")
		if len(full) != 1 || full[0].Code != "api.field" || !slices.Equal(full, partial) {
			t.Fatalf("%s unknown member: full %#v partial %#v", serviceType, full, partial)
		}
	}
	cluster, catalog := withServices(t, fixtureCatalog(), b479Service("alertmanager", "user_data", m("default_webhook_urls", api.StringList("https://hooks.example.test/a"), "extra", m("nested", "x"))))
	if found := issuesAt(Validate(cluster, catalog), "$.spec.ceph.services[3].spec.user_data.extra"); len(found) != 1 {
		t.Fatalf("alertmanager user_data key: %#v", found)
	}
}

func TestB479MinimalDocumentsAdmit(t *testing.T) {
	minimal := b479Minimal()
	type group struct{ services, bindings []api.Value }
	groups := []group{
		{services: []api.Value{minimal["prometheus"], minimal["alertmanager"], minimal["grafana"], minimal["node-exporter"], minimal["ceph-exporter"]}},
		{services: []api.Value{minimal["snmp-gateway"], minimal["alertmanager"]}, bindings: []api.Value{leaves("snmp-gateway", "/spec/credentials/snmp_community", "opaque-a")}},
		{services: []api.Value{authGateway(), minimal["oauth2-proxy"]}, bindings: []api.Value{leaves("oauth2-proxy", "/spec/client_secret", "opaque-a", "/spec/cookie_secret", "opaque-a")}},
	}
	for _, name := range []string{"prometheus", "alertmanager", "grafana", "node-exporter", "ceph-exporter", "mgmt-gateway"} {
		groups = append(groups, group{services: []api.Value{minimal[name]}})
	}
	for _, group := range groups {
		cluster, catalog := withGraph(t, b479BindingCatalog(), group.services, group.bindings...)
		if found := addedServiceIssues(Validate(cluster, catalog)); len(found) != 0 {
			t.Fatalf("admitted documents refused: %#v", found)
		}
		if found := addedServiceIssues(ValidatePartial(cluster, catalog)); len(found) != 0 {
			t.Fatalf("admitted partial documents refused: %#v", found)
		}
	}
}

func TestB479RefusalsNameTheirRemedy(t *testing.T) {
	catalog := fixtureCatalog()
	cluster := find(t, catalog, api.StorageCluster, "storage")
	modules := func(services ...api.Value) api.Object {
		changed, _ := withServices(t, catalog, services...)
		return changed.WithSpec(changed.Spec().WithPath(api.StringList("prometheus"), "ceph", "mgrModules"))
	}
	added := func(services ...api.Value) api.Object {
		changed, _ := withServices(t, catalog, services...)
		return changed
	}
	const s3 = "$.spec.ceph.services[3]"
	for _, row := range []struct {
		name, code, field, message, remedy string
		object                             api.Object
	}{
		{"R2 declared", "api.invariant", "$.spec.ceph.mgrModules[0]", "manager module prometheus is derived from the declared prometheus service", "remove prometheus from spec.ceph.mgrModules; cephadm enables it when it deploys the declared prometheus service", modules(b479Service("prometheus"))},
		{"R2 undeclared", "api.invariant", "$.spec.ceph.mgrModules[0]", "manager module prometheus is derived from a declared prometheus service", "remove prometheus from spec.ceph.mgrModules and declare a prometheus service in spec.ceph.services; cephadm enables the module when it deploys that service", modules()},
		{"R3", "api.invariant", s3 + ".service_type", "the SNMP gateway receives alerts only from Alertmanager", "declare an alertmanager service in spec.ceph.services or remove the snmp-gateway service", added(b479Minimal()["snmp-gateway"])},
		{"R8 bare", "api.invariant", s3 + ".spec.virtual_ip", "a management gateway virtual_ip is a bare address without a prefix length", "write spec.virtual_ip as an address without a prefix length, inside a declared public network", added(b479Service("mgmt-gateway", "port", 9443, "virtual_ip", "192.0.2.50/24"))},
		{"R8 public", "api.invariant", s3 + ".spec.virtual_ip", "a management gateway virtual_ip lies inside a declared public network, so a host interface carries it", "choose an address inside spec.ceph.networks.publicCIDRs", added(b479Service("mgmt-gateway", "port", 9443, "virtual_ip", "198.51.100.7"))},
		{"R10", "api.invariant", s3 + ".service_type", "oauth2-proxy authenticates only through a management gateway with enable_auth: true", "declare a mgmt-gateway service with spec.enable_auth: true, or remove the oauth2-proxy service", added(oauth2Proxy(), b479Service("mgmt-gateway", "port", 9443))},
		{"R11", "api.invariant", s3 + ".spec.enable_auth", "enable_auth needs an oauth2-proxy service as the management gateway's authentication backend", "declare an oauth2-proxy service or set spec.enable_auth: false", added(authGateway())},
		{"R12", "api.invariant", s3 + ".spec.ssl", "a plaintext management gateway would carry dashboard credentials unencrypted", "omit spec.ssl or set it to true", added(b479Service("mgmt-gateway", "port", 9443, "ssl", false))},
		{"R12a port", "api.invariant", s3 + ".spec.https_address", "cephadm reserves port 4180 for oauth2-proxy whatever https_address names", "use port 4180 in spec.https_address or omit it", added(oauth2Proxy("https_address", "0.0.0.0:4181"), authGateway())},
		{"R12a malformed", "api.invariant", s3 + ".spec.https_address", "https_address is host:port", "write spec.https_address as host:4180 or omit it", added(oauth2Proxy("https_address", "4180"), authGateway())},
		{"R14a alloy", "api.invariant", s3 + ".service_type", "the alloy log agent ships logs only to a Loki service of this cluster", "declare a loki service in spec.ceph.services or remove the alloy service", added(b479Service("alloy"))},
		{"R14a promtail", "api.invariant", s3 + ".service_type", "the promtail log agent ships logs only to a Loki service of this cluster", "declare a loki service in spec.ceph.services or remove the promtail service", added(b479Service("promtail"))},
		{"R14b", "api.invariant", "$.spec.ceph.services[5].service_type", "alloy and promtail would both ship the same Ceph logs to Loki", "keep one log agent: remove the alloy or the promtail service from spec.ceph.services", added(b479Service("loki"), b479Service("alloy"), b479Service("promtail"))},
		{"R15a", "api.invariant", s3 + ".service_type", "Bootwright applies the crash service on every host as a derived child of the StorageCluster, so a declared crash service would be its second owner", "remove the crash service from spec.ceph.services; the cluster keeps its derived crash service", added(b479Service("crash"))},
		{"R15b tracing", "api.invariant", s3 + ".service_type", "Bootwright does not realize tracing services", "remove this service from spec.ceph.services", added(b479Service("jaeger-agent"))},
		{"R15b node-proxy", "api.invariant", s3 + ".service_type", "Bootwright does not realize this native service type", "remove this service from spec.ceph.services", added(b479Service("node-proxy"))},
		{"alertmanager cluster port", "api.invariant", s3 + ".spec.port", "cephadm reserves port 9094 for the Alertmanager cluster listener", "choose another spec.port", added(b479Service("alertmanager", "port", 9094))},
		{"SNMP V3 engine", "api.invariant", s3 + ".spec.engine_id", "SNMP V3 notifications need an engine_id", "set spec.engine_id or use snmp_version V2c", added(b479Service("snmp-gateway", "snmp_version", "V3", "snmp_destination", "192.0.2.9:162"), b479Service("alertmanager"))},
		{"R17", "api.field", "$.spec.ceph.cephadm.workarounds", "the cephadm workaround list was retired; Bootwright realizes no cephadm workaround", "remove workarounds from $.spec.ceph.cephadm", cluster.WithSpec(cluster.Spec().WithPath(api.StringList("mgmt-gateway-spec-dependency-recording"), "ceph", "cephadm", "workarounds"))},
	} {
		t.Run(row.name, func(t *testing.T) {
			found := issuesAt(Validate(row.object, replace(catalog, row.object)), row.field)
			if len(found) != 1 || found[0] != (api.Issue{Code: row.code, Field: row.field, Message: row.message, Remediation: row.remedy}) {
				t.Fatalf("issues at %s: %#v", row.field, found)
			}
		})
	}
}

func TestB479InlineSecretLeavesRefuse(t *testing.T) {
	const secret = "never-echo-this"
	credential := "native documents never carry credentials inline"
	material := "native documents never carry TLS material inline"
	tlsRemedy := "bind /spec/ssl_cert and /spec/ssl_key to one tlsCertificate Secret in spec.ceph.serviceBindings"
	for _, row := range []struct {
		service         api.Value
		member          string
		message, remedy string
	}{
		{b479Service("grafana", "initial_admin_password", secret), "initial_admin_password", credential, "remove spec.initial_admin_password; bind /spec/initial_admin_password to a Secret in spec.ceph.serviceBindings"},
		{b479Service("snmp-gateway", "snmp_version", "V2c", "snmp_destination", "192.0.2.9:162", "credentials", m("snmp_community", secret)), "credentials", credential, "remove spec.credentials; bind each credential leaf under /spec/credentials to a Secret in spec.ceph.serviceBindings"},
		{b479Service("mgmt-gateway", "port", 9443, "ssl_cert", secret), "ssl_cert", material, "remove spec.ssl_cert; " + tlsRemedy},
		{b479Service("mgmt-gateway", "port", 9443, "ssl_key", secret), "ssl_key", material, "remove spec.ssl_key; " + tlsRemedy},
		{oauth2Proxy("client_secret", secret), "client_secret", credential, "remove spec.client_secret; bind /spec/client_secret to a Secret in spec.ceph.serviceBindings"},
		{oauth2Proxy("cookie_secret", secret), "cookie_secret", credential, "remove spec.cookie_secret; bind /spec/cookie_secret to a Secret in spec.ceph.serviceBindings"},
		{oauth2Proxy("ssl_cert", secret), "ssl_cert", material, "remove spec.ssl_cert; " + tlsRemedy},
		{oauth2Proxy("ssl_key", secret), "ssl_key", material, "remove spec.ssl_key; " + tlsRemedy},
	} {
		cluster, catalog := withServices(t, fixtureCatalog(), row.service)
		field := "$.spec.ceph.services[3].spec." + row.member
		for _, issues := range [][]api.Issue{Validate(cluster, catalog), ValidatePartial(cluster, catalog)} {
			found := issuesAt(issues, field)
			if len(found) != 1 || found[0].Message != row.message || found[0].Remediation != row.remedy || len(issuesAt(issues, "$.spec.ceph.services[3].spec")) != 0 {
				t.Fatalf("inline %s: %#v", field, issues)
			}
			if strings.Contains(fmt.Sprint(issues), secret) {
				t.Fatalf("a diagnostic echoed the authored value: %#v", issues)
			}
		}
	}
}

func TestAlertmanagerUserDataAdmitsOnlyDefaultWebhookURLs(t *testing.T) {
	const spec = "$.spec.ceph.services[3].spec"
	admitted, catalog := withServices(t, fixtureCatalog(), b479Service("alertmanager", "user_data", m("default_webhook_urls", api.StringList("https://hooks.example.test/a"))))
	if found := addedServiceIssues(Validate(admitted, catalog)); len(found) != 0 {
		t.Fatalf("default webhook URLs refused: %#v", found)
	}
	unread, catalog := withServices(t, fixtureCatalog(), b479Service("alertmanager", "user_data", m("webhook_urls", api.StringList("https://hooks.example.test/a"))))
	issues := Validate(unread, catalog)
	found := issuesAt(issues, spec+".user_data.webhook_urls")
	if len(found) != 1 || found[0].Message != "cephadm reads only default_webhook_urls from user_data, so this key would be stored but never applied" || found[0].Remediation != "move the URLs to spec.user_data.default_webhook_urls and remove this key" || len(issuesAt(issues, spec+".user_data")) != 0 || len(addedServiceIssues(issues)) != 1 {
		t.Fatalf("unread user_data key: %#v", issues)
	}
	credentialed, catalog := withServices(t, fixtureCatalog(), b479Service("alertmanager", "user_data", m("default_webhook_urls", api.StringList("https://user:pw@hooks.example.test/"))))
	issues = Validate(credentialed, catalog)
	found = issuesAt(issues, spec+".user_data.default_webhook_urls[0]")
	if len(found) != 1 || found[0].Message != "an Alertmanager webhook URL is an http or https URL without user information" || strings.Contains(fmt.Sprint(issues), "pw") {
		t.Fatalf("credentialed webhook URL: %#v", issues)
	}
}

func TestPrometheusModuleIsDerivedFromTheService(t *testing.T) {
	catalog := fixtureCatalog()
	declared, _ := withServices(t, catalog, b479Service("prometheus"))
	for _, test := range []struct {
		object  api.Object
		message string
	}{
		{declared, "manager module prometheus is derived from the declared prometheus service"},
		{find(t, catalog, api.StorageCluster, "storage"), "manager module prometheus is derived from a declared prometheus service"},
	} {
		changed := test.object.WithSpec(test.object.Spec().WithPath(api.StringList("prometheus"), "ceph", "mgrModules"))
		oneIssueAt(t, Validate(changed, replace(catalog, changed)), "$.spec.ceph.mgrModules[0]", test.message)
		if found := issuesAt(Validate(changed, replace(catalog, changed)), "$.spec.ceph.mgrModules"); len(found) != 0 {
			t.Fatalf("prometheus module refused as unowned: %#v", found)
		}
	}
	if found := Validate(declared, replace(catalog, declared)); len(found) != 0 {
		t.Fatalf("prometheus service without the module refused: %#v", found)
	}
	if !slices.Equal(DerivableManagerModules(), []string{"nfs"}) {
		t.Fatalf("derivable modules: %v", DerivableManagerModules())
	}
	if slices.Contains(DerivedManagerModules(declared.Spec().Get("ceph")), "prometheus") {
		t.Fatal("Bootwright enables a module cephadm enables itself")
	}
}

func TestDeclaredCrashServiceRefusesNamingTheDerivedUnit(t *testing.T) {
	cluster, catalog := withServices(t, fixtureCatalog(), b479Service("crash"))
	for _, issues := range [][]api.Issue{Validate(cluster, catalog), ValidatePartial(cluster, catalog)} {
		found := issuesAt(issues, "$.spec.ceph.services[3].service_type")
		if len(found) != 1 || found[0].Message != "Bootwright applies the crash service on every host as a derived child of the StorageCluster, so a declared crash service would be its second owner" || found[0].Remediation != "remove the crash service from spec.ceph.services; the cluster keeps its derived crash service" {
			t.Fatalf("authored crash: %#v", found)
		}
		if text := found[0].Message + found[0].Remediation; strings.Contains(text, "does not realize") || strings.Contains(text, "bootstrap creates") || strings.ContainsAny(text, "0123456789") {
			t.Fatalf("crash refusal reads as unrealized or names a release: %q", text)
		}
	}
}

func TestOAuth2ProxyPortIsFixed(t *testing.T) {
	const field = "$.spec.ceph.services[3].spec.https_address"
	for address, message := range map[string]string{
		"0.0.0.0:4180": "",
		"[::]:4180":    "",
		"0.0.0.0:4181": "cephadm reserves port 4180 for oauth2-proxy whatever https_address names",
		"4180":         "https_address is host:port",
	} {
		cluster, catalog := withServices(t, fixtureCatalog(), oauth2Proxy("https_address", address), authGateway())
		found := issuesAt(Validate(cluster, catalog), field)
		if message == "" && len(found) != 0 || message != "" && (len(found) != 1 || found[0].Message != message) {
			t.Fatalf("https_address %s: %#v", address, found)
		}
	}
}

func TestMgmtGatewayVirtualIPIsBareAndPublic(t *testing.T) {
	const field = "$.spec.ceph.services[3].spec.virtual_ip"
	const bare = "a management gateway virtual_ip is a bare address without a prefix length"
	gateway := func(address string) (api.Object, api.Catalog) {
		return withServices(t, fixtureCatalog(), b479Service("mgmt-gateway", "port", 9443, "virtual_ip", address))
	}
	cluster, catalog := withServices(t, fixtureCatalog(), gatewayPair("192.0.2.50", "192.0.2.50")...)
	if found := addedServiceIssues(Validate(cluster, catalog)); len(found) != 0 {
		t.Fatalf("public bare VIP refused: %#v", found)
	}
	cluster, catalog = gateway("192.0.2.50/24")
	oneIssueAt(t, Validate(cluster, catalog), field, bare)
	oneIssueAt(t, ValidatePartial(cluster, catalog), field, bare)
	cluster, catalog = gateway("198.51.100.7")
	oneIssueAt(t, Validate(cluster, catalog), field, "a management gateway virtual_ip lies inside a declared public network, so a host interface carries it")
	if found := issuesAt(ValidatePartial(cluster, catalog), field); len(found) != 0 {
		t.Fatalf("partial input resolved the public networks: %#v", found)
	}
	cluster, catalog = withServices(t, fixtureCatalog(), b479Service("mgmt-gateway", "port", 9443), b479Service("mgmt-gateway", "port", 9444))
	oneIssueAt(t, Validate(cluster, catalog), "$.spec.ceph.services[4].service_id", "qualified native service identities must be unique")
}

func TestB479ServiceIDIsForbidden(t *testing.T) {
	for _, service := range b479Minimal() {
		cluster, catalog := withServices(t, fixtureCatalog(), service.With("service_id", api.StringValue("a")))
		for _, issues := range [][]api.Issue{Validate(cluster, catalog), ValidatePartial(cluster, catalog)} {
			oneIssueAt(t, issues, "$.spec.ceph.services[3].service_id", "native singleton services omit service_id; other IDs use the qualified name grammar")
		}
	}
}

func TestServicesWithoutANativeNetworksFieldRefuseNetworks(t *testing.T) {
	minimal := b479Minimal()
	networked := func(service api.Value) api.Value { return service.With("networks", api.StringList("192.0.2.0/24")) }
	for _, row := range []struct {
		serviceType string
		services    []api.Value
	}{
		{"ceph-exporter", []api.Value{networked(minimal["ceph-exporter"])}},
		{"snmp-gateway", []api.Value{networked(minimal["snmp-gateway"]), minimal["alertmanager"]}},
	} {
		cluster, catalog := withServices(t, fixtureCatalog(), row.services...)
		want := api.Issue{Code: "api.invariant", Field: "$.spec.ceph.services[3].networks",
			Message:     "cephadm's " + row.serviceType + " service spec has no networks field, so cephadm would reject this document",
			Remediation: "remove networks from this " + row.serviceType + " service"}
		for _, issues := range [][]api.Issue{Validate(cluster, catalog), ValidatePartial(cluster, catalog)} {
			if found := issuesAt(issues, want.Field); len(found) != 1 || found[0] != want {
				t.Fatalf("%s networks: %#v", row.serviceType, found)
			}
		}
	}
	for _, serviceType := range []string{"prometheus", "alertmanager", "grafana", "node-exporter"} {
		cluster, catalog := withServices(t, fixtureCatalog(), networked(minimal[serviceType]))
		if found := issuesAt(Validate(cluster, catalog), "$.spec.ceph.services[3].networks"); len(found) != 0 {
			t.Fatalf("%s networks refused: %#v", serviceType, found)
		}
	}
}

func TestB479ValueRulesFollowTheNativeGrammar(t *testing.T) {
	alertmanager := b479Service("alertmanager")
	for _, row := range []struct {
		service  api.Value
		member   string
		admitted bool
	}{
		{b479Service("prometheus", "retention_time", "14d"), "retention_time", true},
		{b479Service("prometheus", "retention_time", "14 d"), "retention_time", false},
		{b479Service("prometheus", "retention_size", "50GB"), "retention_size", true},
		{b479Service("prometheus", "retention_size", "50G"), "retention_size", false},
		{b479Service("snmp-gateway", "snmp_version", "V2c", "snmp_destination", "traps.example.test:162"), "snmp_destination", true},
		{b479Service("snmp-gateway", "snmp_version", "V2c", "snmp_destination", "[2001:db8::1]:162"), "snmp_destination", false},
		{b479Service("snmp-gateway", "snmp_version", "V2c", "snmp_destination", "192.0.2.9:70000"), "snmp_destination", false},
		{b479Service("snmp-gateway", "snmp_version", "V2c", "snmp_destination", "192.0.2.9"), "snmp_destination", false},
		{b479Service("snmp-gateway", "snmp_version", "V3", "snmp_destination", "192.0.2.9:162", "engine_id", "8000C53F00"), "engine_id", true},
		{b479Service("snmp-gateway", "snmp_version", "V3", "snmp_destination", "192.0.2.9:162", "engine_id", "8000C53F001"), "engine_id", false},
		{b479Service("snmp-gateway", "snmp_version", "V3", "snmp_destination", "192.0.2.9:162", "engine_id", "8000C53FZZ"), "engine_id", false},
		{oauth2Proxy("redirect_url", "https://ceph.example.test/oauth2/callback"), "redirect_url", true},
		{oauth2Proxy("redirect_url", "http://ceph.example.test/oauth2/callback"), "redirect_url", false},
		{b479Service("oauth2-proxy", "provider_display_name", "IdP", "client_id", "ceph", "oidc_issuer_url", "https://user@idp.example.test/"), "oidc_issuer_url", false},
		{oauth2Proxy("allowlist_domains", api.StringList(".example.test", "*.example.test:*", "192.0.2.9:8443")), "allowlist_domains[0]", true},
		{oauth2Proxy("allowlist_domains", api.StringList("https://example.test")), "allowlist_domains[0]", false},
		{oauth2Proxy("allowlist_domains", api.StringList("example.test:0")), "allowlist_domains[0]", false},
		{b479Service("mgmt-gateway", "port", 9443, "ssl_ciphers", api.ListValue()), "ssl_ciphers", false},
		{b479Service("grafana", "only_bind_port_on_networks", true), "only_bind_port_on_networks", false},
	} {
		cluster, catalog := withServices(t, fixtureCatalog(), row.service, alertmanager, authGateway(), oauth2Proxy())
		field := "$.spec.ceph.services[3].spec." + row.member
		found := issuesAt(Validate(cluster, catalog), field)
		if row.admitted != (len(found) == 0) || len(found) > 1 {
			t.Fatalf("%s admitted=%v: %#v", field, row.admitted, found)
		}
	}
}

func TestLokiAlloyAndPromtailAdmittedAsNativeServiceTypes(t *testing.T) {
	for _, agent := range []string{"alloy", "promtail"} {
		cluster, catalog := withServices(t, fixtureCatalog(), b479Service("loki", "port", 3100), b479Service(agent))
		for _, issues := range [][]api.Issue{Validate(cluster, catalog), ValidatePartial(cluster, catalog)} {
			if found := addedServiceIssues(issues); len(found) != 0 {
				t.Fatalf("loki with %s refused: %#v", agent, found)
			}
		}
	}
	cluster, catalog := withServices(t, fixtureCatalog(), b479Service("loki", "retention_period", "7d"))
	if found := issuesAt(Validate(cluster, catalog), "$.spec.ceph.services[3].spec"); len(found) != 1 || found[0].Code != "api.field" {
		t.Fatalf("an unrealized loki member was admitted: %#v", Validate(cluster, catalog))
	}
}

func TestR14NoLongerRefusesLoki(t *testing.T) {
	for _, name := range []string{"loki", "alloy", "promtail"} {
		if refusal, refused := serviceTypeRefusal(name); refused {
			t.Fatalf("%s still refused by type: %#v", name, refusal)
		}
		cluster, catalog := withServices(t, fixtureCatalog(), b479Service(name))
		for _, issue := range Validate(cluster, catalog) {
			if strings.Contains(issue.Message, "Bootwright does not realize log collection yet") {
				t.Fatalf("%s refused as unrealized: %#v", name, issue)
			}
		}
	}
}
