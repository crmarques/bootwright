package installation

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func externalProxy(name string, connection ...api.FieldValue) api.Object {
	return api.NewObject(api.Proxy, name, api.Value{}, api.MapValue(
		text("management", "external"), field("connection", api.MapValue(connection...)),
	))
}

func proxiedGuest(noProxy ...string) api.Object {
	choice := []api.FieldValue{text("proxyRef", "corporate")}
	if len(noProxy) != 0 {
		choice = append(choice, field("noProxy", api.StringList(noProxy...)))
	}
	return guest(field("proxy", api.MapValue(choice...)))
}

func repositoryProfile(baseURLs ...string) api.Object {
	var entries []api.Value
	for index, baseURL := range baseURLs {
		entries = append(entries, api.MapValue(text("id", "repo"+string(rune('a'+index))), text("baseURL", baseURL),
			field("gpgCheck", api.BoolValue(false))))
	}
	return profileCustomizedWith("repositories", api.MapValue(field("configure", api.ListValue(entries...))))
}

func repositoryProxies(t *testing.T, catalog api.Catalog) []string {
	t.Helper()
	kickstart := kickstartOf(t, catalog)
	var proxies []string
	for _, block := range strings.Split(kickstart, "<<'BOOTWRIGHT_REPOSITORY_EOF'\n")[1:] {
		block = block[:strings.Index(block, "BOOTWRIGHT_REPOSITORY_EOF")]
		proxy := ""
		for _, line := range strings.Split(block, "\n") {
			if value, found := strings.CutPrefix(line, "proxy="); found {
				proxy = value
			}
		}
		proxies = append(proxies, proxy)
	}
	return proxies
}

var corporate = externalProxy("corporate",
	text("httpProxy", "http://proxy.example.test:3128"), text("httpsProxy", "http://proxy.example.test:3129"))

// The installed system reaches a repository through the scheme's proxy, or
// the other one when the Proxy declares only that, and the .repo file is the
// one place the proxy is written.
func TestARepositoryReachedThroughTheProxyCarriesIt(t *testing.T) {
	catalog := labCatalog(corporate, proxiedGuest(), repositoryProfile("https://mirror.example.test/a", "http://mirror.example.test/b"))
	if got := repositoryProxies(t, catalog); len(got) != 2 || got[0] != "http://proxy.example.test:3129" || got[1] != "http://proxy.example.test:3128" {
		t.Fatalf("proxies = %q", got)
	}
	httpOnly := externalProxy("corporate", text("httpProxy", "http://proxy.example.test:3128"))
	catalog = labCatalog(httpOnly, proxiedGuest(), repositoryProfile("https://mirror.example.test/a"))
	if got := repositoryProxies(t, catalog); len(got) != 1 || got[0] != "http://proxy.example.test:3128" {
		t.Fatalf("an https repository behind an http-only Proxy carries %q", got)
	}
	kickstart := kickstartOf(t, catalog)
	if strings.Count(kickstart, "proxy.example.test") != 1 {
		t.Fatalf("the proxy is written outside its .repo file:\n%s", kickstart)
	}
}

// The installation's own artifact endpoints are reached directly, so a
// repository the artifact server serves is never sent through the proxy.
func TestTheArtifactEndpointIsExempt(t *testing.T) {
	catalog := labCatalog(corporate, proxiedGuest(), repositoryProfile("http://192.0.2.1:8080/os/extras", "https://mirror.example.test/b"))
	if got := repositoryProxies(t, catalog); len(got) != 2 || got[0] != "" || got[1] != "http://proxy.example.test:3129" {
		t.Fatalf("proxies = %q", got)
	}
}

// A noProxy entry of the Machine's choice exempts every repository host it
// covers, in each form the proxy choice admits.
func TestNoProxyEntriesExemptARepository(t *testing.T) {
	for name, test := range map[string]struct {
		entry, baseURL string
		exempt         bool
	}{
		"exact host":          {"mirror.example.test", "https://mirror.example.test/a", true},
		"exact host, other":   {"mirror.example.test", "https://other.example.test/a", false},
		"exact host, case":    {"Mirror.Example.Test", "https://mirror.example.test/a", true},
		"suffix":              {".example.test", "https://mirror.example.test/a", true},
		"suffix, bare domain": {".example.test", "https://example.test/a", true},
		"suffix, other":       {".example.test", "https://mirror.example.org/a", false},
		"wildcard suffix":     {"*.example.test", "https://mirror.example.test/a", true},
		"suffix, no dot":      {".example.test", "https://badexample.test/a", false},
		"ip":                  {"203.0.113.7", "https://203.0.113.7/a", true},
		"ip, other":           {"203.0.113.7", "https://203.0.113.8/a", false},
		"cidr":                {"203.0.113.0/24", "https://203.0.113.9/a", true},
		"cidr, name":          {"203.0.113.0/24", "https://mirror.example.test/a", false},
		"ipv6":                {"2001:db8::7", "https://[2001:db8::7]/a", true},
		"ipv6 cidr":           {"2001:db8::/32", "https://[2001:db8::9]/a", true},
		"star":                {"*", "https://mirror.example.test/a", true},
		"host with port":      {"mirror.example.test:443", "https://mirror.example.test/a", true},
		"ip with port":        {"203.0.113.7:443", "https://203.0.113.7/a", true},
		"ipv6 with port":      {"[2001:db8::7]:443", "https://[2001:db8::7]/a", true},
	} {
		t.Run(name, func(t *testing.T) {
			got := repositoryProxies(t, labCatalog(corporate, proxiedGuest(test.entry), repositoryProfile(test.baseURL)))
			want := "http://proxy.example.test:3129"
			if test.exempt {
				want = ""
			}
			if len(got) != 1 || got[0] != want {
				t.Fatalf("noProxy %q over %s: proxies = %q, want %q", test.entry, test.baseURL, got, want)
			}
		})
	}
}

// A Machine that selects direct access, or no proxy at all, writes no proxy
// even when the profile selects one.
func TestADirectMachineRendersNoProxy(t *testing.T) {
	profile := repositoryProfile("https://mirror.example.test/a")
	profile = profile.WithSpec(profile.Spec().With("proxy", api.MapValue(text("proxyRef", "corporate"))))
	direct := guest(field("proxy", api.MapValue(field("direct", api.MapValue()))))
	if got := repositoryProxies(t, labCatalog(corporate, direct, profile)); len(got) != 1 || got[0] != "" {
		t.Fatalf("a direct Machine wrote proxies %q", got)
	}
	if got := repositoryProxies(t, labCatalog(corporate, repositoryProfile("https://mirror.example.test/a"))); len(got) != 1 || got[0] != "" {
		t.Fatalf("a Machine with no proxy choice wrote proxies %q", got)
	}
	if got := repositoryProxies(t, labCatalog(corporate, profile)); len(got) != 1 || got[0] != "http://proxy.example.test:3129" {
		t.Fatalf("a Machine inheriting its profile's proxy wrote %q", got)
	}
}
