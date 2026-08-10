package ownership

import "testing"

func TestControllerResolverOwnershipUsesControllerInventory(t *testing.T) {
	group, known := InventoryGroupForKind(string(KindControllerNameResolver))
	if !known || group != GroupController {
		t.Fatalf("controller resolver inventory group = %q, %v; want %q, true", group, known, GroupController)
	}
}
