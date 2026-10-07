package secretstore

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

// update rewrites each golden this package compares instead of comparing it:
// ./scripts/go test ./internal/secrets/secretstore -run Golden -update
var update = flag.Bool("update", false, "rewrite testdata/*.golden from the current output")

// matchesGolden compares one compact, LF-terminated JSON document with
// testdata/<name>.golden, which holds it indented for review. Indenting is
// lossless only for compact input, so the body must equal its json.Compact
// form before its indented golden can stand for the exact bytes.
func matchesGolden(t *testing.T, name string, data []byte) {
	t.Helper()
	body, found := bytes.CutSuffix(data, []byte("\n"))
	if !found || bytes.HasSuffix(body, []byte("\n")) {
		t.Fatalf("%s: a terminated format ends in exactly one LF: %q", name, data)
	}
	var compact, indented bytes.Buffer
	if err := json.Compact(&compact, body); err != nil || !bytes.Equal(compact.Bytes(), body) {
		t.Fatalf("%s: the bytes are not compact JSON (%v):\n%s", name, err, body)
	}
	if err := json.Indent(&indented, body, "", "  "); err != nil {
		t.Fatalf("%s: indenting: %v", name, err)
	}
	indented.WriteByte('\n')
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, indented.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v; run with -update to write it", err)
	}
	if !bytes.Equal(want, indented.Bytes()) {
		t.Errorf("%s differs; rerun with -update if the change is intended:\ngolden:\n%s\ngot:\n%s", path, want, indented.Bytes())
	}
}

// The store record is the exact bytes a reader proves canonical; its golden
// pins the payload as the encoder compacts and escapes it, and reads back
// through the package's own reader.
func TestTheSecretStoreRecordMatchesItsGolden(t *testing.T) {
	selector := Selector{SelectorVersion: RecordVersion, Context: "lab", Backend: "local-keyring", Generation: "gen-00000000000000000000000000000001"}
	data, err := EncodeRecord(selector, []byte(`{ "keyId": "key-1", "note": "<a&b>", "parts": [1, 2] }`))
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "record", data)
	decoded, err := DecodeRecord(data, "lab")
	if err != nil || decoded.Selector != selector {
		t.Fatalf("the record does not decode as itself: %+v (%v)", decoded, err)
	}
}
