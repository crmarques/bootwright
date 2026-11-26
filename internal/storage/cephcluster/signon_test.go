package cephcluster

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

const (
	signOnNativeAt = "$.spec.native"
	signOnSpecAt   = "$.spec.native.spec"
)

func signOnSecrets(pairs ...string) api.Value {
	entries := []api.Value{}
	for i := 0; i < len(pairs); i += 2 {
		entries = append(entries, m("path", pairs[i], "secretRef", pairs[i+1]))
	}
	return api.ListValue(entries...)
}

func requiredSignOnSecrets() api.Value {
	return signOnSecrets(SignOnClientSecret, "opaque-a", SignOnCookieSecret, "token-a")
}

func signOnSpec(extra ...any) api.Value {
	return m(append([]any{"provider_display_name", "IdP", "client_id", "ceph"}, extra...)...)
}

func signOnNative(spec api.Value, extra ...any) api.Value {
	return m(append([]any{"service_type", "oauth2-proxy", "placement", m("label", "mgr"), "spec", spec}, extra...)...)
}

func signOnObject(name string, native api.Value, secrets api.Value) api.Object {
	return object(api.CephSignOn, name, m("clusterRef", "storage", "oidcProviderRef", "idp", "secrets", secrets, "native", native))
}

func signOnBase(t *testing.T, services ...api.Value) api.Catalog {
	t.Helper()
	_, catalog := withServices(t, b479BindingCatalog(), services...)
	return replace(catalog, object(api.OIDCProvider, "idp", m("management", "external", "issuerURL", "https://idp.example.test/realms/ceph")))
}

func signOnCatalog(t *testing.T) api.Catalog {
	t.Helper()
	return signOnBase(t, authGateway())
}

func signOnIssues(catalog api.Catalog, sign api.Object) (full, partial []api.Issue) {
	catalog = replace(catalog, sign)
	return Validate(sign, catalog), ValidatePartial(sign, catalog)
}

func requireSignOnIssue(t *testing.T, issues []api.Issue, field, message, remedy string) {
	t.Helper()
	found := issuesAt(issues, field)
	if len(found) != 1 || found[0] != (api.Issue{Code: "api.invariant", Field: field, Message: message, Remediation: remedy}) {
		t.Fatalf("issues at %s: %#v", field, found)
	}
}

func TestMinimalSignOnAdmits(t *testing.T) {
	catalog := signOnCatalog(t)
	for name, sign := range map[string]api.Object{
		"required only": signOnObject("sign-on", signOnNative(signOnSpec()), requiredSignOnSecrets()),
		"every optional member": signOnObject("sign-on", signOnNative(signOnSpec("https_address", "0.0.0.0:4180", "redirect_url", "https://ceph.example.test/oauth2/callback", "allowlist_domains", api.StringList(".example.test", "*.example.test:*", "192.0.2.9:8443")),
			"networks", api.StringList("192.0.2.0/24")), signOnSecrets(SignOnClientSecret, "opaque-a", SignOnCookieSecret, "token-a", SignOnCertificate, "tls-a", SignOnKey, "tls-a")),
	} {
		full, partial := signOnIssues(catalog, sign)
		if len(full) != 0 || len(partial) != 0 {
			t.Fatalf("%s: full %#v partial %#v", name, full, partial)
		}
	}
}

func TestSignOnNeedsAManagementGatewayOnItsCluster(t *testing.T) {
	sign := signOnObject("sign-on", signOnNative(signOnSpec()), requiredSignOnSecrets())
	full, partial := signOnIssues(signOnBase(t), sign)
	requireSignOnIssue(t, full, "$.spec.clusterRef", "a CephSignOn signs users in to its cluster's management gateway, and CephCluster storage declares no mgmt-gateway service",
		"declare a mgmt-gateway service in spec.services of CephCluster storage, or remove this CephSignOn")
	if len(partial) != 0 {
		t.Fatalf("partial input resolved the cluster: %#v", partial)
	}
}

