package prerequisites

import "testing"

func TestMirrorAuthorityKeepsPrivateTrustScopedToItsHTTPSPort(t *testing.T) {
	// A mirror and vCenter can share a hostname and have separate trust anchors.
	for endpoint, want := range map[string]string{
		"https://Services.Example.Test/tools":     "services.example.test",
		"https://services.example.test:443/sdk":   "services.example.test",
		"https://services.example.test:8443/sdk":  "services.example.test:8443",
		"https://[2001:db8::1]:8443/sdk":          "[2001:db8::1]:8443",
		"http://services.example.test:8443/tools": "",
	} {
		if got := MirrorAuthority(endpoint); got != want {
			t.Errorf("authority of %q = %q, want %q", endpoint, got, want)
		}
	}
}
