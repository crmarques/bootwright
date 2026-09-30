package power

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// update rewrites each golden this package compares instead of comparing it:
// ./scripts/go test ./internal/machine/power -run Golden -update
var update = flag.Bool("update", false, "rewrite testdata/*.golden from the current output")

// matchesGolden compares one compact JSON document with testdata/<name>.golden,
// which holds it indented for review. A terminated format ends in exactly one
// LF, which is stripped before the comparison; any other format ends in no
// whitespace at all. Indenting is lossless only for compact input, so the body
// must equal its json.Compact form before its indented golden can stand for
// the exact bytes.
func matchesGolden(t *testing.T, name string, data []byte, terminated bool) {
	t.Helper()
	body := data
	if terminated {
		trimmed, found := bytes.CutSuffix(data, []byte("\n"))
		if !found || bytes.HasSuffix(trimmed, []byte("\n")) {
			t.Fatalf("%s: a terminated format ends in exactly one LF: %q", name, data[max(0, len(data)-16):])
		}
		body = trimmed
	} else if len(bytes.TrimRight(data, " \t\r\n")) != len(data) {
		t.Fatalf("%s: an unterminated format ends in no whitespace: %q", name, data[max(0, len(data)-16):])
	}
	var compact, indented bytes.Buffer
	if err := json.Compact(&compact, body); err != nil {
		t.Fatalf("%s: the bytes are not one JSON document: %v", name, err)
	}
	if !bytes.Equal(compact.Bytes(), body) {
		t.Fatalf("%s: the bytes are not compact JSON, so an indented golden cannot pin them:\n%s", name, body)
	}
	if err := json.Indent(&indented, body, "", "  "); err != nil {
		t.Fatalf("%s: indenting: %v", name, err)
	}
	indented.WriteByte('\n')
	matchesTextGolden(t, name, indented.Bytes())
}

// matchesTextGolden compares bytes that are not one compact JSON document with
// testdata/<name>.golden byte for byte. git diff --check refuses a line ending
// in a space or tab and a blank line at the end of a file, so a golden holding
// either could never be committed.
func matchesTextGolden(t *testing.T, name string, data []byte) {
	t.Helper()
	text := string(data)
	if text == "\n" || strings.HasSuffix(text, "\n\n") {
		t.Fatalf("%s: the bytes end in a blank line, which git diff --check refuses", name)
	}
	for number, line := range strings.Split(text, "\n") {
		if strings.TrimRight(line, " \t") != line {
			t.Fatalf("%s: line %d ends in a space or tab, which git diff --check refuses", name, number+1)
		}
	}
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v; run with -update to write it", err)
	}
	if string(want) != text {
		t.Errorf("%s differs (-golden +got); rerun with -update if the change is intended:\n%s%s",
			path, lineDiff(string(want), text), firstDifference(string(want), text))
	}
}

// firstDifference quotes both texts around the first byte they differ at,
// which locates a change inside one long line such as an embedded document.
func firstDifference(want, got string) string {
	at := 0
	for at < len(want) && at < len(got) && want[at] == got[at] {
		at++
	}
	excerpt := func(text string) string { return text[max(0, at-40):min(len(text), at+40)] }
	return fmt.Sprintf("first difference at byte %d: golden %q, got %q", at, excerpt(want), excerpt(got))
}

// lineDiff lists the lines only the golden holds (-) and only the output holds
// (+), each numbered in its own text, along a longest common subsequence.
func lineDiff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	common := make([][]int, len(w)+1)
	for i := range common {
		common[i] = make([]int, len(g)+1)
	}
	for i := len(w) - 1; i >= 0; i-- {
		for j := len(g) - 1; j >= 0; j-- {
			if w[i] == g[j] {
				common[i][j] = common[i+1][j+1] + 1
			} else {
				common[i][j] = max(common[i+1][j], common[i][j+1])
			}
		}
	}
	var out strings.Builder
	for i, j := 0, 0; i < len(w) || j < len(g); {
		switch {
		case i < len(w) && j < len(g) && w[i] == g[j]:
			i, j = i+1, j+1
		case i < len(w) && (j == len(g) || common[i+1][j] >= common[i][j+1]):
			fmt.Fprintf(&out, "-%d: %s\n", i+1, w[i])
			i++
		default:
			fmt.Fprintf(&out, "+%d: %s\n", j+1, g[j])
			j++
		}
	}
	return out.String()
}

