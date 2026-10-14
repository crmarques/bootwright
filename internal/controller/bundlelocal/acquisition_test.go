package bundlelocal

import (
	"net/http"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

func TestExplicitProxyIgnoresAmbientAndMatchesWithoutDNS(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://ambient.invalid:3128")
	t.Setenv("HTTP_PROXY", "http://ambient.invalid:3128")
	t.Setenv("ALL_PROXY", "http://ambient.invalid:3128")
	t.Setenv("NO_PROXY", "*")
	request, _ := http.NewRequest(http.MethodGet, "https://files.pythonhosted.org/package", nil)
	direct, err := explicitProxy(prerequisites.SetupEgress{})
	if err != nil {
		t.Fatal(err)
	}
	if selected, err := direct(request); err != nil || selected != nil {
		t.Fatal("direct acquisition used ambient proxy")
	}
	proxied, err := explicitProxy(prerequisites.SetupEgress{HTTPSProxy: "http://explicit.example:3128", NoProxy: []string{".bypass.example", "192.0.2.0/24", "exact.example:443"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		target string
		bypass bool
	}{
		{"https://files.pythonhosted.org/package", false},
		{"https://bypass.example/package", false},
		{"https://child.bypass.example/package", true},
		{"https://192.0.2.20/package", true},
		{"https://192.0.3.20/package", false},
		{"https://exact.example/package", true},
		{"https://exact.example:444/package", false},
	} {
		request, _ := http.NewRequest(http.MethodGet, test.target, nil)
		selected, err := proxied(request)
		if err != nil || (selected == nil) != test.bypass {
			t.Fatalf("proxy route %q: %v %v", test.target, selected, err)
		}
	}
	for _, policy := range []prerequisites.SetupEgress{{HTTPSProxy: "http://user:secret@proxy.example"}, {HTTPProxy: "http://proxy.example"}, {HTTPSProxy: "http://proxy.example/config"}, {NoProxy: []string{"https://invalid.example"}}} {
		if _, err := explicitProxy(policy); err == nil {
			t.Fatal("accepted unqualified explicit proxy policy")
		}
	}
}