func TestACephClusterHasOneSignOn(t *testing.T) {
	catalog := signOnCatalog(t)
	first := signOnObject("a-sign-on", signOnNative(signOnSpec()), requiredSignOnSecrets())
	second := signOnObject("b-sign-on", signOnNative(signOnSpec()), requiredSignOnSecrets())
	catalog = replace(replace(catalog, first), second)
	if issues := Validate(first, catalog); len(issues) != 0 {
		t.Fatalf("first sign-on refused: %#v", issues)
	}
	requireSignOnIssue(t, Validate(second, catalog), "$.spec.clusterRef", "CephSignOn a-sign-on already signs users in to CephCluster storage, and a cluster has one sign-on",
		"remove this CephSignOn, or point it at another CephCluster")
	if issues := ValidatePartial(second, catalog); len(issues) != 0 {
		t.Fatalf("partial input checked the duplicate: %#v", issues)
	}
	if found := ClusterSignOns(catalog, "storage"); len(found) != 2 || found[0].Name() != "a-sign-on" || found[1].Name() != "b-sign-on" {
		t.Fatalf("cluster sign-ons: %#v", found)
	}
	if found := ClusterSignOns(catalog, "absent"); len(found) != 0 {
		t.Fatalf("sign-ons of another cluster: %#v", found)
	}
}

func TestSignOnRefusedNativeMembersNameTheirRemedy(t *testing.T) {
	const providerRemedy = "set spec.trustBundleRef on the OIDCProvider that spec.oidcProviderRef names"
	catalog := signOnCatalog(t)
	for _, row := range []struct {
		name, field, message, remedy string
		native                       api.Value
	}{
		{"oidc_issuer_url", signOnSpecAt + ".oidc_issuer_url", "the issuer URL comes from the OIDCProvider's issuerURL",
			"remove spec.native.spec.oidc_issuer_url; spec.oidcProviderRef names the OIDCProvider whose spec.issuerURL oauth2-proxy uses", signOnNative(signOnSpec("oidc_issuer_url", "https://idp.example.test/"))},
		{"custom_configs", signOnNativeAt + ".custom_configs", "the identity provider's CA bundle comes from the OIDCProvider's trustBundleRef",
			"remove spec.native.custom_configs; " + providerRemedy, signOnNative(signOnSpec(), "custom_configs", api.ListValue(m("mount_path", OAuth2ProxyCABundlePath)))},
		{"ssl_insecure_skip_verify true", signOnSpecAt + ".ssl_insecure_skip_verify", "oauth2-proxy must verify the identity provider's certificate", "omit spec.native.spec.ssl_insecure_skip_verify", signOnNative(signOnSpec("ssl_insecure_skip_verify", true))},
		{"ssl_insecure_skip_verify false", signOnSpecAt + ".ssl_insecure_skip_verify", "oauth2-proxy must verify the identity provider's certificate", "omit spec.native.spec.ssl_insecure_skip_verify", signOnNative(signOnSpec("ssl_insecure_skip_verify", false))},
		{"scope", signOnSpecAt + ".scope", "no oauth2-proxy spec Bootwright realizes has this member", "omit spec.native.spec.scope", signOnNative(signOnSpec("scope", "openid"))},
		{"email_domains", signOnSpecAt + ".email_domains", "no oauth2-proxy spec Bootwright realizes has this member", "omit spec.native.spec.email_domains", signOnNative(signOnSpec("email_domains", api.StringList("example.test")))},
		{"ssl", signOnSpecAt + ".ssl", tlsNotQualifiedMessage, "omit spec.native.spec.ssl", signOnNative(signOnSpec("ssl", true))},
		{"custom_sans", signOnSpecAt + ".custom_sans", "cephadm-signed certificates are not supported by Bootwright yet", "omit spec.native.spec.custom_sans", signOnNative(signOnSpec("custom_sans", api.StringList("ceph.example.test")))},
		{"service_id", signOnNativeAt + ".service_id", "oauth2-proxy is a singleton service named by its type", "remove spec.native.service_id", signOnNative(signOnSpec(), "service_id", "a")},
		{"service_name", signOnNativeAt + ".service_name", "oauth2-proxy is a singleton service named by its type", "remove spec.native.service_name", signOnNative(signOnSpec(), "service_name", "oauth2-proxy.a")},
		{"unmanaged", signOnNativeAt + ".unmanaged", "owned services must remain managed and executable", "remove spec.native.unmanaged", signOnNative(signOnSpec(), "unmanaged", true)},
		{"extra_container_args", signOnNativeAt + ".extra_container_args", "native effect-bearing options are not qualified by this core consumer", "remove spec.native.extra_container_args", signOnNative(signOnSpec(), "extra_container_args", api.StringList("--privileged"))},
		{"count_per_host", signOnNativeAt + ".placement.count_per_host", "oauth2-proxy places at most one daemon per host", "omit placement.count_per_host", signOnNative(signOnSpec()).With("placement", m("label", "mgr", "count_per_host", 1))},
	} {
		t.Run(row.name, func(t *testing.T) {
			full, partial := signOnIssues(catalog, signOnObject("sign-on", row.native, requiredSignOnSecrets()))
			for _, issues := range [][]api.Issue{full, partial} {
				requireSignOnIssue(t, issues, row.field, row.message, row.remedy)
				if len(issues) != 1 {
					t.Fatalf("refused member left other issues: %#v", issues)
				}
			}
		})
	}
}

