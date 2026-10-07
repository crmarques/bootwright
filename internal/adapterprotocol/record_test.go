package adapterprotocol

import (
	"slices"
	"strings"
	"testing"
)

const sha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func refuses(t *testing.T, admission Admission, lines ...string) {
	t.Helper()
	for _, line := range lines {
		if record, err := Decode([]byte(line), admission); err == nil {
			t.Errorf("accepted %q as %+v", line, record)
		}
	}
}

func accepts(t *testing.T, admission Admission, lines ...string) []Record {
	t.Helper()
	var records []Record
	for _, line := range lines {
		record, err := Decode([]byte(line), admission)
		if err != nil {
			t.Errorf("refused %q: %v", line, err)
		}
		records = append(records, record)
	}
	return records
}

func TestDecodeRefusesCaseVariantDuplicateMembers(t *testing.T) {
	for _, admission := range []Admission{Lifecycle, Controller} {
		refuses(t, admission,
			`{"phase":"loaded","Phase":"loaded"}`,
			`{"Phase":"loaded","phase":"loaded"}`,
			`{"phase":"loaded","phase":"loaded"}`,
			`{"phase":"loaded","phase":"continue"}`,
			`{"evidence":{"A":1,"a":2},"outcome":"changed","phase":"completed"}`,
			`{"evidence":{"x":{"Key":1,"kEY":2}},"outcome":"changed","phase":"completed"}`,
		)
	}
	refuses(t, Controller, `{"phase":"prepared","preparation":{"addedSources":[],"inventorySHA256":"x","inventorySHA256":"y"}}`)
}

func TestDecodeRefusesNullOrNonObjectEvidence(t *testing.T) {
	for _, evidence := range []string{`null`, `"x"`, `1`, `[]`, `[{"a":1}]`, `{}`, `true`} {
		for _, admission := range []Admission{Lifecycle, Controller} {
			refuses(t, admission, `{"evidence":`+evidence+`,"outcome":"changed","phase":"completed"}`)
		}
	}
	accepts(t, Lifecycle, `{"evidence":{"absent":false},"outcome":"changed","phase":"completed"}`)
}

func TestDecodeRefusesNonCanonicalSpacing(t *testing.T) {
	for _, line := range []string{
		`{"phase": "loaded"}`,
		`{"phase" :"loaded"}`,
		`{"group":"boot", "phase":"group","status":"running"}`,
		` {"phase":"loaded"}`,
		`{"phase":"loaded"} `,
		"{\"phase\":\t\"loaded\"}",
		"{\"phase\":\"loaded\"}\r",
		"{\"phase\":\"loaded\"}\n",
		`{"evidence":{"a": 1},"outcome":"changed","phase":"completed"}`,
		`{"evidence":{"a":[1, 2]},"outcome":"changed","phase":"completed"}`,
		`{"phase":"loaded"} {"phase":"loaded"}`,
	} {
		refuses(t, Lifecycle, line)
	}
	records := accepts(t, Lifecycle,
		`{"group":"pull the image","phase":"group","status":"running"}`,
		`{"evidence":{"a":"one, two: \"three\" \\ four"},"outcome":"changed","phase":"completed"}`,
	)
	if len(records) != 2 || records[0].Group != "pull the image" {
		t.Fatalf("records = %+v, want a group whose name holds spaces", records)
	}
}

func TestDecodeRefusesUnsortedMembersAndNonASCII(t *testing.T) {
	refuses(t, Lifecycle,
		`{"phase":"group","group":"boot","status":"running"}`,
		`{"group":"boot","status":"running","phase":"group"}`,
		`{"outcome":"changed","evidence":{"a":1},"phase":"completed"}`,
		`{"evidence":{"b":1,"a":2},"outcome":"changed","phase":"completed"}`,
		`{"evidence":{"a":[{"z":1,"y":2}]},"outcome":"changed","phase":"completed"}`,
		`{"evidence":{"a":"caf`+"\u00e9"+`"},"outcome":"changed","phase":"completed"}`,
		`{"evidence":{"a":"`+"\x7f"+`"},"outcome":"changed","phase":"completed"}`,
		`{"evidence":{"a":"`+"\x01"+`"},"outcome":"changed","phase":"completed"}`,
	)
	refuses(t, Controller, `{"phase":"refused","source":"tool-helm","reason":"timeout"}`)
	accepts(t, Lifecycle,
		`{"evidence":{"A":1,"B":2,"c":3},"outcome":"changed","phase":"completed"}`,
		`{"evidence":{"a":"caf\u00e9"},"outcome":"changed","phase":"completed"}`,
	)
}

