package v1alpha1

import (
	"reflect"
	"sort"
	"testing"
)

// TestInfraComponentSlotsCoverArmUnion ties the three places the InfraComponent
// arm union is spelled — the struct's pointer arm fields, SetSlots, and
// InfraComponentSlots — into one enforced invariant. Adding a managed service
// arm without updating SetSlots (so the exactly-one validator never counts it)
// or without updating InfraComponentSlots (so the registry and render vars never
// see it) is exactly the silent, scattered omission a new service risks; this
// guard turns it into a red test. Reflection over the struct keeps the arm side
// self-maintaining: a new pointer field is detected without editing the test.
func TestInfraComponentSlotsCoverArmUnion(t *testing.T) {
	specType := reflect.TypeOf(InfraComponentSpec{})

	// Build a spec with every pointer arm populated, and count the arms.
	specValue := reflect.New(specType).Elem()
	armCount := 0
	for i := 0; i < specType.NumField(); i++ {
		field := specType.Field(i)
		if field.Type.Kind() != reflect.Ptr {
			continue // Type discriminator and any non-arm scalar
		}
		armCount++
		specValue.Field(i).Set(reflect.New(field.Type.Elem()))
	}
	spec := specValue.Interface().(InfraComponentSpec)

	got := spec.SetSlots()
	if len(got) != armCount {
		t.Fatalf("InfraComponentSpec has %d pointer arm fields but SetSlots returned %d slots (%v); add the new arm to SetSlots", armCount, len(got), got)
	}
	if !sameStringSet(got, InfraComponentSlots()) {
		t.Fatalf("SetSlots %v and InfraComponentSlots %v disagree; keep the arm union, SetSlots, and InfraComponentSlots in lockstep", sortedCopy(got), sortedCopy(InfraComponentSlots()))
	}
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := map[string]int{}
	for _, s := range a {
		set[s]++
	}
	for _, s := range b {
		set[s]--
	}
	for _, n := range set {
		if n != 0 {
			return false
		}
	}
	return true
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
