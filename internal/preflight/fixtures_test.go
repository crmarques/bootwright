package preflight

import (
	"path/filepath"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/state/desired"
)

func fixturePath(name string) string {
	return filepath.Join("..", "..", "test", "e2e", name)
}

func loadFixtureState(t *testing.T, name string) v1alpha1.State {
	t.Helper()
	state, err := desiredstate.LoadNormalizeValidate([]string{fixturePath(name)})
	if err != nil {
		t.Fatalf("load fixture %s: %v", name, err)
	}
	return state
}