func TestDecodeAdmitsEachRunnersPhasesOnly(t *testing.T) {
	refuses(t, Lifecycle,
		`{"phase":"prepared","preparation":{"addedSources":[],"inventorySHA256":"`+sha+`"}}`,
		`{"phase":"native"}`,
		`{"phase":"continue"}`,
		`{"phase":"refused","reason":"timeout","source":"tool-helm"}`,
		`{"phase":"unknown"}`,
		`{"phase":1}`,
		`{"phase":null}`,
	)
	refuses(t, Controller,
		`{"group":"boot","phase":"group","status":"running"}`,
		`{"phase":"unknown"}`,
	)
	lifecycle := accepts(t, Lifecycle,
		`{"phase":"loaded"}`,
		`{"group":"pull-image","phase":"group","status":"running"}`,
		`{"phase":"refused","reason":"identity-mismatch"}`,
		`{"evidence":{"absent":false},"outcome":"changed","phase":"completed"}`,
	)
	controller := accepts(t, Controller,
		`{"phase":"loaded"}`,
		`{"phase":"prepared","preparation":{"addedSources":[],"inventorySHA256":"`+sha+`"}}`,
		`{"phase":"native"}`,
		`{"phase":"continue"}`,
		`{"phase":"refused","reason":"release-stamp"}`,
		`{"phase":"refused","reason":"timeout","source":"tool-helm"}`,
		`{"evidence":{"absent":false},"outcome":"unchanged","phase":"completed"}`,
	)
	phases := func(records []Record) []string {
		var names []string
		for _, record := range records {
			names = append(names, record.Phase)
		}
		return names
	}
	if got := phases(lifecycle); !slices.Equal(got, []string{"loaded", "group", "refused", "completed"}) {
		t.Fatalf("lifecycle phases = %v", got)
	}
	if got := phases(controller); !slices.Equal(got, []string{"loaded", "prepared", "native", "continue", "refused", "refused", "completed"}) {
		t.Fatalf("controller phases = %v", got)
	}
	if controller[5].Source != "tool-helm" || controller[5].Reason != "timeout" || controller[4].Source != "" {
		t.Fatalf("controller refusals = %+v and %+v", controller[4], controller[5])
	}
}

func TestDecodeHoldsEachPhaseToItsExactFields(t *testing.T) {
	refuses(t, Lifecycle,
		`{"evidence":{"a":1},"group":"boot","outcome":"changed","phase":"completed"}`,
		`{"group":"boot","phase":"group","status":"bogus"}`,
		`{"group":"boot","phase":"group","status":""}`,
		`{"group":"","phase":"group","status":"running"}`,
		`{"group":1,"phase":"group","status":"running"}`,
		`{"phase":"refused","reason":""}`,
		`{"extra":1,"phase":"loaded"}`,
		`{"evidence":{"a":1},"phase":"loaded"}`,
		`{"phase":"loaded","reason":"identity-mismatch"}`,
		`{"group":"pull-image","phase":"group"}`,
		`{"group":"pull-image","phase":"group","reason":"identity-mismatch","status":"running"}`,
		`{"evidence":{"a":1},"outcome":"done","phase":"completed"}`,
		`{"outcome":"changed","phase":"completed"}`,
		`{"evidence":{"a":1},"outcome":"changed","phase":"completed","reason":"identity-mismatch"}`,
		`{"phase":"refused"}`,
		`{"outcome":"changed","phase":"refused","reason":"identity-mismatch"}`,
		`{"group":"read-state","phase":"refused","reason":"identity-mismatch"}`,
		`not json`,
		`{"phase":"loaded"}}`,
		`{"phase":"loaded"}]`,
		`{"phase":"loaded"}{"phase":"loaded"}`,
		`["phase","loaded"]`,
		`"loaded"`,
		`{}`,
		``,
	)
	refuses(t, Controller,
		`{"evidence":{},"phase":"continue"}`,
		`{"outcome":"changed","phase":"prepared","preparation":{"addedSources":null,"inventorySHA256":"x"}}`,
		`{"phase":"prepared","preparation":null}`,
		`{"phase":"prepared","preparation":[]}`,
		`{"phase":"prepared"}`,
		`{"phase":"refused","reason":"timeout","source":""}`,
		`{"phase":"refused","reason":"release-stamp","outcome":"changed"}`,
	)
	nested := `{"evidence":` + strings.Repeat(`{"a":`, 15) + `1` + strings.Repeat(`}`, 15) + `,"outcome":"changed","phase":"completed"}`
	accepts(t, Lifecycle, nested)
	refuses(t, Lifecycle, `{"evidence":`+strings.Repeat(`{"a":`, 16)+`1`+strings.Repeat(`}`, 16)+`,"outcome":"changed","phase":"completed"}`)
}

