package ownership

import (
	"path/filepath"
	"testing"
)

func TestEffectiveRoleDefaultsToOwner(t *testing.T) {
	if got := (ResourceRecord{}).EffectiveRole(); got != RoleOwner {
		t.Fatalf("empty role must read as owner, got %q", got)
	}
	if (ResourceRecord{}).IsReference() {
		t.Fatalf("empty role must not be a reference")
	}
	if got := (ResourceRecord{Role: RoleReference}).EffectiveRole(); got != RoleReference {
		t.Fatalf("role reference, got %q", got)
	}
	if !(ResourceRecord{Role: RoleReference}).IsReference() {
		t.Fatalf("role reference must report IsReference")
	}
}

func TestRoleRoundTripsThroughSaveLoad(t *testing.T) {
	dir := t.TempDir()
	rec := ResourceRecord{Kind: "infra-component", Name: "prov1-edge", Host: "bastion.lab", Role: RoleReference, Context: "spoke"}
	if err := SaveResource(dir, rec); err != nil {
		t.Fatalf("save: %v", err)
	}
	records, err := LoadResources(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("want 1 record, got %d", len(records))
	}
	if records[0].EffectiveRole() != RoleReference {
		t.Fatalf("role did not round-trip, got %q", records[0].EffectiveRole())
	}
}

func TestReferenceRecordDoesNotOverwriteOwnerFile(t *testing.T) {
	dir := t.TempDir()
	owner := ResourceRecord{Kind: "infra-component", Name: "prov1-edge", Host: "bastion.lab", Context: "hub"}
	ref := ResourceRecord{Kind: "infra-component", Name: "prov1-edge", Host: "bastion.lab", Role: RoleReference, Context: "spoke"}

	ownerPath, err := ResourcePath(dir, owner)
	if err != nil {
		t.Fatalf("owner path: %v", err)
	}
	refPath, err := ResourcePath(dir, ref)
	if err != nil {
		t.Fatalf("reference path: %v", err)
	}
	if ownerPath == refPath {
		t.Fatalf("owner and reference records must not share a path: %s", ownerPath)
	}
	if filepath.Base(ownerPath) != "prov1-edge.json" {
		t.Fatalf("owner filename = %q, want prov1-edge.json", filepath.Base(ownerPath))
	}
	if filepath.Base(refPath) != "prov1-edge@spoke.json" {
		t.Fatalf("reference filename = %q, want prov1-edge@spoke.json", filepath.Base(refPath))
	}

	if err := SaveResource(dir, owner); err != nil {
		t.Fatalf("save owner: %v", err)
	}
	if err := SaveResource(dir, ref); err != nil {
		t.Fatalf("save reference: %v", err)
	}
	records, err := LoadResources(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("want owner + reference (2 records), got %d", len(records))
	}
}

func TestReferenceRecordPathRequiresContext(t *testing.T) {
	_, err := ResourcePath(t.TempDir(), ResourceRecord{Kind: "infra-component", Name: "prov1-edge", Role: RoleReference})
	if err == nil {
		t.Fatalf("a reference record with no context must not resolve a path")
	}
}

func TestValidateResourceRejectsUnknownRole(t *testing.T) {
	if err := ValidateResource(ResourceRecord{Kind: "infra-component", Name: "edge", Role: "bystander"}); err == nil {
		t.Fatalf("unknown role must be rejected")
	}
	if err := ValidateResource(ResourceRecord{Kind: "infra-component", Name: "edge", Role: RoleReference}); err != nil {
		t.Fatalf("reference role must validate: %v", err)
	}
}

func TestOwnerAndReferenceContextScans(t *testing.T) {
	root := t.TempDir()
	dirHub := filepath.Join(root, "hub", "ownership")
	dirSpokeA := filepath.Join(root, "spoke-a", "ownership")
	dirSpokeB := filepath.Join(root, "spoke-b", "ownership")

	id := SharedComponentID{Kind: "infra-component", Name: "prov1-artifacts", Host: "bastion.lab"}
	if err := SaveResource(dirHub, ResourceRecord{Kind: id.Kind, Name: id.Name, Host: id.Host, Context: "hub"}); err != nil {
		t.Fatalf("save hub owner: %v", err)
	}
	for ctx, dir := range map[string]string{"spoke-a": dirSpokeA, "spoke-b": dirSpokeB} {
		if err := SaveResource(dir, ResourceRecord{Kind: id.Kind, Name: id.Name, Host: id.Host, Role: RoleReference, Context: ctx}); err != nil {
			t.Fatalf("save %s reference: %v", ctx, err)
		}
	}
	stores := []ContextStore{
		{Context: "hub", Dir: dirHub},
		{Context: "spoke-a", Dir: dirSpokeA},
		{Context: "spoke-b", Dir: dirSpokeB},
	}

	refs, skipped := ReferenceContexts(stores, "hub", id)
	if len(skipped) != 0 {
		t.Fatalf("unexpected skipped: %v", skipped)
	}
	if len(refs) != 2 || refs[0] != "spoke-a" || refs[1] != "spoke-b" {
		t.Fatalf("want [spoke-a spoke-b] referrers, got %v", refs)
	}
	if owners, _ := OtherContextsWithRole(stores, "hub", id, RoleOwner); len(owners) != 0 {
		t.Fatalf("hub is the only owner; want no sibling owners, got %v", owners)
	}

	owners, _ := OtherContextsWithRole(stores, "spoke-a", id, RoleOwner)
	if len(owners) != 1 || owners[0] != "hub" {
		t.Fatalf("want [hub] owner, got %v", owners)
	}
}
