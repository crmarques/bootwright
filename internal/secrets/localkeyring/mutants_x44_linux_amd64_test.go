//go:build linux && amd64

package localkeyring

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

// Initialization refuses a store root holding content it cannot attribute to
// an earlier initialization, a stray file beside no marker, and writes no
// marker over it (X33's mutants, B255).
func TestInitializationRefusesARootItCannotAttribute(t *testing.T) {
	h := newUninitializedIntegrationStore(t)
	secretsRoot := filepath.Join(h.root, "contexts", "example", "secrets")
	if err := os.WriteFile(filepath.Join(secretsRoot, "stray"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	err := h.workspace.MutateSecrets(context.Background(), h.context, func(area secretstore.Area) error {
		session, err := h.implementation.Initialize(context.Background(), h.context, area, nil)
		if session != nil {
			session.Close()
		}
		return err
	})
	found := diagnostics.Of(err)
	if len(found) != 1 || found[0].Message != "nonempty secret store has no attributable initialization state" {
		t.Fatalf("initialization over an unattributable root = %+v", found)
	}
	if _, statErr := os.Stat(filepath.Join(secretsRoot, initializationPath)); !os.IsNotExist(statErr) {
		t.Fatalf("initialization wrote its marker over the root it refused (%v)", statErr)
	}
}
