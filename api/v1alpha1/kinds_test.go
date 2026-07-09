package v1alpha1

import (
	"reflect"
	"testing"
)

func TestAuthoredKindAccessorsCoverState(t *testing.T) {
	accessors := AuthoredKindAccessors()
	stateType := reflect.TypeOf(State{})
	if len(accessors) != stateType.NumField() {
		t.Fatalf("registry has %d kinds, State has %d fields", len(accessors), stateType.NumField())
	}
	seenField := map[string]bool{}
	seenKind := map[string]bool{}
	for _, acc := range accessors {
		if acc.Kind == "" || acc.StateField == "" || acc.Names == nil {
			t.Fatalf("incomplete accessor %+v", acc)
		}
		if seenKind[acc.Kind] {
			t.Errorf("kind %s registered twice", acc.Kind)
		}
		seenKind[acc.Kind] = true
		if seenField[acc.StateField] {
			t.Errorf("State field %s claimed twice", acc.StateField)
		}
		seenField[acc.StateField] = true
		field, ok := stateType.FieldByName(acc.StateField)
		if !ok {
			t.Errorf("accessor %s names missing State field %s", acc.Kind, acc.StateField)
			continue
		}
		if field.Type.Kind() != reflect.Slice {
			t.Errorf("State field %s is not a slice", acc.StateField)
		}
	}
}

func TestAuthoredKindAccessorNamesReadTheirField(t *testing.T) {
	var s State
	v := reflect.ValueOf(&s).Elem()
	for i := 0; i < v.NumField(); i++ {
		elem := reflect.New(v.Field(i).Type().Elem()).Elem()
		elem.FieldByName("Metadata").FieldByName("Name").SetString("probe-" + v.Type().Field(i).Name)
		v.Field(i).Set(reflect.Append(v.Field(i), elem))
	}
	for _, acc := range AuthoredKindAccessors() {
		names := acc.Names(s)
		want := "probe-" + acc.StateField
		if len(names) != 1 || names[0] != want {
			t.Errorf("accessor %s returned %v, want [%s]", acc.Kind, names, want)
		}
	}
}
