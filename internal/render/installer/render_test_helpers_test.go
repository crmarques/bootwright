package installer_test

import (
	"testing"

	secretstore "github.com/crmarques/bootwright/internal/secrets"
)

const fixtureRoot = "../../state/desired/testdata/good"

func writeEncryptedContextSecret(t *testing.T, dir, name string, role secretstore.MaterialRole, data []byte) {
	t.Helper()
	store := secretstore.NewContextStore("test", dir)
	if err := store.Write(secretstore.MaterialKey{Name: name, Role: role}, data); err != nil {
		t.Fatalf("write encrypted %s/%s: %v", name, role, err)
	}
}
