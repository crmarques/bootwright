package main

import (
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const subscriptionRowReason = "the install profile selects spec.subscription, which carries secret bytes or effects this executable does not prove"

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
	t.Run("multidc-platform meets the subscription row until x60-prestop-removal lifts it", func(t *testing.T) {
		state, _ := compileAcceptance(t, admissibleExampleSources(t, false))
		var want []lifecycle.Refusal
		for _, name := range []string{"storage-a-01", "storage-a-02", "storage-a-03", "storage-b-01", "storage-b-02", "storage-b-03"} {
			want = append(want, lifecycle.Refusal{Kind: "Machine", Name: name, Reason: subscriptionRowReason,
				Remediation: "remove spec.subscription from MachineInstallProfile/rhel-ceph-node"})
		}
		if refused := installation.Refusals(state.Effective()); !slices.Equal(refused, want) {
			t.Fatalf("refusals = %+v, want %+v", refused, want)
		}
	})
}
