//go:build linux && amd64

package main

import (
	"testing"

	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/substrate/vsphere"
)

// Exercise both installer choices through the same production capability registry.
// Aggregate example goldens can legitimately record a refusal for an unfinished
// scenario. This standalone example must produce both Machine and OS blocks.
func TestVSphereStandaloneExamplePlansBothInstallers(t *testing.T) {
	const name = "lab-vsphere-standalone"
	state, _ := compileAcceptance(t, exampleDirectory(t, name))
	planned := make(map[string]bool)
	for _, block := range productionDefinitions(t, state, name) {
		switch block.ID {
		case "machine-rhel-iso", "machine-rhel-clone":
			if block.Implementation != vsphere.Implementation {
				t.Fatalf("%s selected %s", block.ID, block.Implementation)
			}
			r, err := vsphere.DecodeRequest(block.Request)
			if err != nil || !r.TLSVerify || (r.Template != "") != (block.Object == "rhel-clone") {
				t.Fatalf("%s did not freeze the selected provisioning path: %v", block.ID, err)
			}
		case "os-install-rhel-iso", "os-install-rhel-clone":
			r, err := installation.DecodeRequest(block.Request)
			if err != nil || (r.CloudInit != nil) != (block.Object == "rhel-clone") {
				t.Fatalf("%s did not freeze the selected installation path: %v", block.ID, err)
			}
			if r.CloudInit == nil && (r.Private == nil || r.Image != nil) {
				t.Fatalf("%s needs private installer media", block.ID)
			}
		default:
			continue
		}
		planned[block.ID] = true
	}
	if len(planned) != 4 {
		t.Fatalf("expected two Machine and two OS blocks, got %v", planned)
	}
}
