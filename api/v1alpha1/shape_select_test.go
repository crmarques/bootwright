package v1alpha1

import "testing"

func TestAShapeSelectsAdmitsAndBoundsAValueOnce(t *testing.T) {
	number := &Shape{Type: Number, Minimum: "1", Maximum: "9"}
	text := &Shape{Type: String, MinLength: 2}
	either := &Shape{Alternatives: []*Shape{text, number}}
	if got := either.ForValue(IntegerValue("5")); got != number {
		t.Errorf("an integer selects %v, want the number alternative", got)
	}
	if got := either.ForValue(StringValue("ab")); got != text {
		t.Errorf("a string selects %v, want the string alternative", got)
	}
	if got := either.ForValue(BoolValue(true)); got != either {
		t.Errorf("a boolean selects %v, want the shape itself", got)
	}
	if !number.Admits(IntegerValue("5")) || number.Admits(StringValue("5")) || !text.Admits(StringValue("")) {
		t.Error("Admits does not accept an integer for a number and nothing else")
	}
	for value, want := range map[string][3]bool{"0": {true, false, true}, "5": {false, false, true}, "10": {false, true, true}, "x": {false, false, false}} {
		below, above, numeric := number.OutsideRange(IntegerValue(value))
		if got := [3]bool{below, above, numeric}; got != want {
			t.Errorf("OutsideRange(%s) = %v, want %v", value, got, want)
		}
	}
	if !text.ShortString(StringValue("a")) || text.ShortString(StringValue("ab")) || text.ShortString(IntegerValue("1")) {
		t.Error("ShortString does not hold a string to its minimum length")
	}
}
