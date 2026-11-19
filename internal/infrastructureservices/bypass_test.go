package infrastructureservices

import (
	"net/url"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestAProxyBypassEntryOutsideTheGrammarParsesToNoRule(t *testing.T) {
	for _, entry := range []string{"", "bad entry/x", "10.0.0.0/33", "example.test:", "example.test:80:81", "[::1", "host.", "*."} {
		if _, ok := ParseProxyBypass(entry); ok {
			t.Fatalf("%q parsed", entry)
		}
	}
}

func TestAProxyBypassRuleMatchesTheTargetsItsFormNames(t *testing.T) {
	cases := []struct {
		entry   string
		port    bool
		matches map[string]bool
	}{
		{"*", false, map[string]bool{"https://any.example.test": true, "http://192.0.2.1": true}},
		{"Registry.Example.Test", false, map[string]bool{
			"https://registry.example.test": true, "https://REGISTRY.example.test": true,
			"https://a.registry.example.test": true, "https://other.example.test": false, "https://192.0.2.1": false,
		}},
		{".example.test", false, map[string]bool{"https://a.example.test": true, "https://a.b.example.test": true, "https://other.test": false}},
		{"*.example.test", false, map[string]bool{"https://a.example.test": true, "https://notexample.test": false}},
		{"192.0.2.0/24", false, map[string]bool{"https://192.0.2.7": true, "https://192.0.3.7": false, "https://example.test": false}},
		{"192.0.2.9", false, map[string]bool{"https://192.0.2.9": true, "https://192.0.2.10": false, "https://example.test": false}},
		{"[2001:db8::1]:8443", true, map[string]bool{"https://[2001:db8::1]:8443": true, "https://[2001:db8::1]": false, "https://[2001:db8::2]:8443": false}},
		{"::ffff:192.0.2.9", false, map[string]bool{"https://192.0.2.9": true}},
		{"example.test:8080", true, map[string]bool{"http://example.test:8080": true, "http://example.test": false, "https://example.test:8080": true, "https://example.test": false}},
		{"example.test:443", true, map[string]bool{"https://example.test": true, "http://example.test": false}},
		{"example.test:80", true, map[string]bool{"http://example.test": true, "https://example.test": false}},
	}
	for _, c := range cases {
		rule, ok := ParseProxyBypass(c.entry)
		if !ok {
			t.Fatalf("%q was refused", c.entry)
		}
		if rule.PortQualified() != c.port {
			t.Fatalf("%q port qualified = %v", c.entry, rule.PortQualified())
		}
		for target, want := range c.matches {
			parsed, err := url.Parse(target)
			if err != nil {
				t.Fatal(err)
			}
			if got := rule.Matches(parsed); got != want {
				t.Fatalf("%q matches %s = %v, want %v", c.entry, target, got, want)
			}
		}
	}
}

func TestProxyFactsNameTheManagementAndConnectionOfAProxy(t *testing.T) {
	external := obj(api.Proxy, "egress", m("management", "external", "connection", m(
		"httpProxy", "http://proxy.example.test:3128", "httpsProxy", "http://proxy.example.test:3129",
		"auth", m("proxyAuthRef", "credentials"), "trustBundleRef", "private-ca")))
	facts := ProxyFactsOf(external)
	if facts.Management != "external" || !facts.Authenticated || !facts.PrivateTrusted ||
		facts.HTTPProxy.Text() != "http://proxy.example.test:3128" || facts.HTTPSProxy.Text() != "http://proxy.example.test:3129" {
		t.Fatalf("facts = %+v", facts)
	}
	managed := ProxyFactsOf(obj(api.Proxy, "local", m("management", "managed")))
	if managed.Management != "managed" || managed.Authenticated || managed.PrivateTrusted || managed.HTTPProxy.Present() || managed.HTTPSProxy.Present() {
		t.Fatalf("facts = %+v", managed)
	}
}
