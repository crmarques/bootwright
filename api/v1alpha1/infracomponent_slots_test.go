package v1alpha1

import (
	"reflect"
	"sort"
	"testing"
)

func TestInfraComponentSlotsCoverArmUnion(t *testing.T) {
	specType := reflect.TypeOf(InfraComponentSpec{})

	specValue := reflect.New(specType).Elem()
	armCount := 0
	for i := 0; i < specType.NumField(); i++ {
		field := specType.Field(i)
		if field.Type.Kind() != reflect.Ptr {
			continue
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
