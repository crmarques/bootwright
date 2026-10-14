package installation

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestKeyPolicyRefusalsNameEachBoundSecretAndType(t *testing.T) {
	request := Request{FleetKeyRef: "fleet", Target: Target{HostKeyRef: "installed-host", HostKeyType: "rsa"}}
	refusals := keyPolicyRefusals(request, map[string]string{"fleetKeyType": "ssh-ed25519", "hostKeyType": "ssh-rsa"})
	for reason, wanted := range map[string]string{
		"ssh-policy-fleet-excluded": "Secret/fleet key type ssh-ed25519",
		"ssh-policy-host-excluded":  "Secret/installed-host key type ssh-rsa",
	} {
		got := diagnostics.Of(refusals[reason])
		if len(got) != 1 || !strings.Contains(got[0].Message, wanted) || !strings.Contains(got[0].Message, "nothing was published or booted") {
			t.Fatalf("%s: %v", reason, got)
		}
	}
	if refusals["ssh-policy-unavailable"] == nil {
		t.Fatal("unavailable policy has no bounded refusal")
	}
}
