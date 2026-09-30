//go:build linux && amd64

package main

import (
	"encoding/json"
	"maps"
	"path/filepath"
	"testing"

	"github.com/crmarques/bootwright/internal/secrets/custody"
)

// secret list names each current version by the ordinal the local keyring
// stored for it, counted per secret (specs/secrets.md, specs/cli/output.md).
func TestSecretListGoldenNamesTheKeyringOrdinals(t *testing.T) {
	services, _, input, _ := contextFixture(t)
	addSecretInput(t, input, "secret.yaml", secretDocument("credential", "opaque", "")+"\n---\n"+secretDocument("other", "opaque", ""))
	contextRun(t, services, 0, "context", "init", "--name", "alpha", "--input-dir", input)
	value := addSecretInput(t, t.TempDir(), "value", "synthetic-first-canary")
	contextRun(t, services, 0, "secret", "set", "--name", "credential", "--value-file", value)
	contextRun(t, services, 0, "secret", "set", "--name", "other", "--value-file", value)
	addSecretInput(t, filepath.Dir(value), filepath.Base(value), "synthetic-second-canary")
	contextRun(t, services, 0, "secret", "set", "--name", "credential", "--value-file", value, "--yes")
	out, stderr := contextRun(t, services, 0, "secret", "list")
	if stderr != "" {
		t.Fatalf("secret list wrote stderr: %s", stderr)
	}
	matchesTextGolden(t, "secret-list-two-versions", []byte(out))
	var rows []custody.ListRow
	if err := json.Unmarshal(secretResult(t, services, 0, "secret", "list")["secrets"], &rows); err != nil {
		t.Fatal(err)
	}
	sequences := map[string]int{}
	for _, row := range rows {
		sequences[row.Name] = row.CurrentSequence
	}
	if !maps.Equal(sequences, map[string]int{"credential": 2, "other": 1}) {
		t.Fatalf("listed ordinals: %v", sequences)
	}
}
