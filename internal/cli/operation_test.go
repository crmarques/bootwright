package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func TestOperationBoundaryIsLazyForInformationalMalformedAndUnavailablePaths(t *testing.T) {
	for _, invocation := range []string{
		"--help", "context init --help", "help render effective", "completion bash", "version",
		"__bootwright_complete context", "__bootwright_complete_no_desc render",
		"context init --name example -f one -f two", "context delete --name example --purge=false",
		"validate --output json --unknown", "render effective -f input", "render --output json",
		"add-ons list", "preflight all", "preflight container-cluster",
		"preflight storage-cluster", "preflight add-ons", "cluster list",
		"cluster oc --name example get pods",
		"render --output-dir artifacts --sensitive", "render installer", "render storage",
	} {
		t.Run(invocation, func(t *testing.T) {
			begins := 0
			record := &dispatchRecord{err: availability.ErrNotImplemented}
			runner := New(Config{Services: dispatchSpies(record), BeginOperation: func(ctx context.Context) (context.Context, func()) { begins++; return ctx, func() {} }})
			if begins != 0 {
				t.Fatal("constructor began an operation")
			}
			runner.Run(context.Background(), strings.Fields(invocation))
			if begins != 0 {
				t.Fatal("inert invocation registered an operation")
			}
		})
	}
}

func TestOperationBoundaryOwnsContextAndCleanupForEveryImplementedPath(t *testing.T) {
	invocations := append(contextInvocations(),
		[]string{"validate", "-f", "input"}, []string{"validate", "--context", "example"}, []string{"render", "effective"},
		[]string{"secret", "set", "--name", "example", "--value-file", "value"}, []string{"secret", "generate"}, []string{"secret", "check"}, []string{"secret", "list"},
		[]string{"secret", "show", "--name", "example", "--part", "value"}, []string{"secret", "delete", "--name", "example"},
		[]string{"secret", "encryption", "init"}, []string{"secret", "encryption", "status"}, []string{"secret", "encryption", "rotate"},
		[]string{"media", "add", "--name", "example.iso", "--from-file", "image.iso"}, []string{"media", "list"}, []string{"media", "delete", "--name", "example.iso"},
		[]string{"setup"}, []string{"setup", "--dry-run"}, []string{"preflight", "controller"},
		[]string{"plan"}, []string{"status"}, []string{"apply", "--yes"}, []string{"destroy", "--yes"},
		[]string{"machine", "list"}, []string{"machine", "rsh", "--name", "example"},
		[]string{"machine", "exec", "--name", "example", "uptime"},
		[]string{"machine", "start", "--name", "example"}, []string{"machine", "stop", "--name", "example", "--yes"},
		[]string{"machine", "restart", "--name", "example", "--yes"},
		[]string{"machine", "trust", "--yes"})
	for _, args := range invocations {
		begins, finishes := 0, 0
		parent := context.WithValue(context.Background(), dispatchContextKey{}, "parent")
		var operation context.Context
		record := &dispatchRecord{err: availability.ErrNotImplemented}
		runner := New(Config{Services: dispatchSpies(record), BeginOperation: func(ctx context.Context) (context.Context, func()) {
			begins++
			if ctx != parent {
				t.Fatal("wrong parent context")
			}
			operation = context.WithValue(ctx, dispatchContextKey{}, "operation")
			return operation, func() { finishes++ }
		}})
		runner.Run(parent, args)
		if begins != 1 || finishes != 1 || record.calls != 1 || record.ctx != operation {
			t.Fatal("operation lifetime or context propagation", args, begins, finishes, record)
		}
	}
}

func assertInterruptOutput(t *testing.T, code int, out, errOut string, jsonMode bool) {
	t.Helper()
	if code != 130 {
		t.Fatalf("interrupt exit=%d stdout=%q stderr=%q", code, out, errOut)
	}
	if jsonMode {
		var envelope struct {
			OK          bool
			ExitCode    int
			Result      any
			Diagnostics []diagnostic
			Logs        []string
		}
		if err := json.Unmarshal([]byte(out), &envelope); err != nil || envelope.OK || envelope.ExitCode != 130 || envelope.Result != nil || len(envelope.Diagnostics) != 1 || envelope.Diagnostics[0].Code != "runtime.interrupted" || len(envelope.Logs) != 0 || errOut != "" || strings.Count(out, "\n") != 1 {
			t.Fatalf("interrupt JSON: %s stderr=%q error=%v", out, errOut, err)
		}
	} else if out != "" || errOut != "[FAIL] runtime.interrupted: operation interrupted\n" {
		t.Fatalf("interrupt text: stdout=%q stderr=%q", out, errOut)
	}
}

