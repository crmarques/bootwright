package converge

import (
	"errors"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/converge/remedy"
	"github.com/crmarques/bootwright/internal/ownership"
	"github.com/crmarques/bootwright/internal/workspace"
)

func TestExclusiveSharedServiceRefusesSiblingExactIdentityWithoutBypass(t *testing.T) {
	t.Cleanup(workspace.SetRootDirForTest(t.TempDir()))
	for _, name := range []string{"self", "sibling"} {
		ctx, err := workspace.NewContext(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := workspace.EnsureDirs(ctx); err != nil {
			t.Fatal(err)
		}
	}
	sibling, err := workspace.NewContext("sibling")
	if err != nil {
		t.Fatal(err)
	}
	if err := ownership.SaveResource(sibling.OwnershipDir, ownership.ResourceRecord{
		Kind: "bmc-emulator", Name: "libvirt-a", Host: "service-host", Owner: ownership.Owner, Context: "sibling",
	}); err != nil {
		t.Fatal(err)
	}
	decision, err := PlanExclusiveSharedServiceBlocks("self", []InfraComponentServiceRef{{Kind: "bmc-emulator", Name: "libvirt-a", Host: "service-host"}})
	if err != nil {
		t.Fatal(err)
	}
	refusal := ExclusiveSharedServiceRefusal(decision, nil)
	if refusal == nil {
		t.Fatal("sibling owner did not refuse exclusive shared-service mutation")
	}
	for _, want := range []string{"bmc-emulator/libvirt-a", "service-host", "sibling", "no authorization token"} {
		if !strings.Contains(refusal.Error(), want) {
			t.Fatalf("refusal %q missing %q", refusal, want)
		}
	}
	var remedial remedy.Error
	if !errors.As(refusal, &remedial) || remedial.Remedy().Action != remedy.ActionRetrySameInvocation {
		t.Fatalf("refusal remedy = %#v, want exact retry", remedial)
	}
}

func TestExclusiveSharedServiceRefusesInconclusiveIdentityAndSiblingEvidence(t *testing.T) {
	t.Cleanup(workspace.SetRootDirForTest(t.TempDir()))
	for _, name := range []string{"self", "sibling"} {
		ctx, err := workspace.NewContext(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := workspace.EnsureDirs(ctx); err != nil {
			t.Fatal(err)
		}
	}
	sibling, err := workspace.NewContext("sibling")
	if err != nil {
		t.Fatal(err)
	}
	if err := ownership.SaveResource(sibling.OwnershipDir, ownership.ResourceRecord{
		Kind: "bmc-emulator", Name: "libvirt-a", Owner: ownership.Owner, Context: "sibling",
	}); err != nil {
		t.Fatal(err)
	}
	decision, err := PlanExclusiveSharedServiceBlocks("self", []InfraComponentServiceRef{
		{Kind: "bmc-emulator", Name: "libvirt-a", Host: "service-host"},
		{Kind: "bmc-emulator", Name: "libvirt-b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	refusal := ExclusiveSharedServiceRefusal(decision, nil)
	if refusal == nil {
		t.Fatal("inconclusive identity/evidence did not refuse")
	}
	for _, want := range []string{"no exact kind/name/host identity", "has no host"} {
		if !strings.Contains(refusal.Error(), want) {
			t.Fatalf("refusal %q missing %q", refusal, want)
		}
	}
}