func TestSignOnRefusesInlineCredentialsWithoutEchoingThem(t *testing.T) {
	const secret = "never-echo-this"
	credential := "native documents never carry credentials inline"
	material := "native documents never carry TLS material inline"
	catalog := signOnCatalog(t)
	for _, row := range []struct{ member, message, remedy string }{
		{"client_secret", credential, "remove spec.native.spec.client_secret; bind /spec/client_secret to a Secret in spec.secrets"},
		{"cookie_secret", credential, "remove spec.native.spec.cookie_secret; bind /spec/cookie_secret to a Secret in spec.secrets"},
		{"ssl_cert", material, "remove spec.native.spec.ssl_cert; bind /spec/ssl_cert and /spec/ssl_key to one tlsCertificate Secret in spec.secrets"},
		{"ssl_key", material, "remove spec.native.spec.ssl_key; bind /spec/ssl_cert and /spec/ssl_key to one tlsCertificate Secret in spec.secrets"},
	} {
		full, partial := signOnIssues(catalog, signOnObject("sign-on", signOnNative(signOnSpec(row.member, secret)), requiredSignOnSecrets()))
		for _, issues := range [][]api.Issue{full, partial} {
			requireSignOnIssue(t, issues, signOnSpecAt+"."+row.member, row.message, row.remedy)
			if strings.Contains(fmt.Sprint(issues), secret) {
				t.Fatalf("a diagnostic echoed the authored value: %#v", issues)
			}
		}
	}
}

func TestSignOnSecretsAreCompleteAndTyped(t *testing.T) {
	catalog := signOnCatalog(t)
	native := signOnNative(signOnSpec())
	for _, row := range []struct {
		name    string
		secrets api.Value
		field   string
		message string
		remedy  string
	}{
		{"client_secret missing", signOnSecrets(SignOnCookieSecret, "token-a"), "$.spec.secrets", "a CephSignOn needs the /spec/client_secret binding, so continuation never depends on a secret cephadm generated",
			"add a spec.secrets entry with path /spec/client_secret and an opaque or token Secret"},
		{"cookie_secret missing", signOnSecrets(SignOnClientSecret, "opaque-a"), "$.spec.secrets", "a CephSignOn needs the /spec/cookie_secret binding, so continuation never depends on a secret cephadm generated",
			"add a spec.secrets entry with path /spec/cookie_secret and an opaque or token Secret"},
		{"lone ssl_cert", signOnSecrets(SignOnClientSecret, "opaque-a", SignOnCookieSecret, "token-a", SignOnCertificate, "tls-a"), "$.spec.secrets", "ssl_cert and ssl_key are bound together",
			"bind both /spec/ssl_cert and /spec/ssl_key to one tlsCertificate Secret"},
		{"lone ssl_key", signOnSecrets(SignOnClientSecret, "opaque-a", SignOnCookieSecret, "token-a", SignOnKey, "tls-a"), "$.spec.secrets", "ssl_cert and ssl_key are bound together",
			"bind both /spec/ssl_cert and /spec/ssl_key to one tlsCertificate Secret"},
		{"split pair", signOnSecrets(SignOnClientSecret, "opaque-a", SignOnCookieSecret, "token-a", SignOnCertificate, "tls-a", SignOnKey, "tls-b"), "$.spec.secrets", "ssl_cert and ssl_key must bind the certificate and private key of one tlsCertificate Secret",
			"bind /spec/ssl_cert and /spec/ssl_key to the same tlsCertificate Secret"},
		{"client_secret type", signOnSecrets(SignOnClientSecret, "tls-a", SignOnCookieSecret, "token-a"), "$.spec.secrets[0].secretRef", "this binding leaf accepts only opaque or token Secrets",
			"bind a opaque or token Secret at this path"},
		{"cookie_secret type", signOnSecrets(SignOnClientSecret, "opaque-a", SignOnCookieSecret, "ssh-a"), "$.spec.secrets[1].secretRef", "this binding leaf accepts only opaque or token Secrets",
			"bind a opaque or token Secret at this path"},
		{"unknown path", signOnSecrets(SignOnClientSecret, "opaque-a", SignOnCookieSecret, "token-a", "/spec/initial_admin_password", "opaque-a"), "$.spec.secrets[2].path", "a CephSignOn binds only /spec/client_secret, /spec/cookie_secret, /spec/ssl_cert and /spec/ssl_key",
			"remove this entry or bind one of those paths"},
	} {
		t.Run(row.name, func(t *testing.T) {
			full, partial := signOnIssues(catalog, signOnObject("sign-on", native, row.secrets))
			requireSignOnIssue(t, full, row.field, row.message, row.remedy)
			if len(full) != 1 {
				t.Fatalf("secret rule left other issues: %#v", full)
			}
			if row.name == "unknown path" {
				requireSignOnIssue(t, partial, row.field, row.message, row.remedy)
			} else if len(partial) != 0 {
				t.Fatalf("partial input checked a whole-document secret rule: %#v", partial)
			}
		})
	}
}

