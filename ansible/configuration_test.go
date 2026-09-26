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
