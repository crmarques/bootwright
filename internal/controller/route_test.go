package controller_test

import (
	"net/url"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func environment(pairs ...string) func(string) (string, bool) {
	values := map[string]string{}
	for index := 0; index+1 < len(pairs); index += 2 {
		values[pairs[index]] = pairs[index+1]
	}
	return func(name string) (string, bool) {
		value, present := values[name]
		return value, present
	}
}

func TestAmbientRouteSelectsTheHTTPSEndpointOperatorsExport(t *testing.T) {
	for _, test := range []struct {
		name     string
		pairs    []string
		endpoint string
		bypass   int
	}{
		{"unset", nil, "", 0},
		{"uppercase", []string{"HTTPS_PROXY", "http://proxy.example:3128"}, "http://proxy.example:3128", 0},
		{"lowercase", []string{"https_proxy", "http://proxy.example:3128"}, "http://proxy.example:3128", 0},
		{"both spellings agreeing", []string{"HTTPS_PROXY", "http://proxy.example:3128", "https_proxy", "http://proxy.example:3128"}, "http://proxy.example:3128", 0},
		{"https endpoint", []string{"HTTPS_PROXY", "https://proxy.example:8443"}, "https://proxy.example:8443", 0},
		{"surrounding whitespace", []string{"HTTPS_PROXY", "  http://proxy.example:3128\t"}, "http://proxy.example:3128", 0},
		{"empty is unset", []string{"HTTPS_PROXY", "", "NO_PROXY", "*"}, "", 0},
		{"bypass entries", []string{"HTTPS_PROXY", "http://proxy.example:3128", "NO_PROXY", ".internal.example, 10.0.0.0/8 ,registry.example:443"}, "http://proxy.example:3128", 3},
		{"bypass duplicates and blanks collapse", []string{"HTTPS_PROXY", "http://proxy.example:3128", "NO_PROXY", ".internal.example,,.internal.example, "}, "http://proxy.example:3128", 1},
		{"bypass without a proxy is dropped", []string{"NO_PROXY", ".internal.example"}, "", 0},
		{"ALL_PROXY is ignored", []string{"ALL_PROXY", "socks5://proxy.example:1080"}, "", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			route, err := controller.RouteFromEnvironment(environment(test.pairs...))
			if err != nil {
				t.Fatal(err)
			}
			if !route.Configured() {
				t.Fatal("an ambient route must always be a decision")
			}
			if route.HTTPSProxy() != test.endpoint || route.Direct() != (test.endpoint == "") {
				t.Fatal("endpoint =", route.HTTPSProxy(), "direct =", route.Direct())
			}
			if len(route.NoProxy()) != test.bypass {
				t.Fatal("bypass =", route.NoProxy())
			}
			if route.HTTPProxy() != "" {
				t.Fatal("an HTTP proxy was invented", route.HTTPProxy())
			}
		})
	}
}

func TestAmbientRouteRefusesEveryAmbiguousOrUnqualifiedValue(t *testing.T) {
	for _, test := range []struct {
		name  string
		pairs []string
	}{
		{"http proxy alone", []string{"HTTP_PROXY", "http://proxy.example:3128"}},
		{"conflicting spellings", []string{"HTTPS_PROXY", "http://one.example:3128", "https_proxy", "http://two.example:3128"}},
		{"conflicting bypass spellings", []string{"HTTPS_PROXY", "http://proxy.example:3128", "NO_PROXY", ".one.example", "no_proxy", ".two.example"}},
		{"embedded credentials", []string{"HTTPS_PROXY", "http://user:secret@proxy.example:3128"}},
		{"unsupported scheme", []string{"HTTPS_PROXY", "socks5://proxy.example:1080"}},
		{"relative endpoint", []string{"HTTPS_PROXY", "proxy.example:3128"}},
		{"endpoint with a path", []string{"HTTPS_PROXY", "http://proxy.example:3128/route"}},
		{"endpoint with a query", []string{"HTTPS_PROXY", "http://proxy.example:3128?a=b"}},
		{"endpoint with a fragment", []string{"HTTPS_PROXY", "http://proxy.example:3128#a"}},
		{"endpoint without a host", []string{"HTTPS_PROXY", "http://"}},
		{"unbounded endpoint", []string{"HTTPS_PROXY", "http://proxy.example:3128/" + strings.Repeat("a", 4096)}},
		{"non-ascii endpoint", []string{"HTTPS_PROXY", "http://pröxy.example:3128"}},
		{"bypass with a scheme", []string{"HTTPS_PROXY", "http://proxy.example:3128", "NO_PROXY", "https://internal.example"}},
		{"bypass with a path", []string{"HTTPS_PROXY", "http://proxy.example:3128", "NO_PROXY", "internal.example/api"}},
		{"bypass with credentials", []string{"HTTPS_PROXY", "http://proxy.example:3128", "NO_PROXY", "user@internal.example"}},
		{"bypass with a named port", []string{"HTTPS_PROXY", "http://proxy.example:3128", "NO_PROXY", "internal.example:https"}},
		{"bypass with an empty port", []string{"HTTPS_PROXY", "http://proxy.example:3128", "NO_PROXY", "internal.example:"}},
		{"bypass with a trailing dot", []string{"HTTPS_PROXY", "http://proxy.example:3128", "NO_PROXY", "internal.example."}},
	} {
		t.Run(test.name, func(t *testing.T) {
			route, err := controller.RouteFromEnvironment(environment(test.pairs...))
			if err == nil {
				t.Fatal("an unqualified ambient route was admitted", route)
			}
			found := diagnostics.Of(err)
			if len(found) != 1 || found[0].Code != "controller.unsupported" || found[0].Remediation == "" {
				t.Fatal("refusal carries no actionable controller diagnostic", found)
			}
			if strings.Contains(found[0].Message, "secret") || strings.Contains(found[0].Message, "user:") {
				t.Fatal("refusal echoed the value it rejected", found[0].Message)
			}
		})
	}
}