func TestSignOnCertificateAndKeyNeedATLSCertificateSecret(t *testing.T) {
	catalog := signOnCatalog(t)
	sign := signOnObject("sign-on", signOnNative(signOnSpec()), signOnSecrets(SignOnClientSecret, "opaque-a", SignOnCookieSecret, "token-a", SignOnCertificate, "opaque-a", SignOnKey, "opaque-a"))
	full, partial := signOnIssues(catalog, sign)
	for _, field := range []string{"$.spec.secrets[2].secretRef", "$.spec.secrets[3].secretRef"} {
		requireSignOnIssue(t, full, field, "this binding leaf accepts only tlsCertificate Secrets", "bind a tlsCertificate Secret at this path")
	}
	if len(full) != 2 || len(partial) != 0 {
		t.Fatalf("certificate type: full %#v partial %#v", full, partial)
	}
}

func TestSignOnPlacementAndValueRulesFollowTheNativeGrammar(t *testing.T) {
	catalog := signOnCatalog(t)
	for _, row := range []struct {
		name     string
		spec     api.Value
		field    string
		message  string
		remedy   string
		admitted bool
	}{
		{"https_address wildcard", signOnSpec("https_address", "0.0.0.0:4180"), "https_address", "", "", true},
		{"https_address v6", signOnSpec("https_address", "[::]:4180"), "https_address", "", "", true},
		{"https_address port", signOnSpec("https_address", "0.0.0.0:4181"), "https_address", "cephadm reserves port 4180 for oauth2-proxy whatever https_address names", "use port 4180 in spec.https_address or omit it", false},
		{"https_address malformed", signOnSpec("https_address", "4180"), "https_address", "https_address is host:port", "write spec.https_address as host:4180 or omit it", false},
		{"redirect_url https", signOnSpec("redirect_url", "https://ceph.example.test/oauth2/callback"), "redirect_url", "", "", true},
		{"redirect_url http", signOnSpec("redirect_url", "http://ceph.example.test/oauth2/callback"), "redirect_url", "redirect_url is an https URL without user information", "write spec.redirect_url as https://host[:port]/path", false},
		{"allowlist admitted", signOnSpec("allowlist_domains", api.StringList(".example.test", "*.example.test:*", "192.0.2.9:8443")), "allowlist_domains[0]", "", "", true},
		{"allowlist scheme", signOnSpec("allowlist_domains", api.StringList("https://example.test")), "allowlist_domains[0]", "allowlist_domains entries are [.|*.]host[:port|:*]", "write the entry as a domain or IPv4 address with an optional leading . or *. and an optional :port or :*", false},
		{"allowlist port", signOnSpec("allowlist_domains", api.StringList("example.test:0")), "allowlist_domains[0]", "allowlist_domains entries are [.|*.]host[:port|:*]", "write the entry as a domain or IPv4 address with an optional leading . or *. and an optional :port or :*", false},
	} {
		t.Run(row.name, func(t *testing.T) {
			sign := signOnObject("sign-on", signOnNative(row.spec), requiredSignOnSecrets())
			field := signOnSpecAt + "." + row.field
			full, partial := signOnIssues(catalog, sign)
			for _, issues := range [][]api.Issue{full, partial} {
				if row.admitted {
					if len(issues) != 0 {
						t.Fatalf("admitted member refused: %#v", issues)
					}
					continue
				}
				requireSignOnIssue(t, issues, field, row.message, row.remedy)
			}
		})
	}
	for i, native := range []api.Value{
		signOnNative(signOnSpec()).Without("placement"),
		signOnNative(signOnSpec()).Without("spec"),
		signOnNative(m("client_id", "ceph")),
		signOnNative(signOnSpec(), "service_type", "grafana"),
		signOnNative(signOnSpec("unknown_member", "x")),
	} {
		full, partial := signOnIssues(catalog, signOnObject("sign-on", native, requiredSignOnSecrets()))
		if len(full) == 0 && len(partial) == 0 {
			t.Fatalf("invalid native document %d was admitted", i)
		}
	}
	full, partial := signOnIssues(catalog, signOnObject("sign-on", signOnNative(signOnSpec("unknown_member", "x")), requiredSignOnSecrets()))
	for _, issues := range [][]api.Issue{full, partial} {
		if found := issuesAt(issues, signOnSpecAt); len(found) != 1 || found[0].Code != "api.field" {
			t.Fatalf("unknown spec member: %#v", issues)
		}
	}
}