func TestInterruptDuringServiceDiscardsTypedResults(t *testing.T) {
	invocations := append(contextInvocations(), []string{"validate", "-f", "input"}, []string{"validate", "--output", "json"}, []string{"render", "effective"}, []string{"render", "effective", "--output", "json"})
	for _, args := range invocations {
		var cancel context.CancelCauseFunc
		finishes := 0
		record := &dispatchRecord{result: syntheticContextResults(), report: &compilation.Report{Counts: compilation.Counts{FilesSeen: 999}}}
		record.result.effective = syntheticEffectiveResult()
		record.afterCall = func() { cancel(ErrInterrupted) }
		var out, errOut bytes.Buffer
		code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record), BeginOperation: func(ctx context.Context) (context.Context, func()) {
			ctx, cancel = context.WithCancelCause(ctx)
			return ctx, func() { finishes++; cancel(nil) }
		}}).Run(context.Background(), args)
		assertInterruptOutput(t, code, out.String(), errOut.String(), args[len(args)-1] == "json")
		if finishes != 1 || record.calls != 1 || strings.Contains(out.String(), "999") {
			t.Fatal("interrupt result or cleanup", args, finishes, record.calls, out.String())
		}
	}
}

func TestInterruptDuringEffectiveEncodingPrecedesOutput(t *testing.T) {
	for _, mode := range []string{"text", "json"} {
		var cancel context.CancelCauseFunc
		finishes, encodes := 0, 0
		record := &dispatchRecord{result: commandResult{effective: syntheticEffectiveResult()}}
		encode := func(context.Context, api.Catalog) ([]byte, error) {
			encodes++
			cancel(ErrInterrupted)
			return []byte("partial"), errors.New("untrusted encoder error")
		}
		var out, errOut bytes.Buffer
		code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record), EncodeEffectiveYAML: encode, EncodeEffectiveJSON: encode, BeginOperation: func(ctx context.Context) (context.Context, func()) {
			ctx, cancel = context.WithCancelCause(ctx)
			return ctx, func() { finishes++; cancel(nil) }
		}}).Run(context.Background(), []string{"render", "effective", "--output", mode})
		assertInterruptOutput(t, code, out.String(), errOut.String(), mode == "json")
		if finishes != 1 || encodes != 1 {
			t.Fatal("encoding interrupt did not finish", finishes, encodes)
		}
	}
}

func TestOperationBoundaryPreservesOrdinaryCancellationAndDeadline(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		finishes := 0
		record := &dispatchRecord{result: syntheticContextResults()}
		var out, errOut bytes.Buffer
		code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record), BeginOperation: func(ctx context.Context) (context.Context, func()) {
			var cancel context.CancelFunc
			if deadline {
				ctx, cancel = context.WithDeadline(ctx, time.Time{})
			} else {
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			return ctx, func() { finishes++; cancel() }
		}}).Run(context.Background(), []string{"context", "current"})
		want := "runtime.canceled"
		if deadline {
			want = "runtime.deadline"
		}
		if code != 1 || out.Len() != 0 || !strings.Contains(errOut.String(), want) || finishes != 1 || record.calls != 0 {
			t.Fatal(code, out.String(), errOut.String(), finishes, record.calls)
		}
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(ErrInterrupted)
	begins := 0
	var out, errOut bytes.Buffer
	code := New(Config{Out: &out, ErrOut: &errOut, BeginOperation: func(ctx context.Context) (context.Context, func()) { begins++; return ctx, func() {} }}).Run(ctx, []string{"validate", "--output", "json"})
	assertInterruptOutput(t, code, out.String(), errOut.String(), true)
	if begins != 0 {
		t.Fatal("already canceled operation acquired capability")
	}
}

func TestInvalidOperationContextFailsClosedAndCleansUp(t *testing.T) {
	for _, nilContext := range []bool{false, true} {
		finishes := 0
		record := &dispatchRecord{result: syntheticContextResults()}
		var out, errOut bytes.Buffer
		code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record), BeginOperation: func(ctx context.Context) (context.Context, func()) {
			if nilContext {
				return nil, func() { finishes++ }
			}
			return ctx, nil
		}}).Run(context.Background(), []string{"context", "list"})
		if code != 1 || out.Len() != 0 || !strings.Contains(errOut.String(), "runtime.internal") || record.calls != 0 || nilContext && finishes != 1 {
			t.Fatal(code, out.String(), errOut.String(), finishes, record.calls)
		}
	}
}

