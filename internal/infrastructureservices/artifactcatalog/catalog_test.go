package artifactcatalog

import (
	"strings"
	"testing"
)

func TestRegistrationPinsAndCredentialFreeOrigins(t *testing.T) {
	valid := Entry{Name: "helm-linux-amd64", SHA256: strings.Repeat("a", 64), Size: 42}
	for _, origin := range []string{"", "https://example.test/helm", "https://mirror.example.test:443/helm"} {
		entry := valid
		entry.OriginURL = origin
		if !entry.Valid() {
			t.Errorf("refused source %q", origin)
		}
		if origin != "" && entry.Identity() == valid.Identity() {
			t.Errorf("source change did not change the identity for %q", origin)
		}
	}
	for _, origin := range []string{
		"http://example.test/helm", "https://user@example.test/helm", "https://example.test/helm?",
		"https://example.test/helm?signature=credential", "https://example.test/helm#", "https://example.test:444/helm",
		"https://example.test:/helm", "https:///helm", "https://example.test/" + strings.Repeat("a", 4096),
	} {
		entry := valid
		entry.OriginURL = origin
		if entry.Valid() {
			t.Errorf("accepted disallowed origin %.100q", origin)
		}
	}
	if ValidName("../archive") || ValidDigest(valid.SHA256[:63]) {
		t.Fatal("accepted an unsafe name or incomplete digest")
	}
}
