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
