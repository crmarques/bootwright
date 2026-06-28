package sshtrust

import (
	"testing"

	secret "github.com/crmarques/bootwright/internal/secrets"
)

// TestDirForSecretsPlaceholderModeOmitsTrustStore pins that the managed trust
// store (known_hosts / trust dir) has no portable form: in placeholder mode the
// derivations return "" so the context-free render omits them rather than
// leaking a path derived from the sentinel.
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
	// A real secrets dir is unaffected.
	if got := DirForSecrets("/ctx/secrets"); got == "" {
		t.Error("DirForSecrets(real) returned empty")
	}
}
