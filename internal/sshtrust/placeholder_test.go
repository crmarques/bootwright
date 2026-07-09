package sshtrust

import (
	"testing"

	secret "github.com/crmarques/bootwright/internal/secrets"
)

func TestDirForSecretsPlaceholderModeOmitsTrustStore(t *testing.T) {
	if got := DirForSecrets(secret.PlaceholderSecretsDir); got != "" {
		t.Errorf("DirForSecrets(sentinel) = %q, want \"\"", got)
	}
	if got := KnownHostsPathForSecrets(secret.PlaceholderSecretsDir); got != "" {
		t.Errorf("KnownHostsPathForSecrets(sentinel) = %q, want \"\"", got)
	}
	if got := StorePathForSecrets(secret.PlaceholderSecretsDir); got != "" {
		t.Errorf("StorePathForSecrets(sentinel) = %q, want \"\"", got)
	}
	if got := DirForSecrets("/ctx/secrets"); got == "" {
		t.Error("DirForSecrets(real) returned empty")
	}
}
