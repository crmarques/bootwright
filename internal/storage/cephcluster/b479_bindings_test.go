package cephcluster

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/secrets"
)

const (
	firstLeaf  = "$.spec.serviceBindings[0].secrets[0]"
	secondLeaf = "$.spec.serviceBindings[0].secrets[1]"
)

func b479BindingCatalog() api.Catalog {
	catalog := bindingCatalog()
	for name, kind := range map[string]string{"token-a": "token", "user-a": "usernamePassword", "user-b": "usernamePassword", "ssh-a": "sshKeyPair", "ca-a": "caBundle"} {
		catalog = replace(catalog, object(api.Secret, name, m("type", kind)))
	}
	return catalog
}

func b479Issues(t *testing.T, partial bool, services []api.Value, bindings ...api.Value) []api.Issue {
	t.Helper()
	cluster, catalog := withGraph(t, b479BindingCatalog(), services, bindings...)
	if partial {
		return ValidatePartial(cluster, catalog)
	}
	return Validate(cluster, catalog)
}

func requireB479Admits(t *testing.T, issues []api.Issue) {
	t.Helper()
	if found := append(addedServiceIssues(issues), issuesUnder(issues, "$.spec.serviceBindings")...); len(found) != 0 {
		t.Fatalf("bound services refused: %#v", found)
	}
}

func snmpV3(extra ...any) []api.Value {
	spec := append([]any{"snmp_version", "V3", "snmp_destination", "192.0.2.9:162", "engine_id", "8000C53F00"}, extra...)
	return []api.Value{b479Service("snmp-gateway", spec...), b479Service("alertmanager")}
}

func TestB479BindingTypesPerLeaf(t *testing.T) {
	grafana := []api.Value{b479Service("grafana")}
	issues := b479Issues(t, false, grafana, leaves("grafana", "/spec/initial_admin_password", "ssh-a"))
	requireOnly(t, issues, firstLeaf+".secretRef", "api.invariant", "this binding leaf accepts only opaque or token Secrets", "bind a opaque or token Secret at this path")
	for _, secret := range []string{"opaque-a", "token-a"} {
		requireB479Admits(t, b479Issues(t, false, grafana, leaves("grafana", "/spec/initial_admin_password", secret)))
	}
	const username, password = "/spec/credentials/snmp_v3_auth_username", "/spec/credentials/snmp_v3_auth_password"
	split := b479Issues(t, false, snmpV3(), leaves("snmp-gateway", username, "user-a", password, "user-b"))
	oneIssueAt(t, split, secondLeaf+".secretRef", "snmp_v3_auth_username and snmp_v3_auth_password must bind the username and password of one usernamePassword Secret")
	oneIssueAt(t, b479Issues(t, false, snmpV3(), leaves("snmp-gateway", username, "user-a")), firstLeaf+".path", "snmp_v3_auth_username and snmp_v3_auth_password are bound together")
	requireB479Admits(t, b479Issues(t, false, snmpV3(), leaves("snmp-gateway", username, "user-a", password, "user-a")))
	gateway := []api.Value{b479Service("mgmt-gateway", "port", 9443)}
	oneIssueAt(t, b479Issues(t, false, gateway, leaves("mgmt-gateway", "/spec/ssl_cert", "tls-a")), firstLeaf+".path", "ssl_cert and ssl_key are bound together")
	requireB479Admits(t, b479Issues(t, false, gateway, leaves("mgmt-gateway", "/spec/ssl_cert", "tls-a", "/spec/ssl_key", "tls-a")))
	requireSlotsMatchTheirSecrets(t)
}

func requireSlotsMatchTheirSecrets(t *testing.T) {
	t.Helper()
	ceph := api.Field{Shape: api.Schema(api.CephCluster)}
	bindings, _ := ceph.Shape.Field("serviceBindings")
	leafShape, _ := bindings.Shape.Element.Field("secrets")
	ref, _ := leafShape.Shape.Element.Field("secretRef")
	rows := map[string]BindingSlot{}
	for _, slot := range bindingSlots {
		rows[slot.ServiceType+slot.Backend+slot.Path] = slot
		for _, kind := range slot.SecretTypes {
			if !slices.Contains(ref.Shape.SecretTypes, kind) {
				t.Fatalf("slot %s %s admits Secret type %s the binding schema refuses", slot.ServiceType, slot.Path, kind)
			}
			if !slices.Contains((secrets.VersionDeclaration{Type: kind}).Parts(), slot.Part) {
				t.Fatalf("slot %s %s reads part %s that a %s Secret lacks", slot.ServiceType, slot.Path, slot.Part, kind)
			}
		}
	}
	for _, slot := range bindingSlots {
		if partner, ok := rows[slot.ServiceType+slot.Backend+slot.Pair]; slot.Pair != "" && (!ok || len(slot.SecretTypes) != 1 || !slices.Equal(slot.SecretTypes, partner.SecretTypes)) {
			t.Fatalf("pair %s %s does not share one Secret type with its partner", slot.ServiceType, slot.Path)
		}
	}
	for _, leaf := range []string{"snmp-gateway/spec/credentials/snmp_community", "snmp-gateway/spec/credentials/snmp_v3_auth_username", "snmp-gateway/spec/credentials/snmp_v3_auth_password", "snmp-gateway/spec/credentials/snmp_v3_priv_password", "oauth2-proxy/spec/client_secret", "oauth2-proxy/spec/cookie_secret", "oauth2-proxy/spec/ssl_cert", "oauth2-proxy/spec/ssl_key", "grafana/spec/initial_admin_password", "mgmt-gateway/spec/ssl_cert", "mgmt-gateway/spec/ssl_key", "oauth2-proxy/custom_configs/0/content"} {
		if _, ok := rows[leaf]; !ok {
			t.Fatalf("secret leaf %s has no binding row", leaf)
		}
	}
}

