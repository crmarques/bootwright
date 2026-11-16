package adapterprotocol

import (
	"encoding/json"
	"slices"
	"testing"
	"unicode/utf8"
)

func FuzzDecodeRecord(f *testing.F) {
	for _, line := range []string{
		``,
		`{`,
		`[]`,
		`{"phase":"loaded"}`,
		`{"phase":"loaded"}{"phase":"loaded"}`,
		`{"phase":"loaded","Phase":"loaded"}`,
		`{"phase":"loaded", "x":1}`,
		`{"phase":"nope"}`,
		`{"phase":"native"}`,
		`{"phase":"continue"}`,
		`{"group":"pull-image","phase":"group","status":"running"}`,
		`{"group":"","phase":"group","status":"running"}`,
		`{"group":"pull-image","phase":"group","status":"unknown"}`,
		`{"phase":"refused","reason":"identity-mismatch"}`,
		`{"phase":"refused","reason":"timeout","source":"tool-helm"}`,
		`{"detail":"a:b@c-d_e.f","phase":"refused","reason":"x"}`,
		`{"nativeError":{"cause":"café failed","coordinate":"vm/one"},"phase":"refused","reason":"native"}`,
		`{"nativeError":{"cause":"\ud800","coordinate":"vm/one"},"phase":"refused","reason":"native"}`,
		`{"phase":"prepared","preparation":{"addedSources":["native-one"],"planDigest":"c"}}`,
		`{"evidence":{},"outcome":"changed","phase":"completed"}`,
		`{"evidence":{"a":{"b":[1,2,{"c":null}]}},"outcome":"unchanged","phase":"completed"}`,
		`{"evidence":{"note":"café <b>"},"outcome":"unchanged","phase":"completed"}`,
		`{"evidence":null,"outcome":"changed","phase":"completed"}`,
		`{"evidence":{"b":1,"a":2},"outcome":"changed","phase":"completed"}`,
		`{"evidence":{},"outcome":"changed","phase":"completed","warnings":[{"code":"storage.health","message":"degraded","object":{"kind":"Machine","name":"node-1"}}]}`,
		`{"evidence":{},"outcome":"changed","phase":"completed","warnings":[]}`,
		`{"evidence":{"a":[[[[[[[[[[[[[[[[[[1]]]]]]]]]]]]]]]]]]},"outcome":"changed","phase":"completed"}`,
	} {
		f.Add([]byte(line), false)
		f.Add([]byte(line), true)
	}
	f.Fuzz(func(t *testing.T, line []byte, controller bool) {
		if len(line) > MaxLine {
			t.Skip()
		}
		admission := Lifecycle
		if controller {
			admission = Controller
		}
		record, err := Decode(line, admission)
		again, repeated := Decode(line, admission)
		if (err == nil) != (repeated == nil) || err == nil && !sameRecord(t, record, again) {
			t.Fatalf("Decode judged %q twice with different answers", line)
		}
		if err != nil {
			return
		}
		if !printable(line) || spaced(line) {
			t.Fatalf("accepted %q that is not compact ASCII", line)
		}
		rule, known := phases[record.Phase]
		if !known || rule.admitted&admission == 0 {
			t.Fatalf("accepted phase %q the admission does not admit", record.Phase)
		}
		checkRecordBounds(t, record)
		reencoded, err := reencode(line)
		if err != nil || !printable(reencoded) {
			return
		}
		stable, err := Decode(reencoded, admission)
		if err != nil || !sameRecord(t, record, stable) {
			t.Fatalf("decode, encode, decode moved %q to %q: %v", line, reencoded, err)
		}
	})
}

func checkRecordBounds(t *testing.T, record Record) {
	t.Helper()
	for name, raw := range map[string]json.RawMessage{"evidence": record.Evidence, "preparation": record.Preparation} {
		if raw != nil && (!json.Valid(raw) || raw[0] != '{') {
			t.Fatalf("%s %q is not one JSON object", name, raw)
		}
	}
	if len(record.Warnings) > MaxWarnings || len(record.Detail) > maxDetail {
		t.Fatalf("record exceeds its bounds: %+v", record)
	}
	for _, warning := range record.Warnings {
		if !slices.Contains(warningCodes, warning.Code) || len(warning.Message) > MaxWarningMessage {
			t.Fatalf("warning %+v is outside its bounds", warning)
		}
	}
	if native := record.NativeError; native != nil {
		if len(native.Coordinate) > MaxNativeCoordinate || len(native.Cause) > MaxNativeCause || !utf8.ValidString(native.Coordinate+native.Cause) {
			t.Fatalf("native error %+v is outside its bounds", native)
		}
	}
}

// reencode writes a record's members back with the sorted keys and compact
// separators the collection's encoder writes.
func reencode(line []byte) ([]byte, error) {
	var members map[string]json.RawMessage
	if err := json.Unmarshal(line, &members); err != nil {
		return nil, err
	}
	return json.Marshal(members)
}

// sameRecord compares two records by their encoding, which compacts the raw
// members alike.
func sameRecord(t *testing.T, left, right Record) bool {
	t.Helper()
	a, err := json.Marshal(left)
	b, other := json.Marshal(right)
	if err != nil || other != nil {
		t.Fatalf("a decoded record does not encode: %v %v", err, other)
	}
	return string(a) == string(b)
}
