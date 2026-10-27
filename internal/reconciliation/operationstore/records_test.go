package operationstore

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// A plan an earlier build froze, with null where a block carried no
// dependencies or consumed authorizations, is a canonical record of that
// build. Its digest is not one this build's rebuild produces, so it refuses
// and names the build to destroy the context with, rather than reading as
// some other plan.
func TestAPlanAnEarlierBuildFrozeWithNullListsRefuses(t *testing.T) {
	const earlierDigest = "d16cc2fde0e94d06ff8101ceeaca39ead28be6b5d9868b4d51cb5fae93c0c7cc"
	ctx := context.Background()
	plan := goldenPlan(t)
	store, area := newStore(t)
	if _, err := store.Index(ctx); err != nil {
		t.Fatal(err)
	}
	operation := testOperation(t, plan)
	if err := store.Register(ctx, operation, plan); err != nil {
		t.Fatal(err)
	}
	indented, err := os.ReadFile(filepath.Join("testdata", "plan-apply.golden"))
	if err != nil {
		t.Fatal(err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, indented); err != nil {
		t.Fatal(err)
	}
	earlier := append(bytes.ReplaceAll(compact.Bytes(), []byte(`:[]`), []byte(`:null`)), '\n')
	var decoded reconciliation.Plan
	if err := decode(earlier, MaxPlanBytes, &decoded); err != nil {
		t.Fatalf("the earlier encoding is not a canonical record: %v", err)
	}
	if digest, err := decoded.Digest(); err != nil || digest != earlierDigest {
		t.Fatalf("the earlier encoding digests as %s (%v), not as the earlier build froze it", digest, err)
	}
	current, err := plan.Digest()
	if err != nil {
		t.Fatal(err)
	}
	area.files[operation.ID+"/plan.json"] = earlier
	record := area.files[operation.ID+"/operation.json"]
	area.files[operation.ID+"/operation.json"] = bytes.Replace(record, []byte(current), []byte(earlierDigest), 1)
	_, err = New(area, fixedClock()).ReadPlan(ctx, operation.ID)
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "lifecycle.state" || reported[0].Source != nil ||
		reported[0].Message != "the frozen plan is not a plan this executable could have produced" ||
		reported[0].Remediation != "destroy this context with the build that registered the operation, which its operation.json records" {
		t.Fatalf("an earlier build's plan read as %+v", reported)
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
		if len(reported) != 1 || reported[0].Code != "lifecycle.state" || reported[0].Message != tc.message {
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
		data, err := encode(Attempt{Version: 1, Block: "alpha", Number: 1, Phase: "running", Build: build,
			Started: "2026-10-26T00:00:00Z", Updated: "2026-10-26T00:00:00Z"}, MaxAttemptBytes)
		if err != nil {
			t.Fatal(err)
		}
		return bytes.Replace(data, []byte("{"), []byte(`{"later":true,`+member), 1)
	}
	plain := "lifecycle record is malformed or unsupported"
	for name, tc := range map[string]struct {
		build                *Build
		member               string
		running              *Executable
		message, remediation string
	}{
		"a named build": {named, "", nil, plain + `; build "v0.9.0" at commit "abc123" wrote it`,
			`continue or remove this operation with build "v0.9.0" at commit "abc123", which reads the record`},
		"another running build": {named, "", &Executable{Version: "v1.0.0", Commit: "def456"}, plain + `; build "v0.9.0" at commit "abc123" wrote it`,
			`continue or remove this operation with build "v0.9.0" at commit "abc123", which reads the record`},
		"the running build": {named, "", &Executable{Version: "v0.9.0", Commit: "abc123"}, plain + `; it names build "v0.9.0" at commit "abc123", this executable`, ""},
		"an unidentified build": {&Build{AutomationDigest: digest, ContentDigest: digest}, "", &Executable{},
			plain + "; the build with automation digest " + digest + " wrote it",
			"continue or remove this operation with the build with automation digest " + digest + ", which reads the record"},
		"an invalid build":       {&Build{AutomationDigest: "x", ContentDigest: digest}, "", nil, plain, ""},
		"no build":               {nil, "", nil, plain, ""},
		"a case-variant member":  {nil, `"Build":{"automationDigest":"` + digest + `","contentDigest":"` + digest + `","executable":{"version":"v0.9.0","commit":"abc123"}},`, nil, plain, ""},
		"a second build member":  {named, `"build":{"automationDigest":"` + digest + `","contentDigest":"` + digest + `","executable":{"version":"v0.8.0","commit":"abc"}},`, nil, plain, ""},
		"a non-canonical member": {nil, `"build":{"contentDigest":"` + digest + `","automationDigest":"` + digest + `","executable":{"version":"v0.9.0","commit":"abc123"}},`, nil, plain, ""},
	} {
		var record Attempt
		reported := diagnostics.Of(decodeUnder(written(tc.build, tc.member), MaxAttemptBytes, &record, tc.running))
		if len(reported) != 1 || reported[0].Code != "lifecycle.state" || reported[0].Message != tc.message || reported[0].Remediation != tc.remediation {
			t.Errorf("%s: refusal = %+v, want %q remedied by %q", name, reported, tc.message, tc.remediation)
		}
	}
}
