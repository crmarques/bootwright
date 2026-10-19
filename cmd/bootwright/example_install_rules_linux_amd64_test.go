package main

import (
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func TestTheReferenceExamplesInstallUnderTheNewRules(t *testing.T) {
	for _, name := range []string{"lab-rhel", "lab-rhel-3-nodes", "libvirt-3-nodes-ceph", "libvirt-network-variants", "lab-baremetal"} {
		t.Run(name, func(t *testing.T) {
			catalog := compiledEffective(t, name)
			if len(installation.InstalledMachines(catalog)) == 0 {
				t.Fatal("the example installs no Machine")
			}
			if refused := installation.Refusals(catalog); len(refused) != 0 {
				t.Fatalf("refusals = %+v, want none", refused)
			}
		})
	}
	t.Run("multidc-platform meets the FIPS on a physical Machine row", func(t *testing.T) {
		state, _ := compileAcceptance(t, admissibleExampleSources(t, false))
		var want []lifecycle.Refusal
		for _, name := range []string{"storage-a-01", "storage-a-02", "storage-a-03", "storage-b-01", "storage-b-02", "storage-b-03"} {
			want = append(want, lifecycle.Refusal{Kind: "Machine", Name: name,
				Reason:      "the FIPS profile arm is qualified only on virtual Machines, and Machine/" + name + " is physical; physical qualification is B78",
				Remediation: "install Machine/" + name + " with a profile that does not enable spec.customizations.security.fips, or disable spec.customizations.security.fips on MachineInstallProfile/rhel-ceph-node"})
		}
		if refused := installation.Refusals(state.Effective()); !slices.Equal(refused, want) {
			t.Fatalf("refusals = %+v, want %+v", refused, want)
		}
	})
}
