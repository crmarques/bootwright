package machine

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/secrets"
)

func TestLifecyclePlacementProvesTheCompleteSSHKeyForItsOwnEndpoint(t *testing.T) {
	const key = "AAAAC3NzaC1lZDI1NTE5AAAAINdamAGCsQq31Uv+08lkBzoO4XLz2qYjJa8CGmj3B1Ea"
	blob, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		t.Fatal(err)
	}
	placement := Placement{Connection: ConnectionSSH, Machine: "relay", Address: "relay.example.test", Port: 2222,
		User: "maint", Privilege: "sudo", PrivateKeyRef: "login", KnownHostsRef: "host-pin"}
	for _, test := range []struct {
		name, line string
		valid      bool
	}{
		{"correct", "[relay.example.test]:2222 ssh-ed25519 " + key, true},
		{"wrong port", "relay.example.test ssh-ed25519 " + key, false},
		{"public key only", "ssh-ed25519 " + key, false},
		{"truncated key", "[relay.example.test]:2222 ssh-ed25519 " + base64.StdEncoding.EncodeToString(blob[:len(blob)-1]), false},
		{"two entries", "[relay.example.test]:2222 ssh-ed25519 " + key + "\n[relay.example.test]:2222 ssh-ed25519 " + key, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			material := map[string]secrets.Material{"host-pin": secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(test.line)})}
			err := placement.ProveHostPin("demo", material)
			if test.valid {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			found := diagnostics.Of(err)
			if len(found) != 1 || found[0].Code != "trust.identity" || !strings.Contains(found[0].Message, "[relay.example.test]:2222") ||
				!strings.Contains(found[0].Message, "Secret/host-pin") || !strings.Contains(found[0].Remediation, "--referenced-secret") {
				t.Fatalf("pin proof = %+v (%v)", found, err)
			}
		})
	}
}
