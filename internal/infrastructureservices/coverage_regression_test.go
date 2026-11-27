package infrastructureservices

import (
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestIdentityServiceAdmissionRequiresExternalTLSAndExactIssuer(t *testing.T) {
	for _, tc := range []struct {
		name    string
		service api.Object
		fields  []string
	}{
		{"managed issuer", obj(api.OIDCProvider, "id", m("management", "managed", "issuerURL", "https://id.example.test/realms/lab/")), []string{"$.spec.management", "$.spec.issuerURL"}},
		{"exact issuer", obj(api.OIDCProvider, "id", m("management", "external", "issuerURL", "https://id.example.test/realms/lab")), nil},
		{"mixed LDAP transport", obj(api.LDAPServer, "directory", m("management", "external", "urls", list(api.StringValue("ldaps://directory.example.test"), api.StringValue("ldap://directory.example.test")))), []string{"$.spec.urls[1]"}},
		{"TLS LDAP", obj(api.LDAPServer, "directory", m("management", "external", "urls", list(api.StringValue("ldaps://directory.example.test")))), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var fields []string
			for _, found := range Validate(tc.service, api.Catalog{}) {
				fields = append(fields, found.Field)
			}
			slices.Sort(fields)
			want := slices.Clone(tc.fields)
			slices.Sort(want)
			if !slices.Equal(fields, want) {
				t.Fatalf("issue fields = %v, want %v", fields, want)
			}
		})
	}
}
