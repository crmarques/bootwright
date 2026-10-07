package canonicaljson

import (
	"encoding/json"
	"math"
	"testing"
	"time"
)

type sizedLeaf struct {
	Text   string   `json:"text"`
	Number int64    `json:"number"`
	Tags   []string `json:"tags"`
}

type sizedRecord struct {
	Flag       bool            `json:"flag"`
	Small      int8            `json:"small"`
	Large      uint64          `json:"large"`
	Name       string          `json:"name"`
	Untagged   int16           `json:""`
	Skipped    string          `json:"-"`
	Pointer    *sizedLeaf      `json:"pointer"`
	Leaves     []sizedLeaf     `json:"leaves"`
	Fixed      [2]uint8        `json:"fixed"`
	Any        any             `json:"any"`
	Raw        json.RawMessage `json:"raw"`
	OmitBool   bool            `json:"omitBool,omitempty"`
	OmitInt    int             `json:"omitInt,omitempty"`
	OmitUint   uint32          `json:"omitUint,omitempty"`
	OmitText   string          `json:"omitText,omitempty"`
	OmitSlice  []int           `json:"omitSlice,omitempty"`
	OmitRef    *bool           `json:"omitRef,omitempty"`
	OmitRawMsg json.RawMessage `json:"omitRaw,omitempty"`
}

func TestSizeEqualsTheEncodedLength(t *testing.T) {
	yes := true
	full := sizedRecord{
		Flag: true, Small: math.MinInt8, Large: math.MaxUint64, Name: "a<b>&c \"q\" \\ \b\f\n\r\t \x00\x1f\x7f \u2028\u2029 é 中 \xff\xfe",
		Untagged: -7, Skipped: "never", Pointer: &sizedLeaf{Text: "", Number: math.MinInt64, Tags: []string{}},
		Leaves: []sizedLeaf{{Text: "x", Number: math.MaxInt64, Tags: []string{"a", "b"}}, {}}, Fixed: [2]uint8{0, 255},
		Any: []any{nil, true, "s", int32(-1), map[string]any(nil) == nil}, Raw: json.RawMessage("{ \"a\" : [1, 2,\n\t3], \"b\" : \"<&> \u2028 \\\" \\\\\" , \"c\":\"\xff\" }"),
		OmitBool: true, OmitInt: -1, OmitUint: 3, OmitText: "t", OmitSlice: []int{}, OmitRef: &yes, OmitRawMsg: json.RawMessage(`[ ]`),
	}
	for name, value := range map[string]any{
		"nil":                  nil,
		"a bool":               false,
		"the least int":        int64(math.MinInt64),
		"the largest uint":     uint64(math.MaxUint64),
		"a uintptr":            uintptr(42),
		"an escaped string":    "<script>&\u2028\xff",
		"a nil slice":          []string(nil),
		"an empty slice":       []string{},
		"a nil pointer":        (*sizedLeaf)(nil),
		"a raw message":        json.RawMessage(" [ \"<\" , 1 ] "),
		"an empty record":      sizedRecord{Raw: json.RawMessage(`null`)},
		"a full record":        full,
		"a pointer to it":      &full,
		"an interface over it": any(full),
	} {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		size, ok := Size(value, len(encoded), 8)
		if !ok || size != len(encoded) {
			t.Errorf("%s: Size = %d, %v; encoding/json wrote %d bytes: %s", name, size, ok, len(encoded), encoded)
		}
	}
}

func TestSizeRefusesWhatItCannotPredict(t *testing.T) {
	type embedded struct{ sizedLeaf }
	type unexported struct {
		Name  string `json:"name"`
		alias string
	}
	type omittedFloat struct {
		Ratio float64 `json:"ratio,omitempty"`
	}
	type quoted struct {
		Count int `json:"count,string"`
	}
	type nested struct {
		Leaves [][]sizedLeaf `json:"leaves"`
	}
	deep := nested{Leaves: [][]sizedLeaf{{{Tags: []string{"a"}}}}}
	encoded, err := json.Marshal(deep)
	if err != nil {
		t.Fatal(err)
	}
	if size, ok := Size(deep, len(encoded), 5); !ok || size != len(encoded) {
		t.Fatalf("the nested value at its own depth: %d, %v", size, ok)
	}
	for name, tc := range map[string]struct {
		value        any
		limit, depth int
	}{
		"a map":                 {map[string]int{"a": 1}, 64, 8},
		"a float":               {1.5, 64, 8},
		"a byte slice":          {[]byte("abc"), 64, 8},
		"an embedded struct":    {embedded{}, 64, 8},
		"an unexported field":   {unexported{Name: "a", alias: "b"}, 64, 8},
		"an omitempty float":    {omittedFloat{}, 64, 8},
		"another tag option":    {quoted{Count: 1}, 64, 8},
		"a self-encoding type":  {time.Time{}, 64, 8},
		"an absent raw message": {json.RawMessage(nil), 64, 8},
		"an invalid raw value":  {json.RawMessage(`{`), 64, 8},
		"one level too deep":    {deep, len(encoded), 4},
		"one byte too long":     {deep, len(encoded) - 1, 5},
	} {
		if size, ok := Size(tc.value, tc.limit, tc.depth); ok {
			t.Errorf("%s: predicted %d bytes", name, size)
		}
	}
}
