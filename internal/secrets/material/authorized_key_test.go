package material

import (
	"crypto/ed25519"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/secrets"
	"golang.org/x/crypto/ssh"
)

func authorizedKeyBase(t *testing.T) string {
	t.Helper()
	public, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSuffix(string(ssh.MarshalAuthorizedKey(key)), "\n")
}

func TestAuthorizedKeyRefusesOptionsAndUnsafeComments(t *testing.T) {
	base := authorizedKeyBase(t)
	for name, line := range map[string]string{
		"unquoted option":             "restrict " + base,
		"comment with a backslash":    base + ` back\slash`,
		"comment with a control":      base + " bell\x07comment",
		"comment with a double quote": base + ` say"hi`,
	} {
		t.Run(name, func(t *testing.T) {
			material := secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(line)})
			got, err := AuthorizedKey(material, "admin-key")
			if err == nil || got != "" {
				t.Fatalf("AuthorizedKey(%q) = %q, %v", line, got, err)
			}
			found := diagnostics.Of(err)
			if len(found) != 1 || found[0].Code != "secret.part" || found[0].Object == nil || found[0].Object.Kind != "Secret" || found[0].Object.Name != "admin-key" || !strings.Contains(found[0].Remediation, "Secret/admin-key") {
				t.Fatalf("refusal = %#v", found)
			}
		})
	}
}

func TestAuthorizedKeyAcceptsPublicPartAndOpaqueLine(t *testing.T) {
	base := authorizedKeyBase(t)
	pair := secrets.NewMaterial(map[secrets.Part][]byte{secrets.PublicKeyPart: []byte(base + "\n"), secrets.PrivateKeyPart: []byte("private")})
	got, err := AuthorizedKey(pair, "admin-key")
	if err != nil || got != base {
		t.Fatalf("sshKeyPair public part = %q, %v", got, err)
	}
	opaque := secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(base + " admin@example.test\n")})
	got, err = AuthorizedKey(opaque, "admin-key")
	if err != nil || got != base+" admin@example.test" {
		t.Fatalf("opaque key = %q, %v", got, err)
	}
}
