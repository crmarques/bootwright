package infrastructureservices

import (
	"fmt"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// crowdedHost is a Machine with n named addresses.
func crowdedHost(n int) api.Object {
	addresses := make([]api.Value, n)
	for i := range addresses {
		addresses[i] = m("name", fmt.Sprintf("a%02d", i), "address", fmt.Sprintf("198.51.100.%d/24", i+1))
	}
	return obj(api.Machine, "host", m("capabilities", api.StringList("container-runtime"), "network", m("addresses", list(addresses...))))
}

func validated(t *testing.T, service api.Object, catalog api.Catalog) []api.Issue {
	t.Helper()
	normalized, _ := Normalize(service, catalog)
	return Validate(normalized, catalog)
}

func TestAWildcardServiceDeclaresAtMostTheEndpointAddressesItsReservationHolds(t *testing.T) {
	implementations := map[api.Kind]string{api.Proxy: "squid", api.DNSServer: "dnsmasq", api.NTPServer: "chrony"}
	for kind, implementation := range implementations {
		endpoints := func(n, distinct int) api.Value {
			items := make([]api.Value, n)
			for i := range items {
				items[i] = m("name", fmt.Sprintf("e%02d", i), "addressRef", fmt.Sprintf("a%02d", i%distinct))
			}
			return list(items...)
		}
		service := func(n, distinct int) api.Object {
			return obj(kind, "service", m("management", "managed", "machineRef", "host", "implementation", implementation, "endpoints", endpoints(n, distinct)))
		}
		catalog := api.NewCatalog([]api.Object{crowdedHost(70)})
		t.Run(string(kind), func(t *testing.T) {
			if issues := validated(t, service(61, 61), catalog); len(issues) != 0 {
				t.Fatalf("61 distinct endpoint addresses were refused: %+v", issues)
			}
			issues := validated(t, service(62, 62), catalog)
			if len(issues) != 1 {
				t.Fatalf("62 distinct endpoint addresses gave %+v, want one issue", issues)
			}
			issue := issues[0]
			if issue.Field != "$.spec.endpoints" || issue.Code != "api.value" ||
				!strings.Contains(issue.Message, "at most 61 distinct endpoint addresses") || !strings.Contains(issue.Message, "this one declares 62") ||
				issue.Remediation != fmt.Sprintf("declare at most 61 distinct endpoint addresses in spec.endpoints of %s/service", kind) {
				t.Fatalf("the refusal is %+v", issue)
			}
			if issues := validated(t, service(70, 61), catalog); len(issues) != 0 {
				t.Fatalf("70 endpoints over 61 distinct addresses were refused: %+v", issues)
			}
		})
	}
}

func TestAnArtifactServerClaimsAtMostTheSocketsItsReservationHolds(t *testing.T) {
	catalog := api.NewCatalog([]api.Object{crowdedHost(70)})
	listeners := func(n int) api.Value {
		items := make([]api.Value, n)
		for i := range items {
			items[i] = m("name", fmt.Sprintf("l%02d", i), "protocol", "http", "port", api.IntegerValue(fmt.Sprint(8000+i)))
		}
		return list(items...)
	}
	endpointsOf := func(listener string, n int) []api.Value {
		items := make([]api.Value, n)
		for i := range items {
			items[i] = m("name", fmt.Sprintf("e%s-%02d", listener, i), "listenerRef", listener, "addressRef", fmt.Sprintf("a%02d", i))
		}
		return items
	}
	server := func(bind string, n int, endpoints ...api.Value) api.Object {
		spec := m("management", "managed", "machineRef", "host", "bindAddress", bind, "listeners", listeners(n))
		if len(endpoints) != 0 {
			spec = spec.With("endpoints", list(endpoints...))
		}
		return obj(api.ArtifactServer, "service", spec)
	}
	cases := []struct {
		name   string
		server api.Object
		field  string
		claims int
	}{
		{"62 listeners on one address", server("198.51.100.1", 62), "", 0},
		{"63 listeners on one address", server("198.51.100.1", 63), "$.spec.listeners", 63},
		{"61 endpoint addresses under a wildcard", server("0.0.0.0", 1, endpointsOf("l00", 61)...), "", 0},
		{"62 endpoint addresses under a wildcard", server("0.0.0.0", 1, endpointsOf("l00", 62)...), "$.spec.endpoints", 63},
		{"two listeners over the same 31 addresses", server("0.0.0.0", 2, append(endpointsOf("l00", 31), endpointsOf("l01", 31)...)...), "$.spec.endpoints", 64},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			issues := validated(t, test.server, catalog)
			if test.field == "" {
				if len(issues) != 0 {
					t.Fatalf("a claim within the bound was refused: %+v", issues)
				}
				return
			}
			if len(issues) != 1 {
				t.Fatalf("the claim gave %+v, want one issue", issues)
			}
			issue := issues[0]
			if issue.Field != test.field || issue.Code != "api.value" ||
				!strings.Contains(issue.Message, fmt.Sprintf("claim at most 62 sockets; this one claims %d", test.claims)) ||
				!strings.Contains(issue.Remediation, "so they claim at most 62 sockets") {
				t.Fatalf("the refusal is %+v", issue)
			}
		})
	}
}
