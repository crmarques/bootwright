package desiredstate

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

// Every authored kind must round-trip through Load: a kind added to State and
// the accessor table but missing a loadFile decode case fails here instead of
// silently dropping user YAML.
func TestLoadCoversEveryAuthoredKind(t *testing.T) {
	var docs []string
	for _, acc := range v1alpha1.AuthoredKindAccessors() {
		docs = append(docs, fmt.Sprintf(
			"apiVersion: %s\nkind: %s\nmetadata:\n  name: probe-%s\nspec: {}\n",
			v1alpha1.APIVersion, acc.Kind, strings.ToLower(acc.Kind)))
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "all-kinds.yaml")
	if err := os.WriteFile(file, []byte(strings.Join(docs, "---\n")), 0o600); err != nil {
		t.Fatalf("write probe file: %v", err)
	}
	state, err := Load([]string{file})
	if err != nil {
		t.Fatalf("load probe file: %v", err)
	}
	for _, acc := range v1alpha1.AuthoredKindAccessors() {
		want := "probe-" + strings.ToLower(acc.Kind)
		if !slices.Contains(acc.Names(state), want) {
			t.Errorf("kind %s did not load: names %v", acc.Kind, acc.Names(state))
		}
	}
}
