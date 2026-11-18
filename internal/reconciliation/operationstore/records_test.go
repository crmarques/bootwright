package operationstore

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestUnreadableOperationNamesItsRegisteringBuildWithoutAdoptingTheRecord(t *testing.T) {
	plan := testPlan(t, "alpha")
	operation := testOperation(t, plan)
	data, err := encode(operation, MaxOperationBytes)
	if err != nil {
		t.Fatal(err)
	}
	// Removing a required member reproduces a record from a different format,
	// without keeping a reader or fixture for that format.
	data = bytes.Replace(data, []byte(`"contracts":{"artifact-server-nginx-v1":"`+strings.Repeat("4", 64)+`"},`), nil, 1)
	for name, running := range map[string]Executable{
		"another build":         {Version: "devel", Commit: "fedcba1"},
		"the registering build": operation.Executable,
	} {
		t.Run(name, func(t *testing.T) {
			store, area := newStore(t)
			store = store.Running(running)
			recordPath := operation.ID + "/operation.json"
			area.files[recordPath] = bytes.Clone(data)
			_, err := store.ReadOperation(context.Background(), operation.ID)
			reported := diagnostics.Of(err)
			if len(reported) != 1 || reported[0].Code != "operations.record-unreadable" || !strings.Contains(reported[0].Message, `build "devel" at commit "abcdef1"`) {
				t.Fatalf("refusal does not name the registering build: %+v", reported)
			}
			remedy := reported[0].Remediation
			if remedy == "" || strings.Contains(remedy, "bootwright status") {
				t.Fatalf("unreadable record sends the operator back to status: %q", remedy)
			}
			if running == operation.Executable {
				if !strings.Contains(remedy, "backup") {
					t.Fatalf("the registering build refuses without a backup remedy: %q", remedy)
				}
			} else if !strings.Contains(remedy, `build "devel" at commit "abcdef1"`) {
				t.Fatalf("remedy does not name the registering build: %q", remedy)
			}
			if !bytes.Equal(area.files[recordPath], data) || len(area.files) != 1 {
				t.Fatal("reading an unsupported record changed durable state")
			}
		})
	}
}

func TestAnUnsupportedOperationMemberStillNamesItsRegisteringBuild(t *testing.T) {
	data, err := encode(testOperation(t, testPlan(t, "alpha")), MaxOperationBytes)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.Replace(data, []byte("{"), []byte(`{"unsupported":true,`), 1)
	var operation Operation
	reported := diagnostics.Of(decodeUnder(data, MaxOperationBytes, &operation, nil))
	if len(reported) != 1 || !strings.Contains(reported[0].Message, "malformed or unsupported") || !strings.Contains(reported[0].Remediation, `build "devel" at commit "abcdef1"`) {
		t.Fatalf("unsupported operation did not name its build: %+v", reported)
	}
}

func TestUnreadableOperationDoesNotTrustAnAmbiguousExecutableHint(t *testing.T) {
	const executable = `{"version":"devel","commit":"abcdef1"}`
	for name, member := range map[string]string{
		"missing":                "",
		"empty":                  `"executable":{"version":"","commit":""},`,
		"case variant":           `"Executable":` + executable + `,`,
		"duplicate":              `"executable":` + executable + `,"executable":` + executable + `,`,
		"case variant duplicate": `"Executable":` + executable + `,"executable":` + executable + `,`,
		"noncanonical":           `"executable":{"commit":"abcdef1","version":"devel"},`,
		"oversized":              `"executable":{"version":"devel","commit":"` + strings.Repeat("a", maxIdentifier+1) + `"},`,
	} {
		t.Run(name, func(t *testing.T) {
			var operation Operation
			reported := diagnostics.Of(decodeUnder([]byte(`{`+member+`"version":1}`+"\n"), MaxOperationBytes, &operation, nil))
			if len(reported) != 1 || strings.Contains(reported[0].Message, "abcdef1") || reported[0].Remediation == "" || strings.Contains(reported[0].Remediation, "bootwright status") {
				t.Fatalf("ambiguous identity became a build hint or a status loop: %+v", reported)
			}
		})
	}
}

// A lifecycle record reads back only as the exact canonical bytes its type
// encodes, LF-terminated, and every other encoding refuses in its own words.
func TestTheLifecycleRecordRefusesEachNonCanonicalEncodingInItsOwnWords(t *testing.T) {
	const exact = `{"version":1,"current":"op-abababababababababababababababab"}` + "\n"
	var index Index
	if err := decode([]byte(exact), MaxIndexBytes, &index); err != nil {
		t.Fatalf("the canonical record refused: %v", err)
	}
	for name, tc := range map[string]struct{ data, message string }{
		"an undeclared member":  {`{"undeclared":true,"version":1,"current":"op-abababababababababababababababab"}` + "\n", "lifecycle record is malformed or unsupported"},
		"a case-variant member": {`{"Version":2,"version":1,"current":"op-abababababababababababababababab"}` + "\n", "lifecycle record is not canonical"},
		"a duplicate member":    {`{"version":2,"version":1,"current":"op-abababababababababababababababab"}` + "\n", "lifecycle record is not canonical"},
		"trailing data":         {exact + "x", "lifecycle record contains trailing data"},
		"a space after a colon": {`{"version": 1,"current":"op-abababababababababababababababab"}` + "\n", "lifecycle record is not canonical"},
		"a missing line feed":   {exact[:len(exact)-1], "lifecycle record is not canonical"},
		"a doubled line feed":   {exact + "\n", "lifecycle record is not canonical"},
	} {
		var index Index
		reported := diagnostics.Of(decode([]byte(tc.data), MaxIndexBytes, &index))
		if len(reported) != 1 || reported[0].Code != "operations.record-invalid" || reported[0].Message != tc.message {
			t.Errorf("%s: refusal = %+v, want %q", name, reported, tc.message)
		}
	}
}

