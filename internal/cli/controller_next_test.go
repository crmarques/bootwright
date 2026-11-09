package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// A ready host names creating a context as its next command, because the name
// and the input directory are the operator's to choose.
func TestAReadyControllerCheckNamesContextInit(t *testing.T) {
	report := controllerReport("ready", false)
	report.Actions = nil
	record := &dispatchRecord{result: commandResult{controller: report}}
	var out, errOut bytes.Buffer
	code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(context.Background(), []string{"controller", "check"})
	if code != 0 || errOut.Len() != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut.String())
	}
	if want := "    Outcome  ready\n    Next     bootwright context init --name <name> --input-dir <dir>\n"; !strings.HasSuffix(out.String(), want) {
		t.Errorf("controller check ended %q, want the outcome followed by %q", out.String(), want)
	}
}

// The controller check is context-free: it accepts no --context, so the
// readiness of a context is asked of preflight --stage controller instead.
func TestControllerCheckRefusesContext(t *testing.T) {
	record := &dispatchRecord{}
	var out, errOut bytes.Buffer
	code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(context.Background(), []string{"controller", "check", "--context", "lab"})
	if code != 2 || out.Len() != 0 || !strings.Contains(errOut.String(), "cli.usage") || record.calls != 0 {
		t.Fatalf("exit %d, stdout %q, stderr %q, calls %d", code, out.String(), errOut.String(), record.calls)
	}
}
