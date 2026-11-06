package main

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/substrate"
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
	t.Run("multidc-platform refuses incompatible delivered FIPS host keys", func(t *testing.T) {
		state, _ := compileAcceptance(t, admissibleExampleSources(t, false))
		refused := installation.Refusals(state.Effective())
		for _, row := range refused {
			machine, found := state.Effective().Find(api.Machine, row.Name)
			if !found {
				t.Fatal("unknown refused machine")
			}
			reference := machine.Spec().Get("os", "install", "hostKeyRef").Text()
			if row.Reason != "a FIPS-mode installation refuses Ed25519 keys, and the delivered host key Secret/"+reference+" generates one" {
				t.Fatalf("unexpected refusal: %+v", row)
			}
		}
		if len(refused) != 6 {
			t.Fatalf("expected six incompatible generated keys, got %d", len(refused))
		}
	})
}

func TestTheMultidcKubeVirtProvidersPassTheRefusalsBeforeRegistration(t *testing.T) {
	state, _ := compileAcceptance(t, admissibleExampleSources(t, false))
	catalog := state.Effective()
	providers := substrate.ProvidersOn(catalog, substrate.ArmKubeVirt)
	if len(providers) != 2 {
		t.Fatalf("the reference declares %d KubeVirt providers, want two", len(providers))
	}
	owned := map[string]bool{}
	for _, provider := range providers {
		owned[string(api.InfraProvider)+"/"+provider.Name()] = true
		hosted := substrate.HostedMachines(catalog, provider.Name())
		if len(hosted) == 0 {
			t.Fatalf("%s hosts no Machine", provider.Identity())
		}
		for _, machine := range hosted {
			owned[string(api.Machine)+"/"+machine.Name()] = true
		}
	}
	for _, refusal := range productionUnsupported(t, state) {
		if owned[refusal.Kind+"/"+refusal.Name] {
			t.Errorf("%s/%s is refused before registration: %s", refusal.Kind, refusal.Name, refusal.Reason)
		}
	}
}
