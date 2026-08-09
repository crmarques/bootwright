package converge

import (
	"errors"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/converge/remedy"
	"github.com/crmarques/bootwright/internal/ownership"
	"github.com/crmarques/bootwright/internal/workspace"
)

func TestPlanInfraComponentApplyBlocksSiblingOwnerByExactIdentity(t *testing.T) {
	t.Cleanup(workspace.SetRootDirForTest(t.TempDir()))
	mustContext(t, "spoke")
	hub := mustContext(t, "hub")
	owner := sharedArtifactRecord()
	owner.Labels["bootwright.kind"] = "loadBalancer"
	saveRecord(t, hub.OwnershipDir, owner)

	decision, err := PlanInfraComponentApplyBlocks("spoke", []InfraComponentServiceRef{{Name: owner.Name, Kind: "loadBalancer", Host: owner.Host}})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	refusal := InfraComponentApplyRefusal(decision, nil)
	if refusal == nil || !strings.Contains(refusal.Error(), "hub") || !strings.Contains(refusal.Error(), owner.Host) {
		t.Fatalf("refusal must name sibling and exact host: %v", refusal)
	}
	var typed remedy.Error
	if !errors.As(refusal, &typed) || typed.Remedy().Action != remedy.ActionRetrySameInvocation {
		t.Fatalf("refusal remedy = %#v, want exact same invocation", typed)
	}
}

func TestPlanInfraComponentApplyDoesNotConflateDifferentHosts(t *testing.T) {
	t.Cleanup(workspace.SetRootDirForTest(t.TempDir()))
	mustContext(t, "spoke")
	hub := mustContext(t, "hub")
	owner := sharedArtifactRecord()
	owner.Host = "other-bastion"
	saveRecord(t, hub.OwnershipDir, owner)

	decision, err := PlanInfraComponentApplyBlocks("spoke", []InfraComponentServiceRef{{Name: owner.Name, Kind: "loadBalancer", Host: "bastion.lab"}})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if refusal := InfraComponentApplyRefusal(decision, nil); refusal != nil {
		t.Fatalf("different host must not block: %v", refusal)
	}
}

func TestPlanInfraComponentApplyFailsClosedOnIncompleteSiblingIdentity(t *testing.T) {
	t.Cleanup(workspace.SetRootDirForTest(t.TempDir()))
	mustContext(t, "spoke")
	hub := mustContext(t, "hub")
	if err := ownership.SaveResource(hub.OwnershipDir, ownership.ResourceRecord{Kind: ownershipInfraComponentKind, Name: "prov1-edge", Owner: ownership.Owner}); err != nil {
		t.Fatalf("save incomplete owner: %v", err)
	}

	decision, err := PlanInfraComponentApplyBlocks("spoke", []InfraComponentServiceRef{{Name: "prov1-edge", Kind: "loadBalancer", Host: "bastion.lab"}})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	refusal := InfraComponentApplyRefusal(decision, nil)
	if refusal == nil || !strings.Contains(refusal.Error(), "has no host") || !strings.Contains(refusal.Error(), "no --authorize token") {
		t.Fatalf("incomplete evidence must fail closed with no bypass: %v", refusal)
	}
}
