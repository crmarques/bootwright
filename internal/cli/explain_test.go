package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

var explainFixture = []diagnostics.Stop{
	{Code: "runner.deadline", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyUnproved, Cause: diagnostics.CauseDeadline,
		Recovery: diagnostics.RecoveryRepeat, Meaning: "the adapter run reached its deadline before it ended", Remedy: "repeat the operation"},
	{Code: "destroy.in-use", Level: diagnostics.LevelRemoval, Certainty: diagnostics.CertaintyNone, Cause: diagnostics.CauseTarget,
		Recovery: diagnostics.RecoveryFixThenRepeat, Meaning: "a removal would take back state that is still in use", Remedy: "stop what the message names"},
}

func explain(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := New(Config{Out: &out, ErrOut: &errOut, Stops: func() []diagnostics.Stop { return explainFixture }}).Run(context.Background(), append([]string{"explain"}, args...))
	return code, out.String(), errOut.String()
}

func TestExplainPrintsOneStopsRow(t *testing.T) {
	code, out, _ := explain(t, "--code", "runner.deadline")
	for _, want := range []string{"runner.deadline", "Certainty     unproved", "Cause         deadline", "Repeat helps  yes", "Remedy        repeat the operation"} {
		if code != 0 || !strings.Contains(out, want) {
			t.Fatalf("explain exited %d and printed\n%s\nwithout %q", code, out, want)
		}
	}
}

func TestExplainListsEveryStopInCodeOrder(t *testing.T) {
	code, out, _ := explain(t, "--output", "json")
	var envelope struct {
		Result explainPresentation `json:"result"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &envelope) != nil {
		t.Fatalf("explain exited %d and printed %s", code, out)
	}
	stops := envelope.Result.Stops
	if len(stops) != 2 || stops[0].Code != "destroy.in-use" || stops[0].Repeat || stops[1].Code != "runner.deadline" || !stops[1].Repeat {
		t.Fatalf("explain listed %+v", stops)
	}
}

func TestExplainRefusesAnUnknownCode(t *testing.T) {
	code, _, errOut := explain(t, "--code", "runner.nope")
	if code != 2 || !strings.Contains(errOut, "no stop is named runner.nope; bootwright explain lists every stop") {
		t.Fatalf("explain exited %d and reported %s", code, errOut)
	}
}