func TestAmbientRouteBoundsItsBypassList(t *testing.T) {
	entries := make([]string, 0, 129)
	for index := range 129 {
		entries = append(entries, "host"+string(rune('a'+index%26))+string(rune('a'+index/26))+".example")
	}
	pairs := []string{"HTTPS_PROXY", "http://proxy.example:3128", "NO_PROXY", strings.Join(entries, ",")}
	if _, err := controller.RouteFromEnvironment(environment(pairs...)); err == nil {
		t.Fatal("an unbounded bypass list was admitted")
	}
	pairs[3] = strings.Join(entries[:128], ",")
	route, err := controller.RouteFromEnvironment(environment(pairs...))
	if err != nil || len(route.NoProxy()) != 128 {
		t.Fatal(err, len(route.NoProxy()))
	}
}

func TestAmbientRouteIgnoresTheProcessEnvironmentWithoutALookup(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://ambient.invalid:3128")
	t.Setenv("NO_PROXY", "*")
	route, err := controller.RouteFromEnvironment(nil)
	if err != nil || !route.Direct() || route.HTTPSProxy() != "" {
		t.Fatal("a route was read without an explicit lookup", route, err)
	}
	if controller.Baseline().Route().Configured() != true || !controller.Baseline().Route().Direct() {
		t.Fatal("the context-free baseline stopped being an explicit direct decision")
	}
}

func TestProxySelectorRoutesOnlyWhatNoBypassCovers(t *testing.T) {
	selector, err := controller.NewProxySelector("", "http://proxy.example:3128", []string{".bypass.example", "192.0.2.0/24", "exact.example:443", "*.wild.example"})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		target string
		bypass bool
	}{
		{"https://files.pythonhosted.org/package", false},
		{"https://host.bypass.example/path", true},
		{"https://bypass.example/path", false},
		{"https://wild.example/path", false},
		{"https://192.0.2.7/path", true},
		{"https://192.0.3.7/path", false},
		{"https://exact.example/path", true},
		{"https://exact.example:8443/path", false},
		{"https://node.wild.example/path", true},
	} {
		target, parseErr := url.Parse(test.target)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		if (selector(target) == nil) != test.bypass {
			t.Fatal("route decision is wrong for", test.target)
		}
	}
}

func TestProxySelectorRefusesWhatTheReceiptCannotCarry(t *testing.T) {
	if _, err := controller.NewProxySelector("http://proxy.example:3128", "", nil); err != controller.ErrProxyScheme {
		t.Fatal("an HTTP-only route was admitted", err)
	}
	if _, err := controller.NewProxySelector("", "http://user@proxy.example:3128", nil); err != controller.ErrProxyEndpoint {
		t.Fatal("a credential-bearing proxy was admitted", err)
	}
	if _, err := controller.NewProxySelector("", "http://proxy.example:3128", []string{"not a host"}); err != controller.ErrProxyBypass {
		t.Fatal("an unqualified bypass entry was admitted", err)
	}
	overflow := make([]string, 129)
	for index := range overflow {
		overflow[index] = "host" + string(rune('a'+index%26)) + string(rune('a'+index/26)) + ".example"
	}
	if _, err := controller.NewProxySelector("", "http://proxy.example:3128", overflow); err != controller.ErrProxyBypass {
		t.Fatal("an unbounded bypass list was admitted", err)
	}
}

