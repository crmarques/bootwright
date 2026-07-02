package ownership

import (
	"path/filepath"
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
	// ctx-c records a different component on the same bastion: must not match.
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
	// ctx-a is self (excluded), ctx-c references a different component; only ctx-b.
	if len(referrers) != 1 || referrers[0] != "ctx-b" {
		t.Fatalf("want [ctx-b], got %v", referrers)
	}
}

func TestOtherContextsWithRoleDiffersByHost(t *testing.T) {
	root := t.TempDir()
	dirA := filepath.Join(root, "ctx-a", "ownership")
	dirB := filepath.Join(root, "ctx-b", "ownership")
	// Same name, different host -> a different bastion's service, not a referrer.
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
	// ctx-b is a real context that has never recorded ownership (empty/absent store):
	// LoadResources returns no records and no error, so it is simply not a referrer.
	dirB := filepath.Join(root, "ctx-b", "ownership")
	stores := []ContextStore{{Context: "ctx-b", Dir: dirB}}
	referrers, skipped := OtherContextsWithRole(stores, "ctx-a", SharedComponentID{Kind: "infra-component", Name: "prov1-edge", Host: "bastion.lab"}, RoleOwner)
	if len(referrers) != 0 || len(skipped) != 0 {
		t.Fatalf("empty sibling store must yield no referrer and no skip, got referrers=%v skipped=%v", referrers, skipped)
	}
}
