package canonicaljson

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
)

type fuzzInner struct {
	Flag  bool    `json:"flag"`
	Items []int64 `json:"items,omitempty"`
}

type fuzzShape struct {
	Name   string            `json:"name"`
	Count  int               `json:"count"`
	Inner  *fuzzInner        `json:"inner,omitempty"`
	Labels map[string]string `json:"labels,omitempty"`
	Raw    json.RawMessage   `json:"raw,omitempty"`
}

var refusals = []error{ErrEncode, ErrMalformed, ErrTrailing, ErrNotCanonical}

func FuzzDecode(f *testing.F) {
	for _, seed := range []string{
		``,
		`{}`,
		`null`,
		`{"name":"a","count":1}`,
		`{"name":"a","count":1}` + "\n",
		`{"name":"a","count":1} `,
		`{"name":"a","count":1}{}`,
		`{"count":1,"name":"a"}`,
		`{"name":"a","Name":"b","count":1}`,
		`{"name":"a","name":"b","count":1}`,
		`{"name":"a","count":1,"extra":true}`,
		`{"name":"a","count":9223372036854775808}`,
		`{"name":"café <b>","count":-0}`,
		`{"name":"a","count":1,"inner":{"flag":true,"items":[1,2,3]}}`,
		`{"name":"a","count":1,"labels":{"a":"1","b":"2"}}`,
		`{"name":"a","count":1,"raw":{"z":1,"a":[true,null]}}`,
		`{"name":"a","count":1,"raw":{ "a" : 1 }}`,
		`{"name":"\ud800","count":1}`,
		`{"name":"a","count":1.0}`,
		`{"name":"a","count":1e2}`,
	} {
		f.Add([]byte(seed), false)
		f.Add([]byte(seed), true)
	}
	f.Fuzz(func(t *testing.T, data []byte, line bool) {
		if len(data) > 65536 {
			t.Skip()
		}
		ending := Bare
		if line {
			ending = Line
		}
		var target fuzzShape
		err := Decode(data, &target, ending)
		if err != nil {
			if !isRefusal(err) {
				t.Fatalf("Decode refused %q with %v, which is no refusal kind", data, err)
			}
			return
		}
		var closed fuzzShape
		if err := DecodeClosed(data, &closed); err != nil {
			t.Fatalf("Decode accepted %q that DecodeClosed refuses: %v", data, err)
		}
		encoded, err := Encode(target, ending)
		if err != nil || !bytes.Equal(encoded, data) {
			t.Fatalf("Decode accepted %q that is not its own encoding %q", data, encoded)
		}
		var stable fuzzShape
		if err := Decode(encoded, &stable, ending); err != nil {
			t.Fatalf("decode, encode, decode refused %q: %v", encoded, err)
		}
		again, err := Encode(stable, ending)
		if err != nil || !bytes.Equal(again, encoded) {
			t.Fatalf("decode, encode, decode moved %q to %q", encoded, again)
		}
	})
}

func FuzzProveObject(f *testing.F) {
	for _, seed := range []string{
		``,
		`{}`,
		`[]`,
		`null`,
		`{"a":1}`,
		`{"a":1} `,
		`{"a":1}{"b":2}`,
		`{"b":1,"a":2}`,
		`{"a":1,"a":2}`,
		`{"a": 1}`,
		`{"a":12345678901234567890}`,
		`{"a":1.50}`,
		`{"a":1E2}`,
		`{"a":"<b>&"}`,
		`{"a":"<b>&"}`,
		`{"a":"café"}`,
		`{"a":{"c":[1,2],"b":null}}`,
		`{"a":{"b":1,"c":2}}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 65536 {
			t.Skip()
		}
		err := ProveObject(data)
		if err != nil && !isRefusal(err) {
			t.Fatalf("ProveObject refused %q with %v, which is no refusal kind", data, err)
		}
		if err != nil {
			return
		}
		if len(data) < 2 || data[0] != '{' || data[len(data)-1] != '}' || !json.Valid(data) {
			t.Fatalf("ProveObject accepted %q, which is not one compact object", data)
		}
		var closed map[string]any
		if err := json.Unmarshal(data, &closed); err != nil {
			t.Fatalf("ProveObject accepted %q that does not read as an object: %v", data, err)
		}
		if ProveObject(data) != nil {
			t.Fatalf("ProveObject judged %q twice with different answers", data)
		}
	})
}

func isRefusal(err error) bool {
	for _, refusal := range refusals {
		if errors.Is(err, refusal) {
			return true
		}
	}
	return false
}
