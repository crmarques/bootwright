package managedservice

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

// A host reservation knows only the contexts this host coordinates, so the
// adapter proves a socket held by anything else before its first effect and
// names the port it found held. The service's own diagnostic replaces the
// adapter's failure: the service as the refused object, the exact socket it
// binds, or for a wildcard bind the port and the bind that covers it, and what
// to change.
func TestAForeignListenerRefusalNamesTheSocketAndTheRemedy(t *testing.T) {
	for _, tc := range []struct {
		bind  string
		names []string
	}{
		{"192.0.2.1", []string{"192.0.2.1:53"}},
		{"fd00::1", []string{"[fd00::1]:53"}},
		{"0.0.0.0", []string{"port 53", "wildcard bind 0.0.0.0"}},
		{"::", []string{"port 53", "wildcard bind ::"}},
	} {
		refusals := ForeignListenerRefusals("DNSServer", "dns", tc.bind, []int{53})
		if keys := slices.Collect(maps.Keys(refusals)); !slices.Equal(keys, []string{"foreign-listener-53"}) {
			t.Fatalf("%s: refusal keys = %v", tc.bind, keys)
		}
		reported := diagnostics.Of(refusals["foreign-listener-53"])
		if len(reported) != 1 {
			t.Fatalf("%s: diagnostics = %#v", tc.bind, reported)
		}
		got := reported[0]
		if got.Code != "lifecycle.state" || got.Object == nil || got.Object.Kind != "DNSServer" || got.Object.Name != "dns" {
			t.Fatalf("%s: refusal = %#v, want lifecycle.state on DNSServer/dns", tc.bind, got)
		}
		for _, name := range tc.names {
			if !strings.Contains(got.Message, name) {
				t.Fatalf("%s: message %q does not name %q", tc.bind, got.Message, name)
			}
		}
		if !strings.Contains(got.Remediation, "stop what listens there, or choose another bindAddress on DNSServer/dns") ||
			strings.Contains(got.Remediation, "or port") {
			t.Fatalf("%s: remediation = %q, want only a remedy validation admits", tc.bind, got.Remediation)
		}
		if wildcard := strings.HasPrefix(tc.bind, "0.") || tc.bind == "::"; wildcard != strings.Contains(got.Remediation, "retained run output") {
			t.Fatalf("%s: remediation = %q, want the run output named for a wildcard bind only", tc.bind, got.Remediation)
		}
	}
	several := ForeignListenerRefusals("ArtifactServer", "media", "192.0.2.1", []int{80, 443})
	if keys := slices.Sorted(maps.Keys(several)); !slices.Equal(keys, []string{"foreign-listener-443", "foreign-listener-80"}) {
		t.Fatalf("refusal keys = %v, want one per port", keys)
	}
	if message := diagnostics.Of(several["foreign-listener-443"])[0].Message; !strings.Contains(message, "192.0.2.1:443") {
		t.Fatalf("message = %q, want the port it names", message)
	}
}

// A resolver and a time service keep the one port validation permits, so only
// a service whose port is declared offers choosing another one.
func TestAForeignListenerRemedyOffersOnlyWhatValidationAdmits(t *testing.T) {
	for kind, wantPort := range map[string]bool{"DNSServer": false, "NTPServer": false, "Proxy": true, "ArtifactServer": true} {
		refusals := ForeignListenerRefusals(kind, "svc", "192.0.2.1", []int{8080})
		remedy := diagnostics.Of(refusals["foreign-listener-8080"])[0].Remediation
		if strings.Contains(remedy, "or port") != wantPort {
			t.Fatalf("%s: remediation = %q, offers a port choice = %t, want %t", kind, remedy, strings.Contains(remedy, "or port"), wantPort)
		}
	}
}
