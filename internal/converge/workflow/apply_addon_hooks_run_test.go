package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteHookInventoryPinsHostKeysAndKeepsConnectionsAlive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inventory.yaml")
	targets := []hookSSHTarget{{
		label:          "Machine/bastion",
		inventoryName:  "hook_0",
		address:        "bastion.example.test",
		user:           "admin",
		keyPath:        "/runs/connection-secrets/bastion-ssh",
		knownHostsPath: "/context/trust/ssh/known_hosts",
	}}
	if err := writeHookInventory(path, targets, "", ""); err != nil {
		t.Fatalf("writeHookInventory: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read hook inventory: %v", err)
	}
	rendered := string(data)
	for _, want := range []string{
		"BatchMode=yes",
		"StrictHostKeyChecking=yes",
		"UserKnownHostsFile=/context/trust/ssh/known_hosts",
		"ServerAliveInterval=15",
		"ServerAliveCountMax=3",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("hook inventory missing %q; the inventory variable fully replaces ansible.cfg ssh_common_args:\n%s", want, rendered)
		}
	}
	if strings.Contains(rendered, "accept-new") {
		t.Fatalf("hook inventory must not downgrade host-key verification to accept-new:\n%s", rendered)
	}
}

func TestHookInventoryScopesTheSSHUserOverrideToOperatorIdentity(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "inventory.yaml")
	targets := []hookSSHTarget{
		{label: "bastion", inventoryName: "hook_0", address: "bastion.example.test", user: "admin"},
		{label: "lab", inventoryName: "hook_1", address: "lab.example.test", user: "admin", operatorIdentity: true},
	}
	if err := writeHookInventory(path, targets, "", "operator"); err != nil {
		t.Fatalf("writeHookInventory: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read hook inventory: %v", err)
	}
	body := string(data)
	if strings.Count(body, "ansible_user: admin") != 1 {
		t.Fatalf("a login named by desired state must survive --ssh-user: %s", body)
	}
	if strings.Count(body, "ansible_user: operator") != 1 {
		t.Fatalf("only the operatorIdentity target takes the override: %s", body)
	}
}
