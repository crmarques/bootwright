//go:build linux && amd64

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
	"github.com/crmarques/bootwright/internal/secrets/localkeyring"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

// The same declarations imported from another directory, or with their
// documents reordered, keep every Secret available and re-mint nothing,
// because a fingerprint covers no provenance (D71).
func TestAnIdenticalReimportFromAnotherDirectoryKeepsSecretsAvailable(t *testing.T) {
	services, _, input, _ := contextFixture(t)
	documents := []string{
		secretDocument("token", "token", "  source: {generated: {}}\n"),
		secretDocument("ca", "caBundle", "  source: {generated: {commonName: fixture-ca, validityDays: 30}}\n"),
	}
	addSecretInput(t, input, "secret.yaml", strings.Join(documents, "\n---\n"))
	contextRun(t, services, 0, "context", "init", "--name", "alpha", "--input-dir", input)
	contextRun(t, services, 0, "secret", "encryption", "init")
	contextRun(t, services, 0, "secret", "generate")

	moved := filepath.Join(filepath.Dir(input), "moved")
	if err := os.Mkdir(moved, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"environment.yaml", "controller.yaml", "secret.yaml"} {
		data, err := os.ReadFile(filepath.Join(input, name))
		if err != nil {
			t.Fatal(err)
		}
		addSecretInput(t, moved, name, string(data))
	}
	contextRun(t, services, 0, "context", "update", "--name", "alpha", "--input-dir", moved, "--yes")
	contextRun(t, services, 0, "secret", "check")
	if generated, _ := contextRun(t, services, 0, "secret", "generate"); !strings.HasPrefix(generated, "[SKIPPED] Generated Secrets are current") || !strings.Contains(generated, "Unchanged  ca, token") {
		t.Fatalf("generate after a moved import re-minted material:\n%s", generated)
	}

	addSecretInput(t, moved, "secret.yaml", documents[1]+"\n---\n"+documents[0])
	contextRun(t, services, 0, "context", "update", "--name", "alpha", "--input-dir", moved, "--yes")
	contextRun(t, services, 0, "secret", "check")
	if generated, _ := contextRun(t, services, 0, "secret", "generate"); !strings.HasPrefix(generated, "[SKIPPED] Generated Secrets are current") {
		t.Fatalf("generate after reordered documents re-minted material:\n%s", generated)
	}
}

// A version an earlier build stored in the keyring under the fingerprint that
// covered its declaring path and document stays current, binds, and is not
// replaced by equal material.
func TestAKeyringVersionUnderTheLegacyFingerprintStaysCurrent(t *testing.T) {
	services, repository, input, root := contextFixture(t)
	path := addSecretInput(t, input, "secret.yaml", secretDocument("legacy", "opaque", ""))
	contextRun(t, services, 0, "context", "init", "--name", "alpha", "--input-dir", input)
	contextRun(t, services, 0, "secret", "encryption", "init")
	ctx := context.Background()
	snapshot, err := repository.SecretContext(ctx, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	legacy := secrets.Declaration{Name: "legacy", Type: "opaque", Source: "contextStore", Origin: path, Document: 1}
	canonical, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(canonical)
	legacy.Fingerprint = hex.EncodeToString(digest[:])
	material := secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("synthetic-legacy-value")})
	defer material.Clear()
	access := secretstore.NewAccess(repository, secretstore.NewCatalog(localkeyring.New()), nil)
	if err := access.Mutate(ctx, snapshot.Context, func(session secretstore.StoreSession, _ secretstore.Selection) error {
		_, err := session.PutBatch(ctx, []secretstore.Put{{Declaration: legacy, Material: material}})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	contextRun(t, services, 0, "secret", "check")
	listed := secretResult(t, services, 0, "secret", "list")
	if !strings.Contains(string(listed["secrets"]), `"state":"current"`) {
		t.Fatalf("the legacy version is not listed as current: %s", listed["secrets"])
	}
	if _, err := custodyOf(t, services).Bind(ctx, custody.BindRequest{ContextName: "alpha", Names: []string{"legacy"}}); err != nil {
		t.Fatalf("the keyring refused to bind the legacy version: %v", err)
	}
	before := stateFingerprint(t, root)
	value := addSecretInput(t, t.TempDir(), "value", "synthetic-legacy-value")
	if set, _ := contextRun(t, services, 0, "secret", "set", "--name", "legacy", "--value-file", value, "--yes"); !strings.Contains(set, "Changed    0") {
		t.Fatalf("equal material replaced the legacy version:\n%s", set)
	}
	if !sameFingerprints(before, stateFingerprint(t, root)) {
		t.Fatal("an unchanged set wrote the store")
	}
}
