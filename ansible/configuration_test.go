package ansible

import (
	"slices"
	"strings"
	"testing"
)

// A connection that joins a control master reuses an authenticated channel
// another process opened to the same host and account, under that process's
// options rather than the pinned host key and identity this invocation's
// inventory carries. ansible-core adds a shared default ControlPath of its own
// whenever the arguments name ControlPersist, so turning the master off is not
// enough: the path must be disabled too.
func TestSSHConnectionsShareNoControlConnection(t *testing.T) {
	arguments, ok := configured(t, "ssh_connection", "ssh_args")
	if !ok {
		t.Fatal("ansible.cfg sets no ssh_args, so ansible-core's shared ControlMaster default applies")
	}
	fields := strings.Fields(arguments)
	for _, option := range []string{"ControlMaster=no", "ControlPath=none", "ControlPersist=no"} {
		index := slices.Index(fields, option)
		if index < 1 || fields[index-1] != "-o" {
			t.Fatalf("ssh_args %q does not pass -o %s", arguments, option)
		}
	}
}

// ansible-core only warns by default when it loads a collection whose
// requires_ansible excludes it. The collection is qualified for one minor, so
// running it under another is refused instead. That the pinned ansible-core
// loads the collection under this setting is proved by
// ./scripts/ansible-check --suite syntax, not here.
func TestRequiresAnsibleMismatchIsAnError(t *testing.T) {
	if value, ok := configured(t, "defaults", "collections_on_ansible_version_mismatch"); !ok || value != "error" {
		t.Fatalf("ansible.cfg sets collections_on_ansible_version_mismatch to %q, so an unqualified ansible-core only warns", value)
	}
}

// ansible.builtin.default prints the error, warnings and deprecations a no_log
// task raised, whatever value they name, so the adapter prints through the
// collection's censoring callback and no other. The collection's
// test_censored_output.py runs each shape under both callbacks.
func TestTheAdapterOutputPrintsNothingANoLogTaskRaised(t *testing.T) {
	if value, ok := configured(t, "defaults", "stdout_callback"); !ok || value != "bootwright.core.censored" {
		t.Fatalf("ansible.cfg names the stdout callback %q, which prints what a no_log task raised", value)
	}
	if _, ok := Automation()["collections/ansible_collections/bootwright/core/plugins/callback/censored.py"]; !ok {
		t.Fatal("the embedded automation carries no bootwright.core.censored callback")
	}
	for _, key := range []string{"callbacks_enabled", "callback_whitelist"} {
		if value, ok := configured(t, "defaults", key); ok {
			t.Fatalf("ansible.cfg enables the callbacks %q beside the censoring one", value)
		}
	}
}

// configured reads one key from one section of the embedded ansible.cfg.
func configured(t *testing.T, section, key string) (string, bool) {
	t.Helper()
	data, ok := Assets()["ansible.cfg"]
	if !ok {
		t.Fatal("the embedded automation carries no ansible.cfg")
	}
	current := ""
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if name, found := strings.CutPrefix(trimmed, "["); found {
			current = strings.TrimSuffix(name, "]")
			continue
		}
		name, value, found := strings.Cut(trimmed, "=")
		if found && current == section && strings.TrimSpace(name) == key {
			return strings.TrimSpace(value), true
		}
	}
	return "", false
}
