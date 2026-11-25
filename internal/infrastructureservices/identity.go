package infrastructureservices

import (
	"fmt"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func IsIdentityService(kind api.Kind) bool {
	return slices.Contains([]api.Kind{api.OIDCProvider, api.LDAPServer}, kind)
}

func validateIdentityService(o api.Object, partial bool) []api.Issue {
	value := o.Spec()
	issues := []api.Issue{}
	if !partial && value.Get("management").Text() == "managed" {
		issues = append(issues, issue("$.spec.management", "no capability runs an identity service, so "+o.Identity()+" is always external",
			"declare management: external; Bootwright does not run an identity provider"))
	}
	switch o.Kind() {
	case api.OIDCProvider:
		issuer := value.Get("issuerURL")
		if issuer.Type() == api.String && api.ValidLexical(api.LexicalHTTPSURL, issuer.Text()) && !api.ValidOIDCIssuer(issuer.Text()) {
			issues = append(issues, api.Issue{
				Code: "api.value", Field: "$.spec.issuerURL",
				Message:     "an OIDC issuer URL is an https URL of at most 2048 bytes with no query, fragment or trailing slash, because it is compared with the token's iss claim exactly",
				Remediation: "set spec.issuerURL to " + api.OIDCIssuerForm,
			})
		}
	case api.LDAPServer:
		for index, entry := range value.Get("urls").Items() {
			if entry.Type() == api.String && api.ValidLexical(api.LexicalLDAPURL, entry.Text()) && strings.HasPrefix(entry.Text(), "ldap://") {
				issues = append(issues, issue(fmt.Sprintf("$.spec.urls[%d]", index),
					"an LDAP server is reached over TLS only, because a bind password never travels in cleartext and no StartTLS option exists",
					"use ldaps://"))
			}
		}
	}
	return issues
}
