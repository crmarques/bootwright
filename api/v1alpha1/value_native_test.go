package v1alpha1

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestAValueConvertsToNativeDataByItsNumberStyle(t *testing.T) {
	huge := "123456789012345678901234567890"
	value := MapValue(
		FieldValue{Name: "name", Value: StringValue("a")},
		FieldValue{Name: "on", Value: BoolValue(true)},
		FieldValue{Name: "count", Value: IntegerValue("7")},
		FieldValue{Name: "ratio", Value: NumberValue("0.5")},
		FieldValue{Name: "huge", Value: IntegerValue(huge)},
		FieldValue{Name: "list", Value: ListValue(IntegerValue("1"))},
	)
	for name, want := range map[string]struct {
		style NumberStyle
		data  any
	}{
		"text":   {NumberText, map[string]any{"name": "a", "on": true, "count": json.Number("7"), "ratio": json.Number("0.5"), "huge": json.Number(huge), "list": []any{json.Number("1")}}},
		"binary": {NumberBinary, map[string]any{"name": "a", "on": true, "count": int64(7), "ratio": 0.5, "huge": huge, "list": []any{int64(1)}}},
	} {
		if got := value.Native(want.style); !reflect.DeepEqual(got, want.data) {
			t.Errorf("%s: native = %#v, want %#v", name, got, want.data)
		}
	}
	if got := (Value{}).Native(NumberText); got != nil {
		t.Errorf("an absent value converts to %#v", got)
	}
	checked := NativeOptions{Numbers: NumberInteger, RefuseAbsent: true}
	if _, err := value.NativeChecked(checked); err == nil {
		t.Error("a number or an integer beyond 64 bits converted as an integer")
	}
	if got, err := MapValue(FieldValue{Name: "count", Value: IntegerValue("7")}).NativeChecked(checked); err != nil || !reflect.DeepEqual(got, map[string]any{"count": int64(7)}) {
		t.Errorf("integer style = %#v (%v)", got, err)
	}
	if _, err := ListValue(Value{}).NativeChecked(checked); err == nil {
		t.Error("an absent item converted although absence is refused")
	}
	duplicated := MapValue(FieldValue{Name: "a", Value: StringValue("1")}, FieldValue{Name: "a", Value: StringValue("2")})
	if _, err := duplicated.NativeChecked(NativeOptions{RefuseDuplicates: true}); err == nil || err.Error() != "duplicate object field a" {
		t.Errorf("duplicate field error = %v", err)
	}
	if got, err := duplicated.NativeChecked(NativeOptions{}); err != nil || !reflect.DeepEqual(got, map[string]any{"a": "2"}) {
		t.Errorf("duplicates without refusal = %#v (%v)", got, err)
	}
}
