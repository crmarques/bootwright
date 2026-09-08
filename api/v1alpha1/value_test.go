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
