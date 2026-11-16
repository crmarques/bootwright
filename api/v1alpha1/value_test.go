package v1alpha1

import "testing"

func TestValuesAreImmutable(t *testing.T) {
	fields := []FieldValue{{Name: "nested", Value: ListValue(StringValue("original"))}}
	v := MapValue(fields...)
	fields[0].Value = StringValue("changed")
	copy := v.Fields()
	copy[0].Value = StringValue("changed")
	items := v.Get("nested").Items()
	items[0] = StringValue("changed")
	updated := v.WithPath(StringValue("new"), "other", "field")
	if v.Has("other") || v.Get("nested").Items()[0].Text() != "original" || updated.Get("other", "field").Text() != "new" {
		t.Fatal("value aliases mutable input")
	}
}

func TestExactInteger(t *testing.T) {
	const input = "184467440737095516160000000000000000000"
	if got := IntegerValue(input).Text(); got != input {
		t.Fatalf("integer lost precision: %s", got)
	}
}

func TestNamedReturnsOnlyAUniqueMatch(t *testing.T) {
	entry := func(name, tag string) Value {
		return MapValue(FieldValue{Name: "name", Value: StringValue(name)}, FieldValue{Name: "tag", Value: StringValue(tag)})
	}
	list := ListValue(entry("a", "first"), entry("b", "only"), entry("a", "second"))
	if got, ok := list.Named("name", "b"); !ok || got.Get("tag").Text() != "only" {
		t.Fatalf("unique name: %v %v", got, ok)
	}
	for name, want := range map[string]string{"a": "repeated", "c": "absent", "": "empty"} {
		if got, ok := list.Named("name", name); ok || got.Present() {
			t.Errorf("%s name %q matched %v", want, name, got)
		}
	}
	if _, ok := (Value{}).Named("name", "a"); ok {
		t.Fatal("a missing sequence matched")
	}
}