func TestPlanAndRemovalRecordEnvelopesFitTheReservedBytes(t *testing.T) {
	if needed := 2*MaxPlanBytes + 2*MaxOperationBytes + MaxIndexBytes; needed > ReservedBytes {
		t.Fatalf("apply and removal record envelopes need %d bytes, reserve holds %d", needed, ReservedBytes)
	}
}

// A record a later build wrote with a member this build does not know still
// refuses, and names the build its own build member names when exactly one
// member spelled build holds a valid build in its canonical encoding, so the
// remedy is the build that can read it, unless that build is this executable.
func TestARecordWithAnUnknownMemberNamesTheBuildThatWroteIt(t *testing.T) {
	digest := strings.Repeat("ab", 32)
	named := &Build{AutomationDigest: digest, ContentDigest: digest, Executable: Executable{Version: "v0.9.0", Commit: "abc123"}}
	written := func(build *Build, member string) []byte {
		var recorded Build
		if build != nil {
			recorded = *build
		}
		data, err := encode(Attempt{Version: 1, Block: "alpha", Number: 1, Phase: "running", Build: recorded,
			Started: "2026-10-26T00:00:00Z", Updated: "2026-10-26T00:00:00Z"}, MaxAttemptBytes)
		if err != nil {
			t.Fatal(err)
		}
		return bytes.Replace(data, []byte("{"), []byte(`{"later":true,`+member), 1)
	}
	plain := "lifecycle record is malformed or unsupported"
	invalid, otherBuild := operationsRecordInvalid.Code, operationsRecordOtherBuild.Code
	for name, tc := range map[string]struct {
		build                *Build
		member               string
		running              *Executable
		message, remediation string
		code                 string
	}{
		"a named build": {named, "", nil, plain + `; build "v0.9.0" at commit "abc123" wrote it`,
			`continue or remove this operation with build "v0.9.0" at commit "abc123", which reads the record`, otherBuild},
		"another running build": {named, "", &Executable{Version: "v1.0.0", Commit: "def456"}, plain + `; build "v0.9.0" at commit "abc123" wrote it`,
			`continue or remove this operation with build "v0.9.0" at commit "abc123", which reads the record`, otherBuild},
		"the running build": {named, "", &Executable{Version: "v0.9.0", Commit: "abc123"}, plain + `; it names build "v0.9.0" at commit "abc123", this executable`, operationsRecordInvalid.Remedy, invalid},
		"an unidentified build": {&Build{AutomationDigest: digest, ContentDigest: digest}, "", &Executable{},
			plain + "; the build with automation digest " + digest + " wrote it",
			"continue or remove this operation with the build with automation digest " + digest + ", which reads the record", otherBuild},
		"an invalid build":       {&Build{AutomationDigest: "x", ContentDigest: digest}, "", nil, plain, operationsRecordInvalid.Remedy, invalid},
		"no build":               {nil, "", nil, plain, operationsRecordInvalid.Remedy, invalid},
		"a case-variant member":  {nil, `"Build":{"automationDigest":"` + digest + `","contentDigest":"` + digest + `","executable":{"version":"v0.9.0","commit":"abc123"}},`, nil, plain, operationsRecordInvalid.Remedy, invalid},
		"a second build member":  {named, `"build":{"automationDigest":"` + digest + `","contentDigest":"` + digest + `","executable":{"version":"v0.8.0","commit":"abc"}},`, nil, plain, operationsRecordInvalid.Remedy, invalid},
		"a non-canonical member": {nil, `"build":{"contentDigest":"` + digest + `","automationDigest":"` + digest + `","executable":{"version":"v0.9.0","commit":"abc123"}},`, nil, plain, operationsRecordInvalid.Remedy, invalid},
	} {
		var record Attempt
		reported := diagnostics.Of(decodeUnder(written(tc.build, tc.member), MaxAttemptBytes, &record, tc.running))
		if len(reported) != 1 || reported[0].Code != tc.code || reported[0].Message != tc.message || reported[0].Remediation != tc.remediation {
			t.Errorf("%s: refusal = %+v, want %s %q remedied by %q", name, reported, tc.code, tc.message, tc.remediation)
		}
	}
}
