package providedhostname

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestProvidedHostnameRefusesARequestFromAnotherMachine(t *testing.T) {
	block := hostnameExecution(t).Block
	block.Object = "other"
	references, err := (Capability{}).SecretReferences(block)
	items := diagnostics.Of(err)
	if len(references) != 0 || len(items) != 1 || !strings.Contains(items[0].Message, "another block or Machine") {
		t.Fatalf("foreign request returned references %v and diagnostics %v", references, items)
	}
}
