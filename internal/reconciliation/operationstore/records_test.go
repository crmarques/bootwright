package operationstore

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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
	const earlierDigest = "e6b9211062141ac0a9b1138c193236e374a804192aaa3b76ec7e27cbd6ae9a05"
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