func TestSignOnAccessValidatesGroupAndClaimValues(t *testing.T) {
	catalog := signOnCatalog(t)
	base := signOnObject("sign-on", signOnNative(signOnSpec()), requiredSignOnSecrets())
	for _, tc := range []struct {
		access api.Value
		field  string
	}{
		{m("groupsClaim", "ceph_roles", "allowedGroups", api.StringList("/storage/ceph", "Ceph Operators")), ""},
		{m("allowedGroups", api.StringList("ops,all")), "$.spec.access.allowedGroups[0]"},
		{m("allowedGroups", api.StringList(" ops")), "$.spec.access.allowedGroups[0]"},
		{m("allowedGroups", api.StringList("/ops\n")), "$.spec.access.allowedGroups[0]"},
		{m("groupsClaim", "ceph roles", "allowedGroups", api.StringList("ops")), "$.spec.access.groupsClaim"},
	} {
		sign := base.WithSpec(base.Spec().With("access", tc.access))
		full, partial := signOnIssues(catalog, sign)
		for _, issues := range [][]api.Issue{full, partial} {
			if tc.field == "" && len(issues) != 0 || tc.field != "" && len(issuesAt(issues, tc.field)) != 1 {
				t.Fatalf("policy field %q: %#v", tc.field, issues)
			}
		}
	}
}

func TestSignOnPortClashesWithADeclaredListener(t *testing.T) {
	const clash = "the oauth2-proxy port 4180 is already bound on host node-1 by "
	const remedy = "place oauth2-proxy on other hosts, or move the other listener to another port"
	sign := signOnObject("sign-on", signOnNative(signOnSpec()), requiredSignOnSecrets())
	catalog := signOnBase(t, authGateway(), b479Service("grafana", "port", 4180))
	full, partial := signOnIssues(catalog, sign)
	requireSignOnIssue(t, full, "$.spec.native.placement", clash+"CephCluster storage $.spec.services[4].spec.port", remedy)
	if len(partial) != 0 {
		t.Fatalf("partial input resolved the listeners: %#v", partial)
	}
	apart := b479Service("grafana", "port", 4180).With("placement", m("hosts", api.StringList("node-1.storage.example.test")))
	sign = signOnObject("sign-on", signOnNative(signOnSpec()).With("placement", m("hosts", api.StringList("node-2.storage.example.test"))), requiredSignOnSecrets())
	full, _ = signOnIssues(signOnBase(t, authGateway(), apart), sign)
	if len(full) != 0 {
		t.Fatalf("disjoint hosts refused: %#v", full)
	}
	full, _ = signOnIssues(signOnBase(t, authGateway(), b479Service("grafana", "port", 3000)), signOnObject("sign-on", signOnNative(signOnSpec()), requiredSignOnSecrets()))
	if len(full) != 0 {
		t.Fatalf("another port refused: %#v", full)
	}
}

