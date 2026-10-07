package prerequisites

import (
	"errors"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

// Every class a controller adapter may name maps to its own condition and
// remedy; an acquisition class needs the source's host and a native class
// none, internal and any other token map to nothing, and a refusal after an
// authorized native transaction is unknown.
func TestEveryAdapterRefusalClassMaps(t *testing.T) {
	const host = "cdn.example.test"
	for _, check := range []struct {
		reason, host, message string
		routable              bool
	}{
		{"dns", host, "cdn.example.test could not be resolved by this host's configured resolver", true},
		{"certificate", host, "cdn.example.test presented a certificate the qualified system trust store does not accept", false},
		{"timeout", host, "cdn.example.test did not answer within its bounded acquisition deadline", true},
		{"unreachable", host, "cdn.example.test could not be reached from this host", true},
		{"proxy", host, "the HTTPS proxy refused or failed the connection to cdn.example.test", true},
		{"status", host, "cdn.example.test answered the approved download with an unexpected response", false},
		{"redirect", host, "cdn.example.test redirected the approved download outside the approved publishers", false},
		{"integrity", host, "the download from cdn.example.test differs from its approved size or digest", false},
		{"trust", host, "this host's system trust store /etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem is unreadable or unsafe", false},
		{"storage", host, "the filesystem holding /var/tmp could not hold the approved download from cdn.example.test", false},
		{"solver-conflict", "", "the native package solver found the requested dependencies in conflict with this host's installed packages", false},
		{"missing-candidate", "", "the approved repositories offer no package satisfying a requested dependency version", false},
		{"signature", "", "a native package failed its vendor signature check", false},
		{"database", "", "the native package database was busy or changed during the operation", false},
		{"transaction", "", "a vendor package script failed the native transaction", false},
		{"postcondition", "", "the host's package inventory differs from the frozen plan", false},
		{"foundation", "", "the provided OS Python and DNF foundation is incomplete", false},
		{"timeout", "", "the native package inspection did not finish within its bound", false},
	} {
		t.Run(check.reason+" "+check.host, func(t *testing.T) {
			area := ""
			if check.host != "" {
				area = "/var/tmp"
			}
			err, mapped := AdapterRefusal(check.reason, check.host, area, false)
			var scoped *ScopedFailure
			if !mapped || !errors.As(err, &scoped) || scoped.Code != "controller.setup" || scoped.Message != check.message || scoped.Routable != check.routable || scoped.Correction == "" {
				t.Fatalf("AdapterRefusal(%q, %q) = %+v %v, want %q routable %v", check.reason, check.host, scoped, mapped, check.message, check.routable)
			}
			found := diagnostics.Of(err)
			if len(found) != 1 || !strings.HasSuffix(found[0].Remediation, ", then rerun bootwright setup.") {
				t.Fatalf("the refusal renders %+v", found)
			}
			err, mapped = AdapterRefusal(check.reason, check.host, area, true)
			if !mapped || !errors.As(err, &scoped) || scoped.Code != "controller.unknown" || scoped.Correction != "Resolve the native transaction" || scoped.Message != check.message {
				t.Fatalf("an authorized refusal = %+v %v, want controller.unknown", scoped, mapped)
			}
		})
	}
	if err, mapped := AdapterRefusal("dns", host, "/var/tmp", false); !mapped || !strings.Contains(err.(*ScopedFailure).Correction, host) {
		t.Fatalf("the dns remedy names no host: %v", err)
	}
	for _, check := range []struct{ reason, host string }{
		{"internal", ""}, {"internal", host}, {"release-stamp", ""}, {"unknown-class", ""},
		{"dns", ""}, {"solver-conflict", host}, {"", ""}, {"storage", ""},
	} {
		if err, mapped := AdapterRefusal(check.reason, check.host, "", false); mapped || err != nil {
			t.Errorf("AdapterRefusal(%q, %q) mapped to %v", check.reason, check.host, err)
		}
	}
	if err, mapped := AdapterRefusal("storage", host, "", false); mapped || err != nil {
		t.Errorf("a storage refusal that names no area mapped to %v", err)
	}
}
