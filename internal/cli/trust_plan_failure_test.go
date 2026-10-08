package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// A trust plan that could not reach standard output ends the command with its
// own refusal, naming the context and the repeat, rather than an unsupported
// result.
func TestAnUnwrittenTrustPlanEndsWithItsOwnRefusal(t *testing.T) {
	err := NewTrustPlanPresenter(failingWriter{}).PresentTrustPlan(context.Background(), *trustReport())
	record := &dispatchRecord{err: err}
	var out, errOut bytes.Buffer
	code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(context.Background(),
		[]string{"machine", "trust", "--context", "lab"})
	if code != 1 {
		t.Fatalf("exit = %d, stderr %q", code, errOut.String())
	}
	if stderr := errOut.String(); !strings.Contains(stderr, "trust.identity") ||
		!strings.Contains(stderr, "bootwright machine trust --context lab") || strings.Contains(stderr, "runtime.internal") {
		t.Fatalf("stderr = %q", stderr)
	}
}
