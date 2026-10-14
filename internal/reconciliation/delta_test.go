package reconciliation

import (
	"bytes"
	"encoding/json"
	"math/rand"
	"slices"
	"testing"
)

func deltaPlan(t *testing.T, definitions ...BlockDefinition) Plan {
	t.Helper()
	built, err := NewPlan(Apply, definitions)
	if err != nil {
		t.Fatal(err)
	}
	return built
}

func deltaOf(frozen, fresh []BlockDefinition, t *testing.T) Delta {
	t.Helper()
	return Compare(deltaPlan(t, frozen...), deltaPlan(t, fresh...))
}

func changeOf(delta Delta, block string) Change {
	for _, entry := range delta {
		if entry.Block == block {
			return entry.Change
		}
	}
	return ""
}

func TestCompareClassifiesEveryBlock(t *testing.T) {
	edit := func(change func(*BlockDefinition)) BlockDefinition {
		d := definition("a")
		d.Groups = slices.Clone(d.Groups)
		change(&d)
		return d
	}
	for _, test := range []struct {
		name   string
		fresh  BlockDefinition
		expect Change
	}{
		{"identical", definition("a"), Same},
		{"a description", edit(func(d *BlockDefinition) { d.Description = "serve it differently" }), Same},
		{"impacts", edit(func(d *BlockDefinition) { d.Impacts = []string{"serve-a"} }), Same},
		{"a group description", edit(func(d *BlockDefinition) { d.Groups[0].Description = "fetch the image" }), Same},
		{"a request", edit(func(d *BlockDefinition) { d.Request = json.RawMessage(`{"name":"a","x":1}`) }), Changed},
		{"dependencies", edit(func(d *BlockDefinition) { d.Dependencies = []string{"b"} }), Changed},
		{"a stage", edit(func(d *BlockDefinition) { d.Stage = StageClusters }), Changed},
		{"consumes", edit(func(d *BlockDefinition) { d.Consumes = []string{AuthorizationDataLoss} }), Changed},
		{"a group machine", edit(func(d *BlockDefinition) { d.Groups[0].Machines = []string{"elsewhere"} }), Changed},
	} {
		t.Run(test.name, func(t *testing.T) {
			frozen := []BlockDefinition{definition("a"), definition("b")}
			fresh := []BlockDefinition{test.fresh, definition("b")}
			delta := deltaOf(frozen, fresh, t)
			if got := changeOf(delta, "a"); got != test.expect {
				t.Fatalf("change of a = %q, want %q (%+v)", got, test.expect, delta)
			}
			if got := changeOf(delta, "b"); got != Same {
				t.Fatalf("change of b = %q, want %q", got, Same)
			}
		})
	}
	t.Run("added and removed", func(t *testing.T) {
		delta := deltaOf([]BlockDefinition{definition("a"), definition("gone")}, []BlockDefinition{definition("a"), definition("new")}, t)
		want := map[string]Change{"a": Same, "gone": Removed, "new": Added}
		for block, change := range want {
			if got := changeOf(delta, block); got != change {
				t.Fatalf("change of %s = %q, want %q", block, got, change)
			}
		}
		if delta.Count(Added) != 1 || delta.Count(Removed) != 1 || delta.Count(Same) != 1 || delta.Count(Changed) != 0 {
			t.Fatalf("counts over %+v are wrong", delta)
		}
	})
	t.Run("kind and object follow the side that holds the block", func(t *testing.T) {
		gone, fresh := definition("gone"), definition("new")
		gone.Object, fresh.Object = "gone-object", "new-object"
		delta := deltaOf([]BlockDefinition{gone}, []BlockDefinition{fresh}, t)
		want := Delta{
			{Block: "gone", Kind: "ArtifactServer", Object: "gone-object", Change: Removed},
			{Block: "new", Kind: "ArtifactServer", Object: "new-object", Change: Added},
		}
		if !slices.Equal(delta, want) {
			t.Fatalf("delta = %+v, want %+v", delta, want)
		}
	})
	t.Run("unmarshalable effects read changed", func(t *testing.T) {
		broken := deltaPlan(t, definition("a"))
		broken.Blocks[0].Request = json.RawMessage(`{`)
		delta := Compare(broken, broken)
		if got := changeOf(delta, "a"); got != Changed {
			t.Fatalf("change of a = %q, want %q", got, Changed)
		}
	})
}

func TestCompareKeepsFrozenOrderThenFreshOrder(t *testing.T) {
	frozen := []BlockDefinition{definition("a"), definition("b", "a"), definition("c", "b"), definition("d", "c")}
	fresh := []BlockDefinition{definition("a"), definition("x", "a"), definition("c"), definition("y", "x"), definition("d", "c")}
	delta := deltaOf(frozen, fresh, t)
	var got []string
	for _, entry := range delta {
		got = append(got, entry.Block+"="+string(entry.Change))
	}
	want := []string{"a=same", "b=removed", "c=changed", "d=same", "x=added", "y=added"}
	if !slices.Equal(got, want) {
		t.Fatalf("delta order = %v, want %v", got, want)
	}
	first, differs := delta.First()
	if !differs || first.Block != "b" {
		t.Fatalf("first = %+v, %t, want b", first, differs)
	}
}