func TestOAuth2ProxyServiceRefusesInFavourOfASignOn(t *testing.T) {
	const message = "oauth2-proxy is realized by a CephSignOn, which signs users in to the management gateway through an OIDCProvider"
	const remedy = "remove this oauth2-proxy service and its spec.serviceBindings entries from spec.services; declare a CephSignOn"
	proxy := b479Service("oauth2-proxy", "provider_display_name", "IdP", "client_id", "ceph")
	bindings := leaves("oauth2-proxy", SignOnClientSecret, "opaque-a", SignOnCookieSecret, "opaque-a")
	for _, partial := range []bool{false, true} {
		issues := b479Issues(t, partial, []api.Value{proxy, authGateway()}, bindings)
		requireSignOnIssue(t, issues, "$.spec.services[3].service_type", message, remedy)
	}
	issues := b479Issues(t, false, []api.Value{proxy, authGateway()}, bindings)
	for _, field := range []string{firstLeaf + ".path", secondLeaf + ".path"} {
		requireSignOnIssue(t, issues, field, "secret leaf is not qualified for this service schema", "use the existing cluster SSH or entitlement Secret reference; service-specific bindings require their owning slice")
	}
	if found := issuesAt(issues, "$.spec.services[4]"); len(found) != 0 {
		t.Fatalf("management gateway refused beside the proxy: %#v", found)
	}
}

func TestManagementGatewayEnableAuthDoesNotNeedASignOn(t *testing.T) {
	for _, enable := range []bool{true, false} {
		cluster, catalog := withServices(t, b479BindingCatalog(), b479Service("mgmt-gateway", "port", 9443, "enable_auth", enable))
		for _, issues := range [][]api.Issue{Validate(cluster, catalog), ValidatePartial(cluster, catalog)} {
			if found := addedServiceIssues(issues); len(found) != 0 {
				t.Fatalf("enable_auth %v without a sign-on refused: %#v", enable, found)
			}
		}
	}
	catalog := signOnBase(t, b479Service("mgmt-gateway", "port", 9443, "enable_auth", false))
	sign := signOnObject("sign-on", signOnNative(signOnSpec()), requiredSignOnSecrets())
	full, partial := signOnIssues(catalog, sign)
	if len(full) != 0 || len(partial) != 0 {
		t.Fatalf("sign-on beside enable_auth false refused: %#v %#v", full, partial)
	}
	cluster := find(t, replace(catalog, sign), api.CephCluster, "storage")
	if found := addedServiceIssues(Validate(cluster, replace(catalog, sign))); len(found) != 0 {
		t.Fatalf("a declared sign-on refused its cluster's gateway: %#v", found)
	}
}

func TestPartialSignOnChecksOnlyWhatItAuthors(t *testing.T) {
	catalog := signOnCatalog(t)
	fragment := object(api.CephSignOn, "sign-on", m("native", m("spec", m("oidc_issuer_url", "https://idp.example.test/", "https_address", "0.0.0.0:4181"), "custom_configs", api.ListValue(m("mount_path", "/x")), "placement", m("count_per_host", 2))))
	issues := ValidatePartial(fragment, catalog)
	fields := []string{}
	for _, issue := range issues {
		fields = append(fields, issue.Field)
	}
	slices.Sort(fields)
	want := []string{signOnNativeAt + ".custom_configs", signOnNativeAt + ".placement.count_per_host", signOnSpecAt + ".https_address", signOnSpecAt + ".oidc_issuer_url"}
	if !slices.Equal(fields, want) {
		t.Fatalf("partial sign-on issues %v, want %v: %#v", fields, want, issues)
	}
	for _, issues := range [][]api.Issue{ValidatePartial(object(api.CephSignOn, "empty", m()), catalog), ValidatePartial(object(api.CephSignOn, "secrets-only", m("secrets", signOnSecrets(SignOnCertificate, "tls-a"))), catalog)} {
		if len(issues) != 0 {
			t.Fatalf("partial sign-on demanded absent members: %#v", issues)
		}
	}
	unknown := ValidatePartial(object(api.CephSignOn, "unknown", m("secrets", signOnSecrets("/spec/nope", "opaque-a"))), catalog)
	requireSignOnIssue(t, unknown, "$.spec.secrets[0].path", "a CephSignOn binds only /spec/client_secret, /spec/cookie_secret, /spec/ssl_cert and /spec/ssl_key", "remove this entry or bind one of those paths")
}
