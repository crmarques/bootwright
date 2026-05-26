package locality

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestCheckControllerUsesLocalhost(t *testing.T) {
	result := CheckController(v1alpha1.State{}, Policy{})
	if !result.OK {
		t.Fatalf("CheckController failed: %s", result.Evidence)
	}
	if !strings.Contains(result.Evidence, "localhost") {
		t.Fatalf("evidence = %q, want localhost execution evidence", result.Evidence)
	}
}
