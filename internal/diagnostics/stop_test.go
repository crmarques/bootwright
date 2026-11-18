package diagnostics

import (
	"encoding/json"
	"testing"
)

func TestAStopFiresWithItsClassesAndRemedy(t *testing.T) {
	stop := Stop{Code: "runner.deadline", Level: LevelAdapterRun, Certainty: CertaintyUnproved, Cause: CauseDeadline, Recovery: RecoveryRepeat,
		Meaning: "the run reached its deadline", Remedy: "repeat the operation"}
	reported := Of(stop.Fail("the run of block a reached its deadline", ""))
	data, err := json.Marshal(reported)
	want := `[{"severity":"error","code":"runner.deadline","message":"the run of block a reached its deadline","remediation":"repeat the operation","cause":"deadline","recovery":"repeat"}]`
	if err != nil || string(data) != want {
		t.Fatalf("a stop fires as %s (%v)", data, err)
	}
	if !Unproved(stop.Fail("x", "")) || Unproved(Stop{Certainty: CertaintyPossible}.Fail("x", "")) || Unproved(NewFailure("lifecycle.state", "x", "")) || !Unproved(NewFailure("lifecycle.unknown", "x", "")) {
		t.Fatal("Unproved does not follow the certainty")
	}
	if named := Of(stop.Fail("x", "do this")); named[0].Remediation != "do this" {
		t.Fatalf("a raiser's remedy was replaced: %+v", named)
	}
}
