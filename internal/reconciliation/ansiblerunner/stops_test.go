package ansiblerunner

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

var stopCode = regexp.MustCompile(`^[a-z]+(\.[a-z0-9-]+)+$`)

// Every stop the runner declares is a row of the stop catalog, field by field,
// and the catalog's adapter run section lists no other.
func TestTheRunnersStopsAreTheCatalogsAdapterRunStops(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "specs", "cli", "stops.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, section, _ := strings.Cut(string(data), "\n## Adapter run stops\n")
	section, _, _ = strings.Cut(section, "\n## ")
	section, _, _ = strings.Cut(section, "\n### ")
	rows := map[string][]string{}
	for _, line := range strings.Split(section, "\n") {
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		for i := range cells {
			cells[i] = strings.Trim(strings.TrimSpace(cells[i]), "`")
		}
		rows[cells[0]] = cells
	}
	if len(rows) != len(Stops()) {
		t.Fatalf("the catalog lists %d adapter run stops, the runner declares %d", len(rows), len(Stops()))
	}
	for _, stop := range Stops() {
		fault := string(stop.Fault)
		if fault == "" {
			fault = "-"
		}
		want := []string{stop.Code, string(stop.Level), string(stop.Certainty), string(stop.Cause), fault, string(stop.Recovery), stop.Meaning, stop.Remedy}
		if got := rows[stop.Code]; strings.Join(got, "\x00") != strings.Join(want, "\x00") {
			t.Errorf("stop %s\n declared %q\n catalog  %q", stop.Code, want, got)
		}
		if !stopCode.MatchString(stop.Code) || len(stop.Code) > 64 || stop.Meaning == "" || stop.Remedy == "" {
			t.Errorf("stop %s is malformed", stop.Code)
		}
		if failed := diagnostics.Of(stop.Fail("x", "")); len(failed) != 1 || failed[0].Certainty != stop.Certainty || failed[0].Remediation != stop.Remedy {
			t.Errorf("stop %s fires as %+v", stop.Code, failed)
		}
		outcome := reconciliation.OutcomeFailed
		if stop.Certainty == diagnostics.CertaintyUnproved {
			outcome = reconciliation.OutcomeUnknown
		}
		if got := lifecycle.AttemptOutcome(stop.Fail("x", "")); got != outcome {
			t.Errorf("stop %s of certainty %s leaves its attempt %s, want %s", stop.Code, stop.Certainty, got, outcome)
		}
	}
}
