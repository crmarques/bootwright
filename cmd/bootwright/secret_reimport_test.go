//go:build linux && amd64

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	if generated, _ := contextRun(t, services, 0, "secret", "generate"); !strings.HasPrefix(generated, "  ○ Generated Secrets are current") || !strings.Contains(generated, "Unchanged  ca, token") {
		t.Fatalf("generate after a moved import re-minted material:\n%s", generated)
	}

	addSecretInput(t, moved, "secret.yaml", documents[1]+"\n---\n"+documents[0])
	contextRun(t, services, 0, "context", "update", "--name", "alpha", "--input-dir", moved, "--yes")
	contextRun(t, services, 0, "secret", "check")
	if generated, _ := contextRun(t, services, 0, "secret", "generate"); !strings.HasPrefix(generated, "  ○ Generated Secrets are current") {
		t.Fatalf("generate after reordered documents re-minted material:\n%s", generated)
	}
}
