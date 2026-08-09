package ownership

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestOtherContextsWithRoleMatchesSharedComponentAcrossContexts(t *testing.T) {
	root := t.TempDir()
	dirA := filepath.Join(root, "ctx-a", "ownership")
	dirB := filepath.Join(root, "ctx-b", "ownership")
	dirC := filepath.Join(root, "ctx-c", "ownership")

	shared := ResourceRecord{Kind: "infra-component", Name: "prov1-edge", Host: "bastion.lab", Owner: Owner}
	for _, dir := range []string{dirA, dirB} {
		if err := SaveResource(dir, shared); err != nil {
			t.Fatalf("save %s: %v", dir, err)
		}
	}
	other := ResourceRecord{Kind: "infra-component", Name: "prov1-other", Host: "bastion.lab", Owner: Owner}
	if err := SaveResource(dirC, other); err != nil {
		t.Fatalf("save C: %v", err)
	}

	stores := []ContextStore{
		{Context: "ctx-a", Dir: dirA},
		{Context: "ctx-b", Dir: dirB},
		{Context: "ctx-c", Dir: dirC},
	}
	id := SharedComponentID{Kind: "infra-component", Name: "prov1-edge", Host: "bastion.lab"}
	referrers, skipped := OtherContextsWithRole(stores, "ctx-a", id, RoleOwner)
	if len(skipped) != 0 {
		t.Fatalf("unexpected skipped: %v", skipped)
	}
	if len(referrers) != 1 || referrers[0] != "ctx-b" {
		t.Fatalf("want [ctx-b], got %v", referrers)
	}
}

func TestOtherContextsWithRoleDiffersByHost(t *testing.T) {
	root := t.TempDir()
	dirA := filepath.Join(root, "ctx-a", "ownership")
	dirB := filepath.Join(root, "ctx-b", "ownership")
	if err := SaveResource(dirB, ResourceRecord{Kind: "infra-component", Name: "prov1-edge", Host: "other-bastion", Owner: Owner}); err != nil {
		t.Fatalf("save B: %v", err)
	}
	stores := []ContextStore{{Context: "ctx-a", Dir: dirA}, {Context: "ctx-b", Dir: dirB}}
	referrers, _ := OtherContextsWithRole(stores, "ctx-a", SharedComponentID{Kind: "infra-component", Name: "prov1-edge", Host: "bastion.lab"}, RoleOwner)
	if len(referrers) != 0 {
		t.Fatalf("want none (different host), got %v", referrers)
	}
}

func TestOtherContextsWithRoleToleratesEmptySiblingStore(t *testing.T) {
	root := t.TempDir()
	dirB := filepath.Join(root, "ctx-b", "ownership")
	stores := []ContextStore{{Context: "ctx-b", Dir: dirB}}
	referrers, skipped := OtherContextsWithRole(stores, "ctx-a", SharedComponentID{Kind: "infra-component", Name: "prov1-edge", Host: "bastion.lab"}, RoleOwner)
	if len(referrers) != 0 || len(skipped) != 0 {
		t.Fatalf("empty sibling store must yield no referrer and no skip, got referrers=%v skipped=%v", referrers, skipped)
	}
}

func TestOtherContextsWithRolesForComponentsFailsClosedOnMissingHost(t *testing.T) {
	root := t.TempDir()
	dirB := filepath.Join(root, "ctx-b", "ownership")
	if err := SaveResource(dirB, ResourceRecord{Kind: "infra-component", Name: "prov1-edge", Owner: Owner}); err != nil {
		t.Fatalf("save B: %v", err)
	}
	id := SharedComponentID{Kind: "infra-component", Name: "prov1-edge", Host: "bastion.lab"}
	relations, skipped := OtherContextsWithRolesForComponents([]ContextStore{{Context: "ctx-b", Dir: dirB}}, "ctx-a", []SharedComponentID{id}, RoleOwner, RoleReference)
	if len(relations) != 0 {
		t.Fatalf("identity-incomplete record must not be treated as an exact match: %v", relations)
	}
	if len(skipped) != 1 || !strings.Contains(skipped[0].Error(), "has no host") {
		t.Fatalf("missing host must be surfaced as an unsafe scan warning, got %v", skipped)
	}
}

func TestOtherContextsWithRolesForComponentsCombinesOwnerAndReference(t *testing.T) {
	root := t.TempDir()
	id := SharedComponentID{Kind: "infra-component", Name: "prov1-edge", Host: "bastion.lab"}
	dirOwner := filepath.Join(root, "owner", "ownership")
	dirReference := filepath.Join(root, "reference", "ownership")
	if err := SaveResource(dirOwner, ResourceRecord{Kind: id.Kind, Name: id.Name, Host: id.Host, Owner: Owner}); err != nil {
		t.Fatalf("save owner: %v", err)
	}
	if err := SaveResource(dirReference, ResourceRecord{Kind: id.Kind, Name: id.Name, Host: id.Host, Owner: Owner, Role: RoleReference, Context: "reference"}); err != nil {
		t.Fatalf("save reference: %v", err)
	}
	relations, skipped := OtherContextsWithRolesForComponents([]ContextStore{{Context: "owner", Dir: dirOwner}, {Context: "reference", Dir: dirReference}}, "self", []SharedComponentID{id}, RoleOwner, RoleReference)
	if len(skipped) != 0 {
		t.Fatalf("unexpected scan warnings: %v", skipped)
	}
	got := relations[id]
	if len(got) != 2 || got[0] != "owner" || got[1] != "reference" {
		t.Fatalf("combined relations = %v, want [owner reference]", got)
	}
}

func TestOtherContextsWithRolesForComponentsDoesNotScanWithoutAConsequence(t *testing.T) {
	root := t.TempDir()
	badStore := filepath.Join(root, "bad", "ownership")
	if err := SaveResource(badStore, ResourceRecord{Kind: "infra-component", Name: "prov1-edge", Owner: Owner}); err != nil {
		t.Fatalf("save incomplete record: %v", err)
	}
	relations, skipped := OtherContextsWithRolesForComponents([]ContextStore{{Context: "bad", Dir: badStore}}, "self", nil, RoleOwner, RoleReference)
	if len(relations) != 0 || len(skipped) != 0 {
		t.Fatalf("no destructive consequence must not be blocked by unrelated evidence: relations=%v skipped=%v", relations, skipped)
	}
}
