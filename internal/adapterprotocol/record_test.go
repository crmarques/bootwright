package adapterprotocol

import (
	"encoding/json"
	"os"
	"regexp"
	"slices"
	"strconv"
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

func TestPreparedIsAdmittedForLifecycleRuns(t *testing.T) {
	records := accepts(t, Lifecycle,
		`{"phase":"loaded"}`,
		`{"phase":"prepared","preparation":{"marker":"aaaaaaaa"}}`,
	)
	if len(records) != 2 || records[1].Phase != "prepared" || string(records[1].Preparation) != `{"marker":"aaaaaaaa"}` {
		t.Fatalf("records = %+v, want the preparation as written", records)
	}
	refuses(t, Lifecycle,
		`{"phase":"prepared","preparation":"marker"}`,
		`{"phase":"prepared","preparation":{"marker":"a"},"reason":"x"}`,
		`{"phase":"prepared"}`,
	)
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

// canonicalVectors is shared with the collection's test_canonical_json.py: each
// line is the exact bytes module_utils/canonical_json.py writes for a record.
const canonicalVectors = "../../ansible/collections/ansible_collections/bootwright/core/tests/unit/goldens/canonical_json_records.json"

func TestDecodeAcceptsTheCanonicalVectorsThePythonEncoderWrites(t *testing.T) {
	data, err := os.ReadFile(canonicalVectors)
	if err != nil {
		t.Fatal(err)
	}
	var vectors []struct{ Admission, Line string }
	if err := json.Unmarshal(data, &vectors); err != nil || len(vectors) == 0 {
		t.Fatalf("read %s: %v", canonicalVectors, err)
	}
	for _, vector := range vectors {
		admission := map[string]Admission{"lifecycle": Lifecycle, "controller": Controller}[vector.Admission]
		accepts(t, admission, vector.Line)
	}
}

func TestDecodeAdmitsALifecycleRefusalDetailOfTheClosedGrammarOnly(t *testing.T) {
	records := accepts(t, Lifecycle,
		`{"detail":"native-refusal:fixture-session-expired","phase":"refused","reason":"remote-refusal"}`,
		`{"detail":"OSError@ceph_package_sources.stream:142","phase":"refused","reason":"adapter-exception"}`,
		`{"detail":"native-refusal:packages@ceph_package_sources.metadata_member:207","phase":"refused","reason":"remote-refusal"}`,
	)
	if records[0].Detail != "native-refusal:fixture-session-expired" || records[1].Reason != "adapter-exception" ||
		records[2].Detail != "native-refusal:packages@ceph_package_sources.metadata_member:207" {
		t.Fatalf("records = %+v", records)
	}
	refuses(t, Lifecycle,
		`{"detail":"","phase":"refused","reason":"remote-refusal"}`,
		`{"detail":"a value with spaces","phase":"refused","reason":"remote-refusal"}`,
		`{"detail":"`+strings.Repeat("a", 161)+`","phase":"refused","reason":"remote-refusal"}`,
		`{"detail":1,"phase":"refused","reason":"remote-refusal"}`,
		`{"detail":"cause","phase":"loaded"}`,
		`{"detail":"cause","phase":"refused","reason":"remote-refusal","source":"tool-helm"}`,
	)
	refuses(t, Controller, `{"detail":"cause","phase":"refused","reason":"timeout"}`)
}

func TestNativeErrorIsExactLifecycleRefusalData(t *testing.T) {
	line := `{"nativeError":{"cause":"package not found","coordinate":"ceph-operator-selection"},"phase":"refused","reason":"native"}`
	records := accepts(t, Lifecycle, line)
	if len(records) != 1 || records[0].NativeError == nil || records[0].NativeError.Coordinate != "ceph-operator-selection" || records[0].NativeError.Cause != "package not found" {
		t.Fatalf("%+v", records)
	}
	refuses(t, Controller, line)
	refuses(t, Lifecycle,
		`{"nativeError":{"cause":"failure","coordinate":"selected"},"phase":"loaded"}`,
		`{"evidence":{"ready":true},"nativeError":{"cause":"failure","coordinate":"selected"},"outcome":"changed","phase":"completed"}`,
		`{"nativeError":null,"phase":"refused","reason":"native"}`,
		`{"nativeError":{"cause":"failure"},"phase":"refused","reason":"native"}`,
		`{"nativeError":{"cause":"failure","coordinate":"selected","extra":"x"},"phase":"refused","reason":"native"}`,
		`{"nativeError":{"Cause":"failure","coordinate":"selected"},"phase":"refused","reason":"native"}`,
		`{"nativeError":{"coordinate":"selected","cause":"failure"},"phase":"refused","reason":"native"}`,
		`{"nativeError":{"cause":"failure","coordinate":"selected","coordinate":"other"},"phase":"refused","reason":"native"}`,
	)
}
func TestNativeErrorBoundsDecodedUTF8AndRefusesControls(t *testing.T) {
	record := func(cause, coordinate string) string {
		return `{"nativeError":{"cause":` + cause + `,"coordinate":` + coordinate + `},"phase":"refused","reason":"native"}`
	}
	accepts(t, Lifecycle, record(`"`+strings.Repeat("x", MaxNativeCause)+`"`, `"`+strings.Repeat("y", MaxNativeCoordinate)+`"`), record(`"`+strings.Repeat(`\u00e9`, MaxNativeCause/2)+`"`, `"selected"`), record(`"\ud83d\ude00"`, `"selected"`), record(`"\ufffd"`, `"selected"`))
	refuses(t, Lifecycle, record(`"`+strings.Repeat("x", MaxNativeCause+1)+`"`, `"selected"`), record(`"cause"`, `"`+strings.Repeat("x", MaxNativeCoordinate+1)+`"`), record(`"`+strings.Repeat(`\u00e9`, MaxNativeCause/2+1)+`"`, `"selected"`))
	for _, value := range []string{`""`, `"   "`, `null`, `12`, `"line\nnext"`, `"tab\tvalue"`, `"\u0000"`, `"\u001b[31m"`, `"\u007f"`, `"\u0085"`, `"\u202e"`, `"\ud800"`, `"\udc00"`, `"\ud800x"`, `"\u1c89"`, `"\ud87a\udff0"`} {
		refuses(t, Lifecycle, record(value, `"selected"`), record(`"cause"`, value))
	}
	accepts(t, Lifecycle, record(`"literal \\ud800"`, `"selected"`))
}

func TestWarningsAreBoundedRegisteredLifecycleCompletionData(t *testing.T) {
	completion := func(warnings string) string {
		return `{"evidence":{"ready":true},"outcome":"changed","phase":"completed","warnings":` + warnings + `}`
	}
	shadow := `{"code":"substrate.shadowed-route","message":"route 10.0.0.0/8 via 192.0.2.1 dev eth0 covers 10.20.0.0/24"}`
	records := accepts(t, Lifecycle,
		completion(`[`+shadow+`]`),
		completion(`[{"code":"substrate.shadowed-route","message":"m","object":{"kind":"InfraProvider","name":"lab-host.example"}}]`),
		completion(`[`+strings.TrimSuffix(strings.Repeat(shadow+",", MaxWarnings), ",")+`]`),
		completion(`[{"code":"substrate.shadowed-route","message":"`+strings.Repeat("x", MaxWarningMessage)+`"}]`),
	)
	if len(records) != 4 || len(records[0].Warnings) != 1 || records[0].Warnings[0].Code != "substrate.shadowed-route" ||
		records[0].Warnings[0].Message != "route 10.0.0.0/8 via 192.0.2.1 dev eth0 covers 10.20.0.0/24" || records[0].Warnings[0].Object != nil ||
		records[1].Warnings[0].Object == nil || *records[1].Warnings[0].Object != (WarningObject{Kind: "InfraProvider", Name: "lab-host.example"}) ||
		len(records[2].Warnings) != MaxWarnings {
		t.Fatalf("records = %+v", records)
	}
	refuses(t, Controller, completion(`[`+shadow+`]`))
	refuses(t, Lifecycle,
		`{"phase":"loaded","warnings":[`+shadow+`]}`,
		`{"phase":"refused","reason":"remote-refusal","warnings":[`+shadow+`]}`,
		completion(`[]`),
		completion(`null`),
		completion(shadow),
		completion(`[null]`),
		completion(`["substrate.shadowed-route"]`),
		completion(`[`+strings.TrimSuffix(strings.Repeat(shadow+",", MaxWarnings+1), ",")+`]`),
		completion(`[{"code":"substrate.unregistered","message":"m"}]`),
		completion(`[{"code":"SUBSTRATE.SHADOWED-ROUTE","message":"m"}]`),
		completion(`[{"code":"substrate.shadowed-route"}]`),
		completion(`[{"message":"m"}]`),
		completion(`[{"code":"substrate.shadowed-route","message":""}]`),
		completion(`[{"code":"substrate.shadowed-route","message":"   "}]`),
		completion(`[{"code":"substrate.shadowed-route","message":1}]`),
		completion(`[{"code":"substrate.shadowed-route","message":"`+strings.Repeat("x", MaxWarningMessage+1)+`"}]`),
		completion(`[{"code":"substrate.shadowed-route","message":"line\nnext"}]`),
		completion(`[{"code":"substrate.shadowed-route","message":"\u001b[31m"}]`),
		completion(`[{"code":"substrate.shadowed-route","message":"café"}]`),
		completion(`[{"code":"substrate.shadowed-route","message":"m","remedy":"r"}]`),
		completion(`[{"code":"substrate.shadowed-route","message":"m","object":null}]`),
		completion(`[{"code":"substrate.shadowed-route","message":"m","object":{"kind":"InfraProvider"}}]`),
		completion(`[{"code":"substrate.shadowed-route","message":"m","object":{"kind":"infraProvider","name":"lab"}}]`),
		completion(`[{"code":"substrate.shadowed-route","message":"m","object":{"kind":"InfraProvider","name":"Lab"}}]`),
		completion(`[{"code":"substrate.shadowed-route","message":"m","object":{"kind":"InfraProvider","name":"-lab"}}]`),
		completion(`[{"code":"substrate.shadowed-route","message":"m","object":{"kind":"InfraProvider","name":"`+strings.Repeat("a", 254)+`"}}]`),
		completion(`[{"code":"substrate.shadowed-route","message":"m","object":{"kind":"InfraProvider","name":"lab","uid":"x"}}]`),
		completion(`[{"message":"m","code":"substrate.shadowed-route"}]`),
	)
	if got := WarningCodes(); !slices.Equal(got, []string{"substrate.shadowed-route"}) {
		t.Fatalf("registered warning codes = %v", got)
	}
}

const pythonDispatch = "../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/adapter_protocol.py"

// The collection's dispatch clips warnings to exactly what this decoder
// admits, so a valid warning is never refused here and an invalid one is
// dropped there first.
func TestThePythonDispatchBoundsWarningsAsTheDecoderDoes(t *testing.T) {
	data, err := os.ReadFile(pythonDispatch)
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	constant := func(name string) string {
		match := regexp.MustCompile(`(?m)^` + name + ` = (.+)$`).FindStringSubmatch(source)
		if match == nil {
			t.Fatalf("%s declares no %s", pythonDispatch, name)
		}
		return match[1]
	}
	for name, want := range map[string]int{"MAX_WARNINGS": MaxWarnings, "MAX_WARNING_MESSAGE": MaxWarningMessage,
		"MAX_WARNING_KIND": maxWarningKind, "MAX_WARNING_NAME": maxWarningName, "MAX_LINE": MaxLine} {
		if got := constant(name); got != strconv.Itoa(want) {
			t.Errorf("%s = %s in the dispatch, %d in the decoder", name, got, want)
		}
	}
	var codes []string
	for _, quoted := range regexp.MustCompile(`"([^"]+)"`).FindAllStringSubmatch(constant("WARNING_CODES"), -1) {
		codes = append(codes, quoted[1])
	}
	if !slices.Equal(codes, warningCodes) {
		t.Errorf("WARNING_CODES = %q in the dispatch, %q in the decoder", codes, warningCodes)
	}
	if got := constant("NAME"); got != `frozenset("abcdefghijklmnopqrstuvwxyz0123456789-.")` {
		t.Errorf("the dispatch's object name alphabet is %s", got)
	}
}
