package installation

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/substrate"
)

func TestKeyPolicyRefusalsNameEachBoundSecretAndType(t *testing.T) {
	request := Request{FleetKeyRef: "fleet", Target: Target{Channel: substrate.ChannelDeliveredKey, HostKeyRef: "installed-host", HostKeyType: "rsa"}}
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
	if refusals["ssh-policy-generated-excluded"] != nil {
		t.Fatal("a delivered-key installation names a generated host key refusal")
	}
	guest := Request{FleetKeyRef: "fleet", Identity: Identity{Object: "rhel-01", Profile: "rhel-9-8"}, Target: Target{Channel: substrate.ChannelGuestAgent, HostKeyType: "ecdsaP256"}}
	got := diagnostics.Of(keyPolicyRefusals(guest, map[string]string{"fleetKeyType": "ecdsa-sha2-nistp256", "generatedHostKeyType": "ecdsa-sha2-nistp256"})["ssh-policy-generated-excluded"])
	if len(got) != 1 || !strings.Contains(got[0].Message, "the ecdsa-sha2-nistp256 host key the installation of Machine/rhel-01 generates") ||
		got[0].Remediation != "run from a controller whose SSH policy admits ecdsa-sha2-nistp256" {
		t.Fatalf("generated: %v", got)
	}
}
