package reconciliation

import "testing"

func TestPreparationScopeIsFrozenAndNeverNamesRemoval(t *testing.T) {
	plan, err := NewPlan(Apply, nil)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := plan.Digest()
	scope := &PreparationScope{Kind: "example-inventory", Command: "machine inventory", Targets: []string{"example-node"}}
	prepared, err := plan.WithPreparation(scope)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := prepared.Digest()
	if before == after {
		t.Fatal("preparation scope is outside the plan digest")
	}
	scope.Targets[0] = "changed"
	if prepared.Preparation.Targets[0] != "example-node" {
		t.Fatal("caller mutated frozen scope")
	}
	removal, _ := NewPlan(Destroy, nil)
	if _, err := removal.WithPreparation(prepared.Preparation); err == nil {
		t.Fatal("removal carries preparation intent")
	}
}

func TestPreparationRefusesUnboundedOrAmbiguousScope(t *testing.T) {
	for _, scope := range []PreparationScope{
		{}, {Kind: "../inventory", Targets: []string{"node"}},
		{Kind: "inventory", Targets: []string{"b", "a"}},
		{Kind: "inventory", Targets: []string{"a", "a"}},
		{Kind: "inventory", Targets: []string{"a\n"}},
		{Kind: "inventory", Targets: make([]string, MaxMachines+1)},
	} {
		if scope.Validate() == nil {
			t.Fatalf("invalid preparation accepted: %#v", scope)
		}
	}
}

func TestPreparationSubsetsRetainIndependentScope(t *testing.T) {
	plan, _ := NewPlan(Apply, nil)
	plan, _ = plan.WithPreparation(&PreparationScope{Kind: "inventory", Command: "machine discover", Targets: []string{"node"}})
	for _, subset := range []Plan{OwnedSubset(plan, nil), RemainingSubset(plan, nil)} {
		if subset.Preparation == nil {
			t.Fatal("subset lost preparation")
		}
		subset.Preparation.Targets[0] = "changed"
		if plan.Preparation.Targets[0] != "node" {
			t.Fatal("subset aliases preparation")
		}
	}
}