func read(input string, admission Admission) ([]Record, error) {
	out := make(chan Record, 256)
	err := Read(strings.NewReader(input), admission, nil, out)
	close(out)
	var records []Record
	for record := range out {
		records = append(records, record)
	}
	return records, err
}

func TestReadIsBoundedInRecordsAndLength(t *testing.T) {
	for _, bound := range []struct {
		admission Admission
		records   int
	}{{Lifecycle, 64}, {Controller, 132}} {
		if records, err := read(strings.Repeat("{\"phase\":\"loaded\"}\n", bound.records), bound.admission); err != nil || len(records) != bound.records {
			t.Fatalf("%d records read %d (%v)", bound.records, len(records), err)
		}
		if _, err := read(strings.Repeat("{\"phase\":\"loaded\"}\n", bound.records+1), bound.admission); err == nil {
			t.Fatalf("record %d was read", bound.records+1)
		}
	}
	prefix, suffix := `{"evidence":{"a":"`, `"},"outcome":"changed","phase":"completed"}`
	padded := func(length int) string {
		return prefix + strings.Repeat("x", length-len(prefix)-len(suffix)) + suffix
	}
	if records, err := read(padded(MaxLine-1)+"\n", Lifecycle); err != nil || len(records) != 1 {
		t.Fatalf("a record of 64 KiB with its newline was refused: %d (%v)", len(records), err)
	}
	if _, err := read(padded(MaxLine)+"\n", Lifecycle); err == nil {
		t.Fatal("a record of 64 KiB without room for its newline was read")
	}
	if _, err := read(strings.Repeat("x", MaxLine), Controller); err == nil {
		t.Fatal("an unterminated line of 64 KiB was read")
	}
	if _, err := read("{\"phase\":\"loaded\"}\r\n", Lifecycle); err == nil {
		t.Fatal("a record ending in a carriage return was read")
	}
}

func TestReadEndsOnTheConsumersShapeRefusal(t *testing.T) {
	out := make(chan Record, 4)
	refused := Read(strings.NewReader("{\"phase\":\"loaded\"}\n{\"phase\":\"native\"}\n{\"phase\":\"continue\"}\n"), Controller, func(record Record) error {
		if record.Phase == "native" {
			return errRecord
		}
		return nil
	}, out)
	close(out)
	if refused == nil || len(out) != 1 {
		t.Fatalf("the read ended with %v after %d records, want the shape's refusal after one", refused, len(out))
	}
}

// Each line is what controller_channel.canonical writes for one record:
// json.dumps with sort_keys, compact separators and ensure_ascii.
func TestDecodeAcceptsWhatThePluginsEmit(t *testing.T) {
	accepts(t, Lifecycle,
		`{"phase":"loaded"}`,
		`{"group":"pull-image","phase":"group","status":"running"}`,
		`{"evidence":{"absent":false,"answers":[{"address":"192.0.2.10","answer":"artifacts.example.test","port":53}],"container":"bootwright-dns","contentRoot":true,"postcondition":true,"request":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","startedAfterFiles":true,"unit":"active"},"outcome":"changed","phase":"completed"}`,
		`{"phase":"refused","reason":"identity-mismatch"}`,
	)
	records := accepts(t, Controller,
		`{"phase":"loaded"}`,
		`{"phase":"prepared","preparation":{"addedSources":["native-one"],"afterInventorySHA256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","inventorySHA256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","planDigest":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","transitionsSHA256":"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"}}`,
		`{"phase":"native"}`,
		`{"phase":"continue"}`,
		`{"phase":"refused","reason":"timeout","source":"tool-helm"}`,
		`{"evidence":{"added":[],"after":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","before":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","note":"caf\u00e9 <b>","planDigest":"","postcondition":true,"request":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","tools":[]},"outcome":"unchanged","phase":"completed"}`,
	)
	if len(records) != 6 || records[5].Outcome != "unchanged" || !strings.Contains(string(records[5].Evidence), `"note":"caf\u00e9 <b>"`) {
		t.Fatalf("records = %+v, want the evidence bytes as written", records)
	}
}