func TestInterruptedOutputFailureStillFinishesWithoutFallback(t *testing.T) {
	var cancel context.CancelCauseFunc
	finishes := 0
	record := &dispatchRecord{result: commandResult{effective: syntheticEffectiveResult()}}
	record.afterCall = func() { cancel(ErrInterrupted) }
	var errOut bytes.Buffer
	code := New(Config{Out: rejectingWriter{}, ErrOut: &errOut, Services: dispatchSpies(record), BeginOperation: func(ctx context.Context) (context.Context, func()) {
		ctx, cancel = context.WithCancelCause(ctx)
		return ctx, func() { finishes++; cancel(nil) }
	}}).Run(context.Background(), []string{"render", "effective", "--output", "json"})
	if code != 1 || errOut.Len() != 0 || finishes != 1 {
		t.Fatal("interrupted output failure", code, errOut.String(), finishes)
	}
}

// An apply or destroy interrupted after it registered its operation still
// writes that operation's result, its logs, the command that continues it and
// its receipt, which are what the operator continues from, then the interrupt
// alone, naming that command: what the cancellation made each block report is
// in the result and its logs. An interrupt with no registered operation keeps
// the bare diagnostic and writes no result.
func TestAnInterruptedRegisteredOperationKeepsItsResultAndReceipt(t *testing.T) {
	interrupted := func(result *lifecycle.OperationResult, path string) (int, string, string) {
		var cancel context.CancelCauseFunc
		record := &dispatchRecord{result: commandResult{lifecycleOperation: result}}
		record.err = &diagnostics.Failure{Diagnostics: []diagnostics.Diagnostic{{Severity: "error", Code: "lifecycle.state", Message: "the " + path + " did not complete"}}}
		record.afterCall = func() { cancel(ErrInterrupted) }
		var out, errOut bytes.Buffer
		code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record), BeginOperation: func(ctx context.Context) (context.Context, func()) {
			ctx, cancel = context.WithCancelCause(ctx)
			return ctx, func() { cancel(nil) }
		}}).Run(context.Background(), []string{path, "--yes"})
		return code, out.String(), errOut.String()
	}
	for _, path := range []string{"apply", "destroy"} {
		t.Run(path, func(t *testing.T) {
			next := "bootwright " + path + " --context lab"
			result := &lifecycle.OperationResult{
				Context: lifecycle.ContextIdentity{Name: "lab"}, Verb: path,
				Blocks: []lifecycle.BlockResult{
					{ID: "artifact-server-lab", Description: "serve artifacts for lab-artifacts", State: "done"},
					{ID: "dns-lab", Description: "resolve names for lab-dns", State: "pending"},
				},
				LogLocation: "/var/lib/bootwright/contexts/lab/state/operations/op-abc/logs",
				NextCommand: next,
				Receipt:     lifecycle.Receipt{Operation: "op-abc", Verb: path, State: "running", Next: "continue-" + path},
			}
			code, out, errOut := interrupted(result, path)
			verb := strings.ToUpper(path[:1]) + path[1:]
			want := "[FAIL] " + verb + " running\n\nResult\n" +
				"  [DONE]     serve artifacts for lab-artifacts\n" +
				"  [PENDING]  resolve names for lab-dns\n\n" +
				"  Logs  /var/lib/bootwright/contexts/lab/state/operations/op-abc/logs\n" +
				"  Next  " + next + "\n" +
				"operation: op-abc\nverb: " + path + "\nstate: running\nnext: continue-" + path + "\n"
			if code != 130 || out != want || errOut != "[FAIL] runtime.interrupted: operation interrupted; next: "+next+"\n" {
				t.Fatalf("exit %d\nstdout %q\nwant   %q\nstderr %q", code, out, want, errOut)
			}
		})
	}
	settled := &lifecycle.OperationResult{
		Context: lifecycle.ContextIdentity{Name: "lab"}, Verb: "destroy", Settled: true,
		Receipt: lifecycle.Receipt{Operation: "none", Verb: "destroy", State: "done", Next: "none"},
	}
	for name, result := range map[string]*lifecycle.OperationResult{"a settled result": settled, "no result": nil} {
		t.Run(name, func(t *testing.T) {
			code, out, errOut := interrupted(result, "destroy")
			assertInterruptOutput(t, code, out, errOut, false)
		})
	}
}