// Admission and selection check a declared route with the API's proxy rules,
// and the selector builds every route with its own parser, so the two must be
// one grammar: an entry either admits is one the other admits.
func TestProxyGrammarIsOneRule(t *testing.T) {
	for _, test := range []struct {
		value string
		valid bool
	}{
		{"http://proxy.example.test:3128", true},
		{"https://proxy.example.test:8443/", true},
		{"http://192.0.2.10:3128", true},
		{"http://[2001:db8::1]:3128", true},
		{"http://proxy.example.test:3128/path", false},
		{"http://proxy.example.test:3128/?q=1", false},
		{"http://proxy.example.test:3128/#fragment", false},
		{"http://user:secret@proxy.example.test:3128", false},
		{"socks5://proxy.example.test:1080", false},
		{"mailto:proxy@example.test", false},
		{"http:///path", false},
		{"http://proxy.example.test:3128/é", false},
		{"http://" + strings.Repeat("a", 4096) + ".test", false},
	} {
		_, err := controller.NewProxySelector("", test.value, nil)
		if api.ValidLexical("proxy-endpoint", test.value) != test.valid || (err == nil) != test.valid {
			t.Errorf("endpoint %.40q: the API admits %t, the selector %t, want %t", test.value, api.ValidLexical("proxy-endpoint", test.value), err == nil, test.valid)
		}
	}
	for _, test := range []struct {
		value string
		valid bool
	}{
		{"*", true},
		{"10.0.0.0/8", true},
		{"2001:db8::/32", true},
		{"192.0.2.7", true},
		{"2001:db8::1", true},
		{"lab.example.test", true},
		{".example.test", true},
		{"*.example.test", true},
		{"registry.example.test:443", true},
		{"[2001:db8::1]:443", true},
		{"LAB.Example.TEST", true},
		{strings.Repeat("a", 1011) + ".example.test", true},
		{strings.Repeat("a", 1012) + ".example.test", false},
		{strings.Repeat("a", 1007) + ".example.test:443", true},
		{strings.Repeat("a", 1008) + ".example.test:443", false},
		{"10.0.0.0/33", false},
		{"lab.example.test/path", false},
		{"user@lab.example.test", false},
		{"lab.example.test?", false},
		{"lab.example.test#", false},
		{"lab example", false},
		{"lab.example.test.", false},
		{"*.", false},
		{".", false},
		{"lab.example.test:", false},
		{"lab.example.test:https", false},
		{"a:1:2", false},
		{"[2001:db8::1]", false},
		{"[2001:db8::1]443", false},
		{"lab_example.test", false},
		{" lab.example.test", false},
		{"", false},
	} {
		_, err := controller.NewProxySelector("", "http://proxy.example.test:3128", []string{test.value})
		if api.ValidLexical("proxy-bypass", test.value) != test.valid || (err == nil) != test.valid {
			t.Errorf("bypass %.40q: the API admits %t, the selector %t, want %t", test.value, api.ValidLexical("proxy-bypass", test.value), err == nil, test.valid)
		}
	}
}

func TestRouteSummaryNamesItsEndpointAndOrigin(t *testing.T) {
	direct, err := controller.RouteFromEnvironment(environment())
	if err != nil || direct.Summary() != "direct" {
		t.Fatal(direct.Summary(), err)
	}
	single, err := controller.RouteFromEnvironment(environment("HTTPS_PROXY", "http://proxy.example:3128", "NO_PROXY", ".internal.example"))
	if err != nil || single.Summary() != "http://proxy.example:3128 (HTTPS_PROXY), 1 bypass entry" {
		t.Fatal(single.Summary(), err)
	}
	several, err := controller.RouteFromEnvironment(environment("HTTPS_PROXY", "http://proxy.example:3128", "NO_PROXY", ".internal.example,10.0.0.0/8"))
	if err != nil || several.Summary() != "http://proxy.example:3128 (HTTPS_PROXY), 2 bypass entries" {
		t.Fatal(several.Summary(), err)
	}
	if (controller.Route{}).Configured() {
		t.Fatal("the zero route must not present itself as a decision")
	}
}
