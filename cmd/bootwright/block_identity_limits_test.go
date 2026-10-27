//go:build linux && amd64

package main

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/substrate/baremetal"
	"github.com/crmarques/bootwright/internal/substrate/libvirt"
)

func TestEveryBlockAMachineOrProviderContributesIsAnIdentityAtAnyName(t *testing.T) {
	machine, sibling := strings.Repeat("m", 63), strings.Repeat("m", 62)+"n"
	for _, block := range []func(string) string{installation.BlockID, installation.EraseBlockID, libvirt.MachineBlockID, baremetal.BlockID, libvirt.HostBlockID} {
		got := block(machine)
		if !reconciliation.ValidSegment(got) || got == block(sibling) {
			t.Errorf("a 63-byte name yields %q, not a distinct block identity", got)
		}
	}
	if got, want := installation.BlockID(strings.Repeat("m", 52)), "os-install-"+strings.Repeat("m", 52); got != want {
		t.Errorf("the 63-byte identity %q moved to %q", want, got)
	}
}
