package material

import (
	"bytes"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// The password file is one bcrypt line Distribution's own verifier accepts.
// Its salt is random, so nothing in it can be computed from the password and
// the registry's name alone: two derivations for the same name and password,
// as two contexts would make, differ.
func TestThePasswordFileIsARandomlySaltedBcryptLine(t *testing.T) {
	first, err := BcryptPasswordFile([]byte("mirror"), []byte("hunter2"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := BcryptPasswordFile([]byte("mirror"), []byte("hunter2"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first, second) || bytes.Equal(first[:29], second[:29]) {
		t.Fatal("two derivations for one name and password share a salt")
	}
	for _, file := range [][]byte{first, second} {
		user, hash, found := bytes.Cut(bytes.TrimSuffix(file, []byte("\n")), []byte(":"))
		if !found || string(user) != "mirror" || !bytes.HasPrefix(hash, []byte("$2a$12$")) || bcrypt.CompareHashAndPassword(hash, []byte("hunter2")) != nil ||
			bcrypt.CompareHashAndPassword(hash, []byte("hunter3")) == nil || bytes.Contains(file, []byte("hunter2")) {
			t.Fatalf("the password file %q is not a bcrypt line for the credential", file)
		}
	}
}

func TestThePasswordFileRefusesACredentialNoLineCanCarry(t *testing.T) {
	for name, credential := range map[string][2]string{
		"no user": {"", "secret"}, "colon": {"mir:ror", "secret"}, "non-ASCII": {"josé", "secret"},
		"no password": {"mirror", ""}, "beyond bcrypt": {"mirror", strings.Repeat("p", 73)}, "NUL": {"mirror", "sec\x00ret"},
	} {
		if file, err := BcryptPasswordFile([]byte(credential[0]), []byte(credential[1])); err == nil || file != nil {
			t.Errorf("%s: derived %q", name, file)
		}
	}
}
