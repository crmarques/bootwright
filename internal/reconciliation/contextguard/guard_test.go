package contextguard

import (
	"context"
	"errors"
	"testing"
)

func TestMutationEvidence(t *testing.T) {
	for _, operation := range []string{"none", "pending", "failed", "unknown", "applied"} {
		for _, ownership := range []string{"none", "retained"} {
			t.Run(operation+"/"+ownership, func(t *testing.T) {
				data := []byte(`{"version":1,"operation":"` + operation + `","ownership":"` + ownership + `"}`)
				got, err := (Guard{}).Check(context.Background(), data)
				if err != nil {
					t.Fatal(err)
				}
				dispose := operation == "none" && ownership == "none"
				if got.Dispose != dispose || got.Recovery == dispose || got.Update != (operation == "none" || operation == "applied") {
					t.Fatalf("incorrect disposition: %+v", got)
				}
			})
		}
	}
}

func TestEvidenceRefusesUnknownAndAmbiguousRecords(t *testing.T) {
	for _, data := range []string{"", `null`, `{}`, `{"version":1,"operation":"none"}`, `{"version":1,"operation":"none","ownership":"none","extra":0}`, `{"version":1,"operation":"none","ownership":"none","operation":"failed"}`, `{"version":1.0,"operation":"none","ownership":"none"}`, `{"version":2,"operation":"none","ownership":"none"}`, `{"version":1,"operation":"future","ownership":"none"}`, `{"version":1,"operation":"none","ownership":null}`, `{"version":1,"operation":"none","ownership":"none"}{}`} {
		if _, err := (Guard{}).Check(context.Background(), []byte(data)); err == nil {
			t.Fatalf("admitted ambiguous evidence: %s", data)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (Guard{}).Check(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}