func TestB479BindingConditions(t *testing.T) {
	const s3 = "$.spec.services[3]"
	type row struct {
		name, field, message, remedy string
		services                     []api.Value
		bindings                     []api.Value
	}
	const username, password, priv = "/spec/credentials/snmp_v3_auth_username", "/spec/credentials/snmp_v3_auth_password", "/spec/credentials/snmp_v3_priv_password"
	v2c := []api.Value{b479Minimal()["snmp-gateway"], b479Service("alertmanager")}
	auth := leaves("snmp-gateway", username, "user-a", password, "user-a")
	for _, tt := range []row{
		{"grafana password", s3 + ".spec.anonymous_access", "Grafana without anonymous access needs the /spec/initial_admin_password binding", "add a spec.serviceBindings entry for this service that binds /spec/initial_admin_password to an opaque or token Secret", []api.Value{b479Service("grafana", "anonymous_access", false)}, nil},
		{"community", s3 + ".spec.snmp_version", "SNMP V2c notifications need the /spec/credentials/snmp_community binding", "add a spec.serviceBindings entry for this service that binds /spec/credentials/snmp_community to an opaque or token Secret", v2c, nil},
		{"V3 auth", s3 + ".spec.snmp_version", "SNMP V3 notifications need the /spec/credentials/snmp_v3_auth_username and /spec/credentials/snmp_v3_auth_password bindings of one usernamePassword Secret", "add a spec.serviceBindings entry for this service that binds /spec/credentials/snmp_v3_auth_username and /spec/credentials/snmp_v3_auth_password to one usernamePassword Secret", snmpV3(), nil},
		{"V3 on V2c", secondLeaf + ".path", "snmp_v3_auth_username is read only for snmp_version V3", "remove this binding; SNMP V2c binds /spec/credentials/snmp_community", v2c, []api.Value{leaves("snmp-gateway", "/spec/credentials/snmp_community", "opaque-a", username, "user-a", password, "user-a")}},
		{"community on V3", firstLeaf + ".path", "snmp_community is read only for snmp_version V2c", "remove this binding; SNMP V3 binds /spec/credentials/snmp_v3_auth_username and /spec/credentials/snmp_v3_auth_password", snmpV3(), []api.Value{leaves("snmp-gateway", "/spec/credentials/snmp_community", "opaque-a", username, "user-a", password, "user-a")}},
		{"privacy", s3 + ".spec.privacy_protocol", "an SNMP V3 privacy_protocol needs the /spec/credentials/snmp_v3_priv_password binding", "add a spec.serviceBindings entry for this service that binds /spec/credentials/snmp_v3_priv_password to an opaque or token Secret", snmpV3("privacy_protocol", "AES"), []api.Value{auth}},
		{"privacy unread", "$.spec.serviceBindings[0].secrets[2].path", "snmp_v3_priv_password is read only with snmp_version V3 and a privacy_protocol", "remove this binding or set spec.privacy_protocol on an SNMP V3 gateway", snmpV3(), []api.Value{leaves("snmp-gateway", username, "user-a", password, "user-a", priv, "opaque-a")}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			requireOnly(t, b479Issues(t, false, tt.services, tt.bindings...), tt.field, "api.invariant", tt.message, tt.remedy)
			if found := issuesAt(b479Issues(t, true, tt.services, tt.bindings...), tt.field); len(found) != 0 {
				t.Fatalf("partial input checked a whole-document binding rule: %#v", found)
			}
		})
	}
	requireB479Admits(t, b479Issues(t, false, []api.Value{b479Service("grafana", "anonymous_access", false)}, leaves("grafana", "/spec/initial_admin_password", "opaque-a")))
	requireB479Admits(t, b479Issues(t, false, []api.Value{b479Service("grafana")}, leaves("grafana", "/spec/initial_admin_password", "opaque-a")))
	requireB479Admits(t, b479Issues(t, false, snmpV3("privacy_protocol", "AES"), leaves("snmp-gateway", username, "user-a", password, "user-a", priv, "opaque-a")))
	proxy := []api.Value{oauth2Proxy(), authGateway()}
	found := issuesAt(b479Issues(t, false, proxy), s3)
	if len(found) != 2 || found[0].Message != "oauth2-proxy needs the /spec/client_secret binding" || found[1].Message != "oauth2-proxy needs the /spec/cookie_secret binding, so continuation never depends on a secret cephadm generated" {
		t.Fatalf("oauth2-proxy without its secrets: %#v", found)
	}
	if found := issuesAt(b479Issues(t, true, proxy), s3); len(found) != 0 {
		t.Fatalf("partial oauth2-proxy checked its secrets: %#v", found)
	}
}

