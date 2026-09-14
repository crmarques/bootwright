package ansible

import (
	"strings"
	"testing"
)

// An empty value is not an empty list. ansible-core parses a bare "key =" as
// one empty element, so "callbacks_enabled =" loads a callback plugin whose
// name is "", and every playbook aborts before its first task. Omit the key to
// mean none.
func TestConfigurationDeclaresNoEmptyValue(t *testing.T) {
	data, ok := Assets()["ansible.cfg"]
	if !ok {
		t.Fatal("the embedded automation carries no ansible.cfg")
	}
	for number, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") || strings.HasPrefix(trimmed, "[") {
			continue
		}
		key, value, found := strings.Cut(trimmed, "=")
		if !found {
			t.Fatalf("ansible.cfg line %d is neither a section nor an assignment: %q", number+1, trimmed)
		}
		if strings.TrimSpace(value) == "" {
			t.Fatalf("ansible.cfg line %d assigns %q an empty value; omit the key instead", number+1, strings.TrimSpace(key))
		}
	}
}
