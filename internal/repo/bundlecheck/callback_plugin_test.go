package bundlecheck

import (
	"strings"
	"testing"
)

func TestProfilingCallbackStaysOptIn(t *testing.T) {
	source := readRepoFile(t, "ansible/collections/ansible_collections/bootwright/core/plugins/callback/bw_profile.py")
	for _, want := range []string{
		"CALLBACK_NEEDS_ENABLED = True",
		`CALLBACK_TYPE = "aggregate"`,
		`CALLBACK_NAME = "bootwright.core.bw_profile"`,
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("bw_profile.py must declare %s so it never loads unless callbacks_enabled names it", want)
		}
	}
	if strings.Contains(source, `CALLBACK_TYPE = "stdout"`) {
		t.Fatal("bw_profile.py must never become a stdout callback")
	}
}

func TestNoStdoutCallbackIsConfigured(t *testing.T) {
	config := readRepoFile(t, "ansible/ansible.cfg")
	for _, line := range strings.Split(config, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "stdout_callback") || strings.HasPrefix(trimmed, "callbacks_enabled") {
			t.Fatalf("ansible.cfg must leave callback selection to the runner: %s", trimmed)
		}
	}
}