// readsBack decodes a golden strictly into the shape this build encodes and
// requires the re-encoding to be the golden's exact compact bytes, so a golden
// is only ever one this build would itself send.
func readsBack[T interface{ Canonical() ([]byte, error) }](t *testing.T, name string) {
	t.Helper()
	golden, err := os.ReadFile(filepath.Join("testdata", name+".golden"))
	if err != nil {
		t.Fatal(err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, golden); err != nil {
		t.Fatal(err)
	}
	var decoded T
	decoder := json.NewDecoder(bytes.NewReader(compact.Bytes()))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		t.Fatalf("%s no longer decodes: %v", name, err)
	}
	canonical, err := decoded.Canonical()
	if err != nil || !bytes.Equal(canonical, compact.Bytes()) {
		t.Fatalf("%s does not read back as itself (%v):\n%s", name, err, canonical)
	}
}

// goldenCatalog adds rack and zero, two more physical Machines behind the
// controller host, so one survey reads several controllers, and declares each
// named Machine's controller trust as given.
func goldenCatalog(tls map[string]api.Value) api.Catalog {
	objects := catalog().Objects()
	for index, name := range []string{"rack", "zero"} {
		objects = append(objects, object(api.Machine, name, m("os", m("provided", true), "network", addresses(fmt.Sprintf("192.0.2.%d/24", 31+index)),
			"hardware", m("management", m("bmc", m("address", "https://"+name+".example.test/redfish/v1/Systems/7",
				"credentialsRef", name+"-bmc"))))))
	}
	for index, candidate := range objects {
		if value, ok := tls[candidate.Name()]; ok && candidate.Kind() == api.Machine {
			objects[index] = candidate.WithSpec(candidate.Spec().WithPath(value, "hardware", "management", "bmc", "tls"))
		}
	}
	return api.NewCatalog(objects)
}

// A power request and a reading survey are what the adapter validates before
// it acts, so their exact bytes are pinned, and each golden reads back as
// itself.
func TestPowerRequestsMatchTheirGoldens(t *testing.T) {
	bundled := map[string]api.Value{"metal": m("verify", true, "trustBundleRef", "metal-bmc-ca")}
	for name, catalog := range map[string]api.Catalog{
		"request-power": goldenCatalog(nil), "request-power-bundle": goldenCatalog(bundled),
	} {
		t.Run(name, func(t *testing.T) {
			request, _, err := requestFor(catalog, "lab", "metal", Stop, false, nil)
			if err != nil {
				t.Fatal(err)
			}
			canonical, err := request.Canonical()
			if err != nil {
				t.Fatal(err)
			}
			matchesGolden(t, name, canonical, false)
			readsBack[Request](t, name)
		})
	}
	bundled["rack"] = m("verify", true, "trustBundleRef", "rack-bmc-ca")
	for name, test := range map[string]struct {
		catalog api.Catalog
		names   []string
	}{
		"survey-read":         {goldenCatalog(nil), []string{"metal", "rack"}},
		"survey-read-bundles": {goldenCatalog(bundled), []string{"metal", "rack", "zero"}},
	} {
		t.Run(name, func(t *testing.T) {
			surveys, err := readSurveysFor(test.catalog, "lab", test.names, nil)
			if err != nil || len(surveys) != 1 || len(surveys[0].Targets) != len(test.names) {
				t.Fatalf("surveys = %+v (%v), want one host reading %d controllers", surveys, err, len(test.names))
			}
			canonical, err := surveys[0].Canonical()
			if err != nil {
				t.Fatal(err)
			}
			matchesGolden(t, name, canonical, false)
			readsBack[ReadSurvey](t, name)
		})
	}
}
