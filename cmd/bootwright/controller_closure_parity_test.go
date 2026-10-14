package main

import (
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/substrate/libvirt"
)

// The provider host installs the closure the controller stage asks the host's
// dnf for when the host is the controller, so the two lists, written once in
// each package, must name the same packages in the same order.
func TestAProviderHostInstallsTheStagesHypervisorClosure(t *testing.T) {
	stage := prerequisites.StageHostRequirements(prerequisites.NativeRequirements{LibvirtClient: true, Hypervisor: true})
	if !slices.Equal(libvirt.HypervisorPackages(), stage) {
		t.Errorf("a provider host installs %v, the controller stage installs %v", libvirt.HypervisorPackages(), stage)
	}
}