func caMountedProxy(entries ...api.Value) api.Value {
	return oauth2Proxy().With("custom_configs", api.ListValue(entries...))
}

func proxyLeaves(extra ...string) api.Value {
	return leaves("oauth2-proxy", append([]string{"/spec/client_secret", "opaque-a", "/spec/cookie_secret", "opaque-a"}, extra...)...)
}

func TestPrivateCAIdPNeedsItsBoundCABundle(t *testing.T) {
	const s3 = "$.spec.services[3]"
	const content = "/custom_configs/0/content"
	mounted := []api.Value{caMountedProxy(m("mount_path", OAuth2ProxyCABundlePath)), authGateway()}
	requireOnly(t, b479Issues(t, false, mounted, proxyLeaves()), s3+".custom_configs", "api.invariant", "an oauth2-proxy custom_configs entry needs its identity provider's CA bundle bound at /custom_configs/0/content", "add a spec.serviceBindings entry for this service that binds /custom_configs/0/content to a caBundle Secret")
	if found := issuesAt(b479Issues(t, true, mounted, proxyLeaves()), s3+".custom_configs"); len(found) != 0 {
		t.Fatalf("partial input checked the CA bundle binding: %#v", found)
	}
	plain := []api.Value{oauth2Proxy(), authGateway()}
	requireOnly(t, b479Issues(t, false, plain, proxyLeaves(content, "ca-a")), "$.spec.serviceBindings[0].secrets[2].path", "api.invariant", "/custom_configs/0/content is read only with the oauth2-proxy custom_configs entry for the identity provider's CA bundle", "declare custom_configs with mount_path /etc/pki/tls/certs/oauth2-proxy-idp-ca.pem on this oauth2-proxy service, or remove this binding")
	requireOnly(t, b479Issues(t, false, mounted, proxyLeaves(content, "tls-a")), "$.spec.serviceBindings[0].secrets[2].secretRef", "api.invariant", "this binding leaf accepts only caBundle Secrets", "bind a caBundle Secret at this path")
	requireB479Admits(t, b479Issues(t, false, mounted, proxyLeaves(content, "ca-a")))
}

func TestCustomConfigsAdmittedOnlyForTheBoundCABundle(t *testing.T) {
	if OAuth2ProxyCABundlePath != "/etc/pki/tls/certs/oauth2-proxy-idp-ca.pem" {
		t.Fatalf("CA bundle mount path moved: %s", OAuth2ProxyCABundlePath)
	}
	const s3 = "$.spec.services[3]"
	const content = "/custom_configs/0/content"
	entry := m("mount_path", OAuth2ProxyCABundlePath)
	for _, partial := range []bool{false, true} {
		requireB479Admits(t, b479Issues(t, partial, []api.Value{caMountedProxy(entry), authGateway()}, proxyLeaves(content, "ca-a")))
	}
	onlyMessage := "oauth2-proxy takes custom_configs only for its identity provider's CA bundle at /etc/pki/tls/certs/oauth2-proxy-idp-ca.pem"
	onlyRemedy := "declare one custom_configs entry with mount_path /etc/pki/tls/certs/oauth2-proxy-idp-ca.pem and bind /custom_configs/0/content to a caBundle Secret in spec.serviceBindings, or omit custom_configs"
	for name, proxy := range map[string]api.Value{
		"two entries":  caMountedProxy(entry, entry),
		"another path": caMountedProxy(m("mount_path", OAuth2ProxyCABundlePath+".d/extra.pem")),
	} {
		t.Run(name, func(t *testing.T) {
			requireOnly(t, b479Issues(t, false, []api.Value{proxy, authGateway()}, proxyLeaves(content, "ca-a")), s3+".custom_configs", "api.invariant", onlyMessage, onlyRemedy)
		})
	}
	inline := caMountedProxy(m("mount_path", OAuth2ProxyCABundlePath, "content", "-----BEGIN CERTIFICATE-----"))
	issues := b479Issues(t, false, []api.Value{inline, authGateway()}, proxyLeaves(content, "ca-a"))
	requireOnly(t, issues, s3+".custom_configs[0].content", "api.invariant", "native documents never carry the identity provider's CA bundle inline", "remove content; bind /custom_configs/0/content to a caBundle Secret in spec.serviceBindings")
	if strings.Contains(fmt.Sprint(issues), "BEGIN CERTIFICATE") {
		t.Fatalf("a diagnostic echoed the inline bundle: %#v", issues)
	}
	grafana := b479Service("grafana").With("custom_configs", api.ListValue(m("mount_path", OAuth2ProxyCABundlePath, "content", "x")))
	requireOnly(t, b479Issues(t, false, []api.Value{grafana}), s3+".custom_configs", "api.invariant", "native effect-bearing options are not qualified by this core consumer", "omit "+s3+".custom_configs until its effect contract is qualified")
}
