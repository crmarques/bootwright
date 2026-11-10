package infrastructureservices

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestAProxyEndpointOutsideTheGrammarRefusesAtItsField(t *testing.T) {
	const bare = "http://proxy.example.test:3128"
	connectionIssues := func(field, value string, partial bool) []api.Issue {
		proxy := obj(api.Proxy, "egress", m("management", "external", "connection", m(field, value)))
		var found []api.Issue
		issues := validateIntrinsic(proxy, partial)
		if !partial {
			issues = Validate(proxy, api.NewCatalog([]api.Object{proxy}))
		}
		for _, issue := range issues {
			if strings.HasPrefix(issue.Field, "$.spec.connection") {
				found = append(found, issue)
			}
		}
		return found
	}
	for _, field := range []string{"httpProxy", "httpsProxy"} {
		for name, value := range map[string]string{
			"a path":     bare + "/route",
			"a query":    bare + "/?route=1",
			"a fragment": bare + "/#route",
			"4097 bytes": bare + "/" + strings.Repeat("a", 4097-len(bare)-1),
		} {
			for _, partial := range []bool{false, true} {
				issues := connectionIssues(field, value, partial)
				if len(issues) != 1 || issues[0].Code != "api.value" || issues[0].Field != "$.spec.connection."+field ||
					issues[0].Remediation != "set spec.connection."+field+" to "+api.ProxyEndpointForm {
					t.Fatalf("%s with %s (partial %v) = %+v", field, name, partial, issues)
				}
				for _, part := range []string{"proxy.example.test", "route", "aaaa"} {
					if strings.Contains(issues[0].Message, part) {
						t.Fatalf("the refusal of %s with %s echoes its value: %q", field, name, issues[0].Message)
					}
				}
			}
		}
		for _, value := range []string{bare, bare + "/", "http://proxy.example.test"} {
			if issues := connectionIssues(field, value, false); len(issues) != 0 {
				t.Fatalf("%s = %q was refused: %+v", field, value, issues)
			}
		}
	}
}

func TestEveryProxyChoiceHoldsTheBypassGrammar(t *testing.T) {
	choice := m("proxyRef", "egress", "noProxy", api.StringList(".example.test", "10.0.0.0/33", strings.Repeat("a", 1012)+".example.test"))
	issues := ValidateProxyChoice(choice, "$.spec.install.proxy")
	if len(issues) != 2 || issues[0].Field != "$.spec.install.proxy.noProxy[1]" || issues[1].Field != "$.spec.install.proxy.noProxy[2]" {
		t.Fatalf("issues = %+v", issues)
	}
	if issues[0].Code != "api.value" || issues[0].Remediation != "write the entry as "+api.ProxyBypassForms {
		t.Fatalf("issue = %+v", issues[0])
	}
	for _, issue := range issues {
		if strings.Contains(issue.Message+issue.Remediation, "example.test") || strings.Contains(issue.Message+issue.Remediation, "10.0.0.0") {
			t.Fatalf("the refusal echoes its entry: %+v", issue)
		}
	}
	bound := m("proxyRef", "egress", "noProxy", api.StringList(strings.Repeat("a", 1011)+".example.test"))
	if issues := ValidateProxyChoice(bound, "$.spec.install.proxy"); len(issues) != 0 {
		t.Fatalf("a 1024-byte bypass entry was refused: %+v", issues)
	}
}

// A defaults entry reports the same refusal under the Environment, which has
// no spec.install or spec.proxy, so the remedy names no field path and the
// diagnostic's field locates the entry.
func TestABypassRemedyNamesNoFieldPath(t *testing.T) {
	for _, field := range []string{"$.spec.install.proxy", "$.spec.proxy"} {
		choice := m("proxyRef", "egress", "noProxy", api.StringList("bad entry/x"))
		issues := ValidateProxyChoice(choice, field)
		if len(issues) != 1 || strings.Contains(issues[0].Remediation, "spec.") {
			t.Fatalf("%s: issues = %+v, want a remedy that names no spec path", field, issues)
		}
	}
}

func TestAManagedProxyRefusesALocalAllowedDestinationAndAnExternalOneAnyList(t *testing.T) {
	declared := api.StringList(".redhat.com", "127.0.0.1", "[::1]:443", "169.254.169.254", "fe80::1", "0.0.0.0", "::ffff:127.0.0.1", "192.0.2.7:8443")
	managed := obj(api.Proxy, "egress", m("management", "managed", "machineRef", "host", "implementation", "squid", "allowedDestinations", declared))
	var fields []string
	for _, issue := range validateIntrinsic(managed, true) {
		if strings.HasPrefix(issue.Field, "$.spec.allowedDestinations") {
			fields = append(fields, issue.Field)
		}
	}
	if strings.Join(fields, " ") != "$.spec.allowedDestinations[1] $.spec.allowedDestinations[2] $.spec.allowedDestinations[3] $.spec.allowedDestinations[4] $.spec.allowedDestinations[5] $.spec.allowedDestinations[6]" {
		t.Fatalf("refused %v", fields)
	}
	external := obj(api.Proxy, "egress", m("management", "external", "connection", m("httpProxy", "http://proxy.example.test:3128"), "allowedDestinations", api.StringList(".redhat.com")))
	issues := validateIntrinsic(external, false)
	if len(issues) != 1 || issues[0].Field != "$.spec.allowedDestinations" || !strings.Contains(issues[0].Remediation, "management: external does not use") {
		t.Fatalf("external issues = %+v", issues)
	}
}
