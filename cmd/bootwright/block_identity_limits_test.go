//go:build linux && amd64

package main

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/substrate"
	"github.com/crmarques/bootwright/internal/substrate/baremetal"
	"github.com/crmarques/bootwright/internal/substrate/libvirt"
)

// The name limits admission states are the longest names whose blocks are
// still block identities: every block a Machine or a provider at its limit
// contributes is one, and the longest is not one byte past it.
func TestNameLimitsAreTheBlockIdentitiesTheyContribute(t *testing.T) {
	machine := strings.Repeat("m", substrate.MachineNameLimit)
	for _, block := range []string{installation.BlockID(machine), libvirt.MachineBlockID(machine), baremetal.BlockID(machine)} {
		if !reconciliation.ValidSegment(block) {
			t.Errorf("the block %q of a Machine name at the limit is not a block identity", block)
		}
	}
	if block := installation.BlockID(machine + "m"); reconciliation.ValidSegment(block) {
		t.Errorf("the block %q of a Machine name past the limit is still a block identity, so the limit is too low", block)
	}
	provider := strings.Repeat("p", substrate.ProviderNameLimit)
	if block := libvirt.HostBlockID(provider); !reconciliation.ValidSegment(block) {
		t.Errorf("the host block %q of a provider name at the limit is not a block identity", block)
	}
	if block := libvirt.HostBlockID(provider + "p"); reconciliation.ValidSegment(block) {
		t.Errorf("the host block %q of a provider name past the limit is still a block identity, so the limit is too low", block)
	}
}
