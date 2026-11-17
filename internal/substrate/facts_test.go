package substrate

import (
	"maps"
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestEveryArmAnswersTheFactsConsumersAsk(t *testing.T) {
	if got, want := slices.Sorted(maps.Keys(armFacts)), slices.Sorted(slices.Values(Arms())); !slices.Equal(got, want) {
		t.Fatalf("arm facts cover %q, want every declared arm %q", got, want)
	}
	for _, arm := range Arms() {
		if got, want := DeliveryOf(arm) == DeliveryPush, arm == ArmKubeVirt; got != want {
			t.Errorf("%s pushes its image = %t, want %t", arm, got, want)
		}
		if got, want := ControllerCredentialIsAccess(arm), arm == ArmBaremetal || arm == ArmKubeVirt; got != want {
			t.Errorf("%s controller credential is access = %t, want %t", arm, got, want)
		}
	}
	if DeliveryOf("") != DeliveryFetch || ControllerCredentialIsAccess("") || BridgedGuests("") {
		t.Fatal("an unknown arm answers a fact")
	}
}

func TestASyntheticArmAnswersThroughItsFactsRowAlone(t *testing.T) {
	const synthetic = "synthetic"
	t.Cleanup(func() { delete(armFacts, synthetic) })
	armFacts[synthetic] = facts{delivery: DeliveryPush, accessCredential: true, bridgedGuests: true}
	if DeliveryOf(synthetic) != DeliveryPush || !ControllerCredentialIsAccess(synthetic) || !BridgedGuests(synthetic) {
		t.Fatal("a synthetic arm's facts row does not answer its consumers")
	}
	fanout, allowance, margin := ClusterBounds(synthetic, ArmBaremetal)
	baremetal, _ := MachineBoundsFor(ArmBaremetal)
	if fanout != baremetal.Fanout || allowance != baremetal.BootAllowance || margin != baremetal.NodeMargin {
		t.Fatalf("an arm with no machine port bounds moved the cluster bounds to %d, %s, %s", fanout, allowance, margin)
	}
}

func TestOnlyAProviderOfAnArmWithAHypervisorNeedsItsClient(t *testing.T) {
	for _, arm := range Arms() {
		provider := obj(api.InfraProvider, "p", m(arm, m()))
		if got, want := NeedsHypervisorClient(provider), arm == ArmLibvirt; got != want {
			t.Errorf("%s needs the hypervisor client = %t, want %t", arm, got, want)
		}
	}
	both := obj(api.InfraProvider, "p", m(ArmLibvirt, m(), ArmVSphere, m()))
	if !NeedsHypervisorClient(both) || NeedsHypervisorClient(obj(api.InfraProvider, "p", m())) {
		t.Error("the answer follows the declared arms, not the selected variant")
	}
}
