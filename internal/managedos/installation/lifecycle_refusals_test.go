package installation

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestForeignInstallationRefusalNamesTheMachineAndSafeRecovery(t *testing.T) {
	request := Request{Identity: Identity{Object: "node-a"}}
	found := diagnostics.Of(foreignInstallation("example", request))
	if len(found) != 1 || found[0].Code != "lifecycle.state" || found[0].Object == nil || found[0].Object.Name != "node-a" {
		t.Fatalf("refusal = %#v", found)
	}
	for _, text := range []string{"another installation", "no installer was booted"} {
		if !strings.Contains(found[0].Message, text) {
			t.Fatalf("message = %s", found[0].Message)
		}
	}
	if !strings.Contains(found[0].Remediation, "does not replace another installation in place") || !strings.Contains(found[0].Remediation, "bootwright context update --name example") {
		t.Fatalf("remedy = %s", found[0].Remediation)
	}
}
