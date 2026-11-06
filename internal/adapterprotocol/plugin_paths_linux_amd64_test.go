//go:build linux && amd64

package adapterprotocol

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestComposePinsEveryLegacyPluginPath(t *testing.T) {
	command := compose(Invocation{Program: "/usr/bin/python3", Automation: "/approved/automation", Command: exec.Command})
	environment := map[string]string{}
	for _, entry := range command.Env {
		key, value, _ := strings.Cut(entry, "=")
		environment[key] = value
	}
	for _, kind := range []string{"ACTION", "BECOME", "CACHE", "CALLBACK", "CLICONF", "CONNECTION", "DOC_FRAGMENT", "FILTER", "HTTPAPI", "INVENTORY", "LOOKUP", "NETCONF", "STRATEGY", "TERMINAL", "TEST", "VARS"} {
		key := "ANSIBLE_" + kind + "_PLUGINS"
		if value := environment[key]; value == "" || len(filepath.SplitList(value)) != 1 || !strings.HasPrefix(value, "/approved/automation/collections/ansible_collections/bootwright/core/plugins/") {
			t.Errorf("%s = %q leaves a legacy plugin path unpinned", key, value)
		}
	}
	for _, key := range []string{"ANSIBLE_LIBRARY", "ANSIBLE_MODULE_UTILS"} {
		if value := environment[key]; value == "" || strings.Contains(value, "/usr/share/ansible") {
			t.Errorf("%s = %q leaves an ambient import path", key, value)
		}
	}
}
