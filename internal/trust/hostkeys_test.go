package trust

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

// blob builds an SSH public key blob the way every key type encodes one: each
// field is a length-prefixed string, beginning with the algorithm name.
func blob(fields ...[]byte) string {
	var out []byte
	for _, field := range fields {
		out = binary.BigEndian.AppendUint32(out, uint32(len(field)))
		out = append(out, field...)
	}
	return base64.StdEncoding.EncodeToString(out)
}

func key(t *testing.T, seed byte) HostKey {
	t.Helper()
	public := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{seed}, ed25519.SeedSize)).Public()
	return HostKey{
		Type:      "ssh-ed25519",
		PublicKey: blob([]byte("ssh-ed25519"), public.(ed25519.PublicKey)),
	}
}

func rsaKey() HostKey {
	return HostKey{
		Type:      "ssh-rsa",
		PublicKey: blob([]byte("ssh-rsa"), []byte{1, 0, 1}, bytes.Repeat([]byte{7}, 256)),
	}
}

func code(t *testing.T, err error) string {
	t.Helper()
	reported := diagnostics.Of(err)
	if len(reported) == 0 {
		t.Fatalf("error carries no diagnostic: %v", err)
	}
	return reported[0].Code
}

func TestHostTokenBracketsOnlyANonDefaultPort(t *testing.T) {
	for _, test := range []struct {
		port int
		want string
	}{{0, "host.test"}, {22, "host.test"}, {2222, "[host.test]:2222"}} {
		if got := HostToken("host.test", test.port); got != test.want {
			t.Fatalf("port %d = %q, want %q", test.port, got, test.want)
		}
	}
}

func TestAKnownHostsEntryBindsExactlyOneDeclaredTarget(t *testing.T) {
	trusted := key(t, 1)
	valid := trusted.Line("host.test", 22)
	parsed, err := ParseKnownHostsLine(valid, "host.test", 22)
	if err != nil {
		t.Fatal(err)
	}
	if parsed != trusted {
		t.Fatalf("parsed = %+v, want %+v", parsed, trusted)
	}
	if _, err := ParseKnownHostsLine(trusted.Line("host.test", 2222), "host.test", 2222); err != nil {
		t.Fatalf("bracketed host token: %v", err)
	}
	if _, err := ParseKnownHostsLine("# comment\n"+valid, "host.test", 22); err != nil {
		t.Fatalf("a comment beside the single entry: %v", err)
	}
}

// Every shape OpenSSH would accept for more than one host, or for a host this
// Machine did not declare, is refused rather than reduced to something narrower.
func TestAKnownHostsEntryRefusesEveryUnboundShape(t *testing.T) {
	trusted := key(t, 2)
	suffix := " " + trusted.Type + " " + trusted.PublicKey
	for _, test := range []struct{ name, text string }{
		{"marker", "@cert-authority host.test" + suffix},
		{"revocation marker", "@revoked host.test" + suffix},
		{"hashed host", "|1|Zm9v|YmFy" + suffix},
		{"host list", "host.test,192.0.2.10" + suffix},
		{"wildcard pattern", "*.test" + suffix},
		{"negated pattern", "!host.test" + suffix},
		{"another host", "other.test" + suffix},
		{"another port", "[host.test]:2222" + suffix},
		{"two entries", trusted.Line("host.test", 22) + trusted.Line("host.test", 22)},
		{"no entry", "\n# only a comment\n"},
		{"too few fields", "host.test " + trusted.Type},
		{"invalid utf-8", "host.test" + suffix + "\xff"},
	} {
		t.Run(test.name, func(t *testing.T) {
			parsed, err := ParseKnownHostsLine(test.text, "host.test", 22)
			if err == nil {
				t.Fatalf("accepted %q as %+v", test.text, parsed)
			}
			if got := code(t, err); got != "trust.identity" {
				t.Fatalf("code = %q", got)
			}
		})
	}
}

func TestAHostKeyIsAcceptedOnlyAsTheTypeItsBlobProves(t *testing.T) {
	trusted := key(t, 3)
	if _, err := ParseKnownHostsLine("host.test ecdsa-sha2-nistp256 "+trusted.PublicKey, "host.test", 22); err == nil {
		t.Fatal("a mislabeled key was accepted")
	}
	if _, err := ParseKnownHostsLine("host.test ssh-dss "+trusted.PublicKey, "host.test", 22); err == nil {
		t.Fatal("an unqualified key type was accepted")
	}
	if _, err := ParseKnownHostsLine("host.test "+trusted.Type+" not-base64!", "host.test", 22); err == nil {
		t.Fatal("a key that is not a bounded blob was accepted")
	}
}

func TestAnAuthorizedKeyLineIsTheShapeAnInstallationPublishes(t *testing.T) {
	trusted := key(t, 4)
	parsed, err := ParseAuthorizedKey(trusted.Type + " " + trusted.PublicKey + " root@rhel-01\n")
	if err != nil {
		t.Fatal(err)
	}
	if parsed != trusted {
		t.Fatalf("parsed = %+v, want %+v", parsed, trusted)
	}
	if _, err := ParseAuthorizedKey(trusted.Type); err == nil {
		t.Fatal("a type without a key was accepted")
	}
}

// A server holding an RSA key may sign with SHA-2 and may refuse the SHA-1
// form, so pinning the key type alone would refuse a key already trusted.
func TestPinnedAlgorithmsAdmitTheSignaturesAnRSAHostMayOffer(t *testing.T) {
	if got := key(t, 5).Algorithms(); got != "ssh-ed25519" {
		t.Fatalf("ed25519 algorithms = %q", got)
	}
	got := rsaKey().Algorithms()
	if !strings.HasPrefix(got, "rsa-sha2-512,rsa-sha2-256,") || !strings.HasSuffix(got, "ssh-rsa") {
		t.Fatalf("rsa algorithms = %q", got)
	}
}

// The fingerprint is what an operator compares against what they were told out
// of band, so it has to be the value OpenSSH itself prints. This vector is a
// real ed25519 key beside the `ssh-keygen -lf` output for it.
func TestAFingerprintIsTheValueOpenSSHPrints(t *testing.T) {
	parsed, err := ParseAuthorizedKey(
		"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAICG+RadYRVwHqO7OQsOpyh8SDf1tbTQ8PUmgn7UEmou/ no comment")
	if err != nil {
		t.Fatal(err)
	}
	const want = "SHA256:8oT3qNBeFE7en6Ce9uTOcz+5ihUnqv435ifHqGftBJA"
	if got := parsed.Fingerprint(); got != want {
		t.Fatalf("fingerprint = %q, want %q", got, want)
	}
}

func TestAFingerprintIdentifiesTheKeyAnOperatorConfirms(t *testing.T) {
	trusted := key(t, 6)
	fingerprint := trusted.Fingerprint()
	if !strings.HasPrefix(fingerprint, "SHA256:") {
		t.Fatalf("fingerprint = %q", fingerprint)
	}
	if other := key(t, 7).Fingerprint(); other == fingerprint {
		t.Fatal("two distinct keys share a fingerprint")
	}
	if (HostKey{}).Fingerprint() != "" || (HostKey{}).Line("host.test", 22) != "" {
		t.Fatal("an absent key presented a fingerprint or an entry")
	}
}
