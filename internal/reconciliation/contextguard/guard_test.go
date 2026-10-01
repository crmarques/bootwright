package contextguard

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

// The guard grants each recognized state its disposition whether the record
// is the canonical form Bytes publishes or another spelling of the same
// members.
func TestMutationEvidence(t *testing.T) {
	for _, operation := range []string{"none", "pending", "failed", "unknown", "applied"} {
		for _, ownership := range []string{"none", "retained"} {
			t.Run(operation+"/"+ownership, func(t *testing.T) {
				canonical, err := reconciliation.Evidence{Operation: reconciliation.MutationOperation(operation), Ownership: reconciliation.MutationOwnership(ownership)}.Bytes()
				if err != nil {
					t.Fatal(err)
				}
				for _, data := range [][]byte{
					canonical,
					[]byte(`{"version":1,"operation":"` + operation + `","ownership":"` + ownership + `"}`),
					[]byte(" {\"ownership\" : \"" + ownership + "\",\n\t\"operation\":\"" + operation + "\", \"version\":1}\r\n"),
				} {
					got, err := (Guard{}).Check(context.Background(), data)
					if err != nil {
						t.Fatalf("%q: %v", data, err)
					}
					dispose := operation == "none" && ownership == "none"
					if got.Dispose != dispose || got.Update != (operation == "none" || operation == "applied") {
						t.Fatalf("incorrect disposition of %q: %+v", data, got)
					}
				}
			})
		}
	}
}

func TestEvidenceRefusesUnknownAndAmbiguousRecords(t *testing.T) {
	for _, data := range []string{"", `null`, `{}`, `{"version":1,"operation":"none"}`, `{"version":1,"operation":"none","ownership":"none","extra":0}`, `{"version":1,"operation":"none","ownership":"none","operation":"failed"}`, `{"version":1.0,"operation":"none","ownership":"none"}`, `{"version":2,"operation":"none","ownership":"none"}`, `{"version":1,"operation":"future","ownership":"none"}`, `{"version":1,"operation":"none","ownership":null}`, `{"version":1,"operation":"none","ownership":"none"}{}`, `{"version":1,"operation":"none","ownership":"none"}}`, `{"version":1,"operation":"none","ownership":"none"} x`, `{"version":1,"operation":"none","ownership":"none"}` + strings.Repeat(" ", 65536)} {
		if _, err := (Guard{}).Check(context.Background(), []byte(data)); err == nil {
			t.Fatalf("admitted ambiguous evidence of %d bytes: %.120q", len(data), data)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (Guard{}).Check(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}
