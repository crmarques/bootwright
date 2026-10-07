//go:build linux && amd64

package contextfs

import (
	"fmt"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// ambientLookup answers the proxy variables an operator exported.
func ambientLookup(pairs ...string) func(string) (string, bool) {
	values := map[string]string{}
	for index := 0; index+1 < len(pairs); index += 2 {
		values[pairs[index]] = pairs[index+1]
	}
	return func(name string) (string, bool) {
		value, present := values[name]
		return value, present
	}
}

// bypassEntries is count distinct qualified bypass host names.
func bypassEntries(count int) string {
	entries := make([]string, 0, count)
	for index := range count {
		entries = append(entries, fmt.Sprintf("host%03d.example", index))
	}
	return strings.Join(entries, ",")
}

// Setup records the route it acquired over in its receipt, which the store
// validates on publication, after confirmation and resolution. So every route
// the ambient grammar admits must yield a receipt egress the store accepts,
// built exactly as setup builds it, and every route the grammar refuses must
// refuse before any of that, naming the variable to correct (F-079).
func TestEveryAmbientRouteYieldsAReceiptEgressTheStoreAccepts(t *testing.T) {
	admitted := map[string][]string{
		"an uppercase host":         {"HTTPS_PROXY", "http://PROXY.Example:3128"},
		"an uppercase IPv6 literal": {"HTTPS_PROXY", "http://[FD00::1]:3128"},
		"an IPv4 literal":           {"HTTPS_PROXY", "http://192.0.2.10:3128"},
		"an https endpoint":         {"HTTPS_PROXY", "https://proxy.example:8443"},
		"a trailing slash":          {"HTTPS_PROXY", "http://proxy.example:3128/"},
		"no port":                   {"HTTPS_PROXY", "http://proxy.example"},
		"128 bypass entries":        {"HTTPS_PROXY", "http://proxy.example:3128", "NO_PROXY", bypassEntries(128)},
		"a 1024-byte bypass entry":  {"HTTPS_PROXY", "http://proxy.example:3128", "NO_PROXY", strings.Repeat("a", 1011) + ".example.test"},
		"a mixed-case bypass entry": {"HTTPS_PROXY", "http://proxy.example:3128", "NO_PROXY", "Internal.Example.TEST,10.0.0.0/8"},
		"no proxy at all":           {},
	}
	for name, pairs := range admitted {
		t.Run("admits "+name, func(t *testing.T) {
			route, err := controller.RouteFromEnvironment(ambientLookup(pairs...))
			if err != nil {
				t.Fatalf("the ambient route was refused: %#v", diagnostics.Of(err))
			}
			// The egress setup records, as its inspection builds it from the
			// selected route (internal/controller/prerequisites/service.go).
			bypass := route.NoProxy()
			if bypass == nil {
				bypass = []string{}
			}
			receipt := syntheticControllerState(t, prerequisites.SetupContext{}).Receipt
			receipt.Egress = prerequisites.SetupEgress{HTTPProxy: route.HTTPProxy(), HTTPSProxy: route.HTTPSProxy(), NoProxy: bypass}
			if err := validateControllerReceiptIdentity(receipt); err != nil {
				t.Fatalf("the store refuses the egress %+v the route yields: %#v", receipt.Egress, diagnostics.Of(err))
			}
		})
	}
	refused := map[string][]string{
		"a 1025-byte bypass entry": {"HTTPS_PROXY", "http://proxy.example:3128", "NO_PROXY", strings.Repeat("a", 1012) + ".example.test"},
		"129 bypass entries":       {"HTTPS_PROXY", "http://proxy.example:3128", "NO_PROXY", bypassEntries(129)},
		"HTTP_PROXY alone":         {"HTTP_PROXY", "http://proxy.example:3128"},
		"credentials":              {"HTTPS_PROXY", "http://user:secret@proxy.example:3128"},
		"a path":                   {"HTTPS_PROXY", "http://proxy.example:3128/route"},
		"conflicting spellings":    {"HTTPS_PROXY", "http://one.example:3128", "https_proxy", "http://two.example:3128"},
	}
	for name, pairs := range refused {
		t.Run("refuses "+name, func(t *testing.T) {
			route, err := controller.RouteFromEnvironment(ambientLookup(pairs...))
			reported := diagnostics.Of(err)
			if err == nil || len(reported) != 1 {
				t.Fatalf("the ambient route %+v was admitted or refused without one diagnostic: %#v", route, reported)
			}
			text := reported[0].Message + " " + reported[0].Remediation
			if !strings.Contains(text, "HTTPS_PROXY") && !strings.Contains(text, "NO_PROXY") {
				t.Fatalf("the refusal names neither HTTPS_PROXY nor NO_PROXY: %#v", reported[0])
			}
		})
	}
}
