package sshtrust

import (
	"strings"
	"testing"
)

// TestScanHostKeyCommandUsesFIPSHonoringSSH pins that host-key capture runs
// `ssh` with accept-new (which honors the controller's crypto policy) and never
// ssh-keyscan (which ignores it and returns nothing on a FIPS controller).
func TestScanHostKeyCommandUsesFIPSHonoringSSH(t *testing.T) {
	cmd := scanHostKeyCommand("10.0.0.5", 15, "/tmp/kh/known_hosts")
	if cmd.Name != "ssh" {
		t.Fatalf("command name = %q, want ssh", cmd.Name)
	}
	if cmd.Name == "ssh-keyscan" {
		t.Fatal("host-key capture must not shell out to ssh-keyscan (ignores FIPS crypto policy)")
	}
	joined := strings.Join(cmd.Args, " ")
	for _, want := range []string{
		"StrictHostKeyChecking=accept-new",
		"UserKnownHostsFile=/tmp/kh/known_hosts",
		"ConnectTimeout=15",
		"BatchMode=yes",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("ssh args %q missing %q", joined, want)
		}
	}
	if cmd.Args[len(cmd.Args)-1] != "true" {
		t.Errorf("last arg = %q, want the target host and a no-op command", cmd.Args[len(cmd.Args)-1])
	}
}
