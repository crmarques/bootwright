package reconciliation

import (
	"bytes"
	"encoding/json"
)

type Change string

const (
	Same    Change = "same"
	Added   Change = "added"
	Removed Change = "removed"
	Changed Change = "changed"
)

type BlockChange struct {
	Block  string
	Kind   string
	Object string
	Change Change
}

type ObjectChange struct {
	Kind   string
	Object string
	Change Change
	Blocks []string
}

type Delta []BlockChange

func Compare(frozen, fresh Plan) Delta {
	planned := make(map[string]Block, len(fresh.Blocks))
	for _, block := range fresh.Blocks {
		planned[block.ID] = block
	}
	delta := make(Delta, 0, len(frozen.Blocks)+len(fresh.Blocks))
	for _, block := range frozen.Blocks {
		theirs, found := planned[block.ID]
		change := BlockChange{Block: block.ID, Kind: block.Kind, Object: block.Object, Change: Same}
		switch {
		case !found:
			change.Change = Removed
		case differentEffects(block, theirs):
			change.Change = Changed
		}
		delta = append(delta, change)
		delete(planned, block.ID)
	}
	for _, block := range fresh.Blocks {
		if _, extra := planned[block.ID]; extra {
			delta = append(delta, BlockChange{Block: block.ID, Kind: block.Kind, Object: block.Object, Change: Added})
		}
	}
	return delta
}

func (d Delta) First() (BlockChange, bool) {
	for _, change := range d {
		if change.Change != Same {
			return change, true
		}
	}
	return BlockChange{}, false
}

func (d Delta) Count(change Change) int {
	count := 0
	for _, entry := range d {
		if entry.Change == change {
			count++
		}
	}
	return count
}

func (d Delta) Objects() []ObjectChange {
	var objects []ObjectChange
	position := map[string]int{}
	for _, entry := range d {
		key := entry.Kind + "/" + entry.Object
		index, seen := position[key]
		if !seen {
			index = len(objects)
			position[key] = index
			objects = append(objects, ObjectChange{Kind: entry.Kind, Object: entry.Object, Change: entry.Change})
		} else if objects[index].Change != entry.Change {
			objects[index].Change = Changed
		}
		objects[index].Blocks = append(objects[index].Blocks, entry.Block)
	}
	return objects
}

func differentEffects(frozen, fresh Block) bool {
	ours, theirs := effects(frozen), effects(fresh)
	return ours == nil || theirs == nil || !bytes.Equal(ours, theirs)
}

func effects(block Block) []byte {
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