func oldEffectsOf(block Block) []byte {
	block.Description, block.Impacts = "", nil
	groups := make([]Group, 0, len(block.Groups))
	for _, group := range block.Groups {
		group.Description = ""
		groups = append(groups, group)
	}
	block.Groups = groups
	encoded, err := json.Marshal(block)
	if err != nil {
		return nil
	}
	return encoded
}

func oldDifferingBlock(frozen, fresh Plan) (string, bool) {
	planned := make(map[string][]byte, len(fresh.Blocks))
	for _, block := range fresh.Blocks {
		planned[block.ID] = oldEffectsOf(block)
	}
	for _, block := range frozen.Blocks {
		theirs, found := planned[block.ID]
		ours := oldEffectsOf(block)
		if !found || ours == nil || theirs == nil || !bytes.Equal(ours, theirs) {
			return block.ID, true
		}
		delete(planned, block.ID)
	}
	for _, block := range fresh.Blocks {
		if _, extra := planned[block.ID]; extra {
			return block.ID, true
		}
	}
	return "", false
}

func randomDefinitions(rng *rand.Rand, pool []string) []BlockDefinition {
	var definitions []BlockDefinition
	previous := ""
	for _, id := range pool {
		if rng.Intn(4) == 0 {
			continue
		}
		var dependencies []string
		if previous != "" && rng.Intn(3) != 0 {
			dependencies = []string{previous}
		}
		d := definition(id, dependencies...)
		d.Groups = slices.Clone(d.Groups)
		switch rng.Intn(10) {
		case 0:
			d.Description = "another description"
		case 1:
			d.Impacts = []string{"another-impact"}
		case 2:
			d.Groups[0].Description = "another group description"
		case 3:
			d.Request = json.RawMessage(`{"name":"` + id + `","x":1}`)
		case 4:
			d.Stage = StageClusters
		case 5:
			d.Consumes = []string{AuthorizationDataLoss}
		case 6:
			d.Groups[0].Machines = []string{"elsewhere"}
		}
		definitions = append(definitions, d)
		previous = id
	}
	return definitions
}

func TestFirstAgreesWithTheDifferingBlockItReplaces(t *testing.T) {
	rng := rand.New(rand.NewSource(86))
	pool := []string{"a", "b", "c", "d", "e", "f", "g"}
	differing := 0
	for i := 0; i < 200; i++ {
		definitions := randomDefinitions(rng, pool)
		frozen := deltaPlan(t, definitions...)
		fresh := deltaPlan(t, randomDefinitions(rng, pool)...)
		if i%3 == 0 {
			reworded := slices.Clone(definitions)
			for j := range reworded {
				reworded[j].Description = "reworded"
				reworded[j].Impacts = nil
			}
			fresh = deltaPlan(t, reworded...)
		}
		wantBlock, wantDiffers := oldDifferingBlock(frozen, fresh)
		got, gotDiffers := Compare(frozen, fresh).First()
		if gotDiffers != wantDiffers || got.Block != wantBlock {
			t.Fatalf("pair %d: first = %q, %t, want %q, %t", i, got.Block, gotDiffers, wantBlock, wantDiffers)
		}
		if wantDiffers {
			differing++
		}
	}
	if differing == 0 || differing == 200 {
		t.Fatalf("%d of 200 pairs differ, so the comparison is not exercised both ways", differing)
	}
}

func TestObjectsFoldsBlocksByObject(t *testing.T) {
	delta := Delta{
		{Block: "same-1", Kind: "K", Object: "same", Change: Same},
		{Block: "add-1", Kind: "K", Object: "add", Change: Added},
		{Block: "gone-1", Kind: "K", Object: "gone", Change: Removed},
		{Block: "mix-1", Kind: "K", Object: "mix", Change: Same},
		{Block: "same-2", Kind: "K", Object: "same", Change: Same},
		{Block: "add-2", Kind: "K", Object: "add", Change: Added},
		{Block: "gone-2", Kind: "K", Object: "gone", Change: Removed},
		{Block: "mix-2", Kind: "K", Object: "mix", Change: Added},
		{Block: "other-kind", Kind: "L", Object: "same", Change: Removed},
		{Block: "mix-3", Kind: "K", Object: "mix", Change: Same},
	}
	want := []ObjectChange{
		{Kind: "K", Object: "same", Change: Same, Blocks: []string{"same-1", "same-2"}},
		{Kind: "K", Object: "add", Change: Added, Blocks: []string{"add-1", "add-2"}},
		{Kind: "K", Object: "gone", Change: Removed, Blocks: []string{"gone-1", "gone-2"}},
		{Kind: "K", Object: "mix", Change: Changed, Blocks: []string{"mix-1", "mix-2", "mix-3"}},
		{Kind: "L", Object: "same", Change: Removed, Blocks: []string{"other-kind"}},
	}
	got := delta.Objects()
	if len(got) != len(want) {
		t.Fatalf("objects = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i].Kind != want[i].Kind || got[i].Object != want[i].Object || got[i].Change != want[i].Change || !slices.Equal(got[i].Blocks, want[i].Blocks) {
			t.Fatalf("object %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if len(Delta(nil).Objects()) != 0 {
		t.Fatal("an empty delta must fold to no objects")
	}
}
