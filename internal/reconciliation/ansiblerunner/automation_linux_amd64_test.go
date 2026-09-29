//go:build linux && amd64

package ansiblerunner

import (
	"maps"
	"testing"

	"github.com/crmarques/bootwright/ansible"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// Documentation leaves the automation digest, so the runner compares only what
// the digest covers: a bundle whose README is an older one, or lacks it, still
// carries this executable's automation, and one whose module differs does not.
func TestTheRunnerComparesOnlyTheDigestedAutomation(t *testing.T) {
	const (
		readme = "collections/ansible_collections/bootwright/core/README.md"
		module = "collections/ansible_collections/bootwright/core/plugins/modules/artifact_server_protocol.py"
	)
	if _, ok := ansible.Automation()[module]; !ok {
		t.Fatalf("the fixture module %s is not embedded automation", module)
	}
	for name, change := range map[string]func(map[string][]byte){
		"older README":  func(files map[string][]byte) { files[readme] = []byte("# An older release\n") },
		"absent README": func(files map[string][]byte) { delete(files, readme) },
	} {
		t.Run(name, func(t *testing.T) {
			files := maps.Clone(ansible.Assets())
			change(files)
			if err := verifyAutomation(t.Context(), lifecycle.RunRequest{Area: embeddedArea{files: files}}); err != nil {
				t.Fatalf("a bundle whose documentation differs was refused: %v", err)
			}
		})
	}
	files := maps.Clone(ansible.Assets())
	files[module] = append([]byte("# drifted\n"), files[module]...)
	err := verifyAutomation(t.Context(), lifecycle.RunRequest{Area: embeddedArea{files: files}})
	if code, _ := codeOf(err); code != "controller.identity" {
		t.Fatalf("a bundle whose module differs = %q (%v)", code, err)
	}
}
