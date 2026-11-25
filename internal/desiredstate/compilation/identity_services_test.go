package compilation_test

import (
	"context"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

const identitySecrets = `
---
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata: {name: directory-ca}
spec: {type: caBundle}
---
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata: {name: directory-bind}
spec: {type: usernamePassword}
---
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata: {name: not-a-bundle}
spec: {type: token}
`

func identityObject(kind, spec string) string {
	return "\n---\napiVersion: bootwright.io/v1alpha1\nkind: " + kind + "\nmetadata: {name: sso}\nspec:\n" + spec
}

func ldapSpec(replace map[string]string) string {
	lines := map[string]string{
		"management": "  management: external\n",
		"urls":       "  urls: [ldaps://dc-01.example.test:636, ldaps://dc-02.example.test]\n",
		"trust":      "  trustBundleRef: directory-ca\n",
		"bind":       "  bindCredentialsRef: directory-bind\n",
		"users":      "  users: {baseDN: \"OU=People,DC=example,DC=test\", nameAttribute: sAMAccountName, filter: \"(memberOf=CN=ceph,DC=example,DC=test)\"}\n",
		"ad":         "  activeDirectory: {realm: EXAMPLE.TEST}\n",
	}
	var spec strings.Builder
	for _, key := range []string{"management", "urls", "trust", "bind", "users", "ad"} {
		line, replaced := replace[key]
		if !replaced {
			line = lines[key]
		}
		spec.WriteString(line)
	}
	return spec.String()
}

func TestIdentityServicesAreAdmittedAndRetainedWhileUnconsumed(t *testing.T) {
	input := sources(environmentYAML + identitySecrets +
		identityObject("OIDCProvider", "  management: external\n  issuerURL: https://sso.example.test/realms/ceph\n  trustBundleRef: directory-ca\n") +
		strings.Replace(identityObject("LDAPServer", ldapSpec(nil)), "name: sso", "name: directory", 1))
	state, _, err := regressionCompiler().Compile(context.Background(), input)
	if err != nil {
		t.Fatalf("a well-formed identity declaration was refused: %v", err)
	}
	if _, found := state.Effective().Find(api.OIDCProvider, "sso"); !found {
		t.Error("the unconsumed OIDCProvider left the effective state")
	}
	server, found := state.Effective().Find(api.LDAPServer, "directory")
	if !found {
		t.Fatal("the unconsumed LDAPServer left the effective state")
	}
	if server.Spec().Get("urls").Len() != 2 || server.Spec().Get("urls").Items()[0].Text() != "ldaps://dc-01.example.test:636" {
		t.Errorf("urls lost their authored order: %v", server.Spec().Get("urls"))
	}
	if server.Spec().Get("activeDirectory", "realm").Text() != "EXAMPLE.TEST" {
		t.Error("the realm's case changed")
	}
}

func TestIdentityServiceRefusalsNameTheirFieldAndNeverTheValue(t *testing.T) {
	oidc := func(spec string) string { return identityObject("OIDCProvider", "  management: external\n"+spec) }
	secrets := environmentYAML + identitySecrets
	for _, test := range []struct{ name, document, code, field, remedy string }{
		{"managed provider", identityObject("OIDCProvider", "  management: managed\n  issuerURL: https://sso.example.test\n"), "api.invariant", "$.spec.management", "declare management: external; Bootwright does not run an identity provider"},
		{"managed directory", identityObject("LDAPServer", ldapSpec(map[string]string{"management": "  management: managed\n"})), "api.invariant", "$.spec.management", "declare management: external; Bootwright does not run an identity provider"},
		{"http issuer", oidc("  issuerURL: http://sso.example.test\n"), "api.value", "$.spec.issuerURL", ""},
		{"issuer with userinfo", oidc("  issuerURL: https://user:secret@sso.example.test\n"), "api.value", "$.spec.issuerURL", ""},
		{"issuer with a query", oidc("  issuerURL: https://sso.example.test/realms/ceph?a=b\n"), "api.value", "$.spec.issuerURL", "https://sso.example.test/realms/ceph"},
		{"issuer with a fragment", oidc("  issuerURL: https://sso.example.test/realms/ceph#top\n"), "api.value", "$.spec.issuerURL", "https://sso.example.test/realms/ceph"},
		{"issuer with a trailing slash", oidc("  issuerURL: https://sso.example.test/realms/ceph/\n"), "api.value", "$.spec.issuerURL", "https://sso.example.test/realms/ceph"},
		{"issuer over 2048 bytes", oidc("  issuerURL: https://sso.example.test/" + strings.Repeat("a", 2040) + "\n"), "api.value", "$.spec.issuerURL", "https://sso.example.test/realms/ceph"},
		{"provider trust of the wrong type", oidc("  issuerURL: https://sso.example.test\n  trustBundleRef: not-a-bundle\n"), "api.reference", "$.spec.trustBundleRef", ""},
		{"cleartext ldap", identityObject("LDAPServer", ldapSpec(map[string]string{"urls": "  urls: [ldaps://dc-01.example.test, ldap://dc-02.example.test]\n"})), "api.invariant", "$.spec.urls[1]", "use ldaps://"},
		{"ldap url with a path", identityObject("LDAPServer", ldapSpec(map[string]string{"urls": "  urls: [ldaps://dc-01.example.test/dc=example]\n"})), "api.value", "$.spec.urls[0]", ""},
		{"ldap url with userinfo", identityObject("LDAPServer", ldapSpec(map[string]string{"urls": "  urls: [ldaps://admin:secret@dc-01.example.test]\n"})), "api.value", "$.spec.urls[0]", ""},
		{"ldap url with a query", identityObject("LDAPServer", ldapSpec(map[string]string{"urls": "  urls: [\"ldaps://dc-01.example.test?x\"]\n"})), "api.value", "$.spec.urls[0]", ""},
		{"ldap url with a fragment", identityObject("LDAPServer", ldapSpec(map[string]string{"urls": "  urls: [\"ldaps://dc-01.example.test#x\"]\n"})), "api.value", "$.spec.urls[0]", ""},
		{"duplicate urls", identityObject("LDAPServer", ldapSpec(map[string]string{"urls": "  urls: [ldaps://dc-01.example.test, ldaps://dc-01.example.test]\n"})), "api.duplicate", "$.spec.urls[1]", ""},
		{"empty urls", identityObject("LDAPServer", ldapSpec(map[string]string{"urls": "  urls: []\n"})), "", "$.spec.urls", ""},
		{"missing urls", identityObject("LDAPServer", ldapSpec(map[string]string{"urls": ""})), "api.required", "$.spec.urls", ""},
		{"missing bind credentials", identityObject("LDAPServer", ldapSpec(map[string]string{"bind": ""})), "api.required", "$.spec.bindCredentialsRef", ""},
		{"bind credentials of the wrong type", identityObject("LDAPServer", ldapSpec(map[string]string{"bind": "  bindCredentialsRef: directory-ca\n"})), "api.reference", "$.spec.bindCredentialsRef", ""},
		{"empty base DN", identityObject("LDAPServer", ldapSpec(map[string]string{"users": "  users: {baseDN: \"\", nameAttribute: uid}\n"})), "", "$.spec.users.baseDN", ""},
		{"base DN over 1024 bytes", identityObject("LDAPServer", ldapSpec(map[string]string{"users": "  users: {baseDN: \"DC=" + strings.Repeat("a", 1030) + "\", nameAttribute: uid}\n"})), "api.value", "$.spec.users.baseDN", ""},
		{"name attribute with a space", identityObject("LDAPServer", ldapSpec(map[string]string{"users": "  users: {baseDN: \"DC=example\", nameAttribute: \"sam account\"}\n"})), "api.value", "$.spec.users.nameAttribute", ""},
		{"name attribute starting with a digit", identityObject("LDAPServer", ldapSpec(map[string]string{"users": "  users: {baseDN: \"DC=example\", nameAttribute: 1uid}\n"})), "api.value", "$.spec.users.nameAttribute", ""},
		{"unparenthesized filter", identityObject("LDAPServer", ldapSpec(map[string]string{"users": "  users: {baseDN: \"DC=example\", nameAttribute: uid, filter: \"memberOf=x\"}\n"})), "api.value", "$.spec.users.filter", ""},
		{"unbalanced filter", identityObject("LDAPServer", ldapSpec(map[string]string{"users": "  users: {baseDN: \"DC=example\", nameAttribute: uid, filter: \"(&(a=b)\"}\n"})), "api.value", "$.spec.users.filter", ""},
		{"filter closing before it opens", identityObject("LDAPServer", ldapSpec(map[string]string{"users": "  users: {baseDN: \"DC=example\", nameAttribute: uid, filter: \")(a=b)(\"}\n"})), "api.value", "$.spec.users.filter", ""},
		{"filter with the user placeholder", identityObject("LDAPServer", ldapSpec(map[string]string{"users": "  users: {baseDN: \"DC=example\", nameAttribute: uid, filter: \"(uid=@USERNAME@)\"}\n"})), "api.value", "$.spec.users.filter", ""},
		{"filter over 1024 bytes", identityObject("LDAPServer", ldapSpec(map[string]string{"users": "  users: {baseDN: \"DC=example\", nameAttribute: uid, filter: \"(a=" + strings.Repeat("b", 1030) + ")\"}\n"})), "api.value", "$.spec.users.filter", ""},
		{"realm that is not a domain", identityObject("LDAPServer", ldapSpec(map[string]string{"ad": "  activeDirectory: {realm: \"not a domain\"}\n"})), "api.value", "$.spec.activeDirectory.realm", ""},
		{"active directory without a realm", identityObject("LDAPServer", ldapSpec(map[string]string{"ad": "  activeDirectory: {}\n"})), "api.required", "$.spec.activeDirectory.realm", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			state, report, err := regressionCompiler().Compile(context.Background(), sources(secrets+test.document))
			sink := requireCompilationFailure(t, state, report, err)
			found := false
			for _, d := range sink {
				if d.Field != test.field || test.code != "" && d.Code != test.code {
					continue
				}
				found = true
				if test.remedy != "" && !strings.Contains(d.Remediation, test.remedy) {
					t.Errorf("remediation %q lacks %q", d.Remediation, test.remedy)
				}
			}
			if !found {
				t.Fatalf("no %s diagnostic at %s: %#v", test.code, test.field, sink)
			}
			for _, d := range sink {
				for _, leaked := range []string{"secret@", "admin:secret", "@USERNAME@", "memberOf", "OU=People"} {
					if strings.Contains(d.Message+d.Remediation, leaked) {
						t.Errorf("a diagnostic echoes %q: %s / %s", leaked, d.Message, d.Remediation)
					}
				}
			}
		})
	}
}
