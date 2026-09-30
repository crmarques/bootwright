package lifecycle

import (
	"testing"
	"time"
)

// A capability states its run's deadline only through its invocation, and the
// request carries exactly that deadline, or none when the invocation states
// none, so the runner keeps its default.
func TestRunForCarriesTheInvocationsDeadline(t *testing.T) {
	for _, deadline := range []time.Duration{0, 3*time.Hour + 5*time.Minute} {
		if got := RunFor(Execution{}, Invocation{Deadline: deadline}).Deadline; got != deadline {
			t.Fatalf("an invocation stating %s produced a request stating %s", deadline, got)
		}
	}
}

// An attempt keeps its adapter's output beside its own log, so a failure that
// output explains points there, whatever the capability asked for.
func TestRunForPointsAnAdapterFailureBesideTheAttemptLog(t *testing.T) {
	if got := RunFor(Execution{}, Invocation{}).OutputRemediation; got != "read the adapter output retained beside this attempt's log" {
		t.Fatalf("an attempt's request points an adapter failure at %q", got)
	}
}
