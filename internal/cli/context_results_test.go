package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

func syntheticContextResults() commandResult {
	summary := contexts.Summary{Name: "example", Mode: contexts.Ready, Current: true}
	return commandResult{
		admission: &contexts.AdmissionResult{Context: summary, Counts: compilation.Counts{FilesSeen: 3, ObjectsDecoded: 4}, FilesCopied: 5, InputChanged: true},
		use:       &contexts.UseResult{Context: summary},
		list:      &contexts.ListResult{Contexts: []contexts.Summary{{Name: "z", Mode: contexts.Initializing}, summary}},
		current:   &contexts.CurrentResult{Context: summary},
		deletion:  &contexts.DeleteResult{Name: summary.Name, Outcome: "deleted", CurrentCleared: true},
	}
}

func contextInvocations() [][]string {
	return [][]string{
		{"context", "init", "--name", "example", "-f", "input"},
		{"context", "update", "--name", "example", "-f", "input"},
		{"context", "use", "--name", "example"},
		{"context", "list"},
		{"context", "current"},
		{"context", "current", "--short"},
		{"context", "delete", "--name", "example", "--purge"},
	}
}

func TestContextSuccessJourneysHaveCompleteTextResults(t *testing.T) {
	for _, args := range contextInvocations() {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			record := &dispatchRecord{result: syntheticContextResults()}
			before := append([]contexts.Summary(nil), record.result.list.Contexts...)
			var out, errOut bytes.Buffer
			code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(context.Background(), append(args, "--context", "ignored"))
			if code != 0 || errOut.Len() != 0 || record.calls != 1 || !strings.HasSuffix(out.String(), "\n") {
				t.Fatalf("code=%d out=%q err=%q record=%#v", code, out.String(), errOut.String(), record)
			}
			if args[1] == "current" && len(args) > 2 {
				if out.String() != "example\n" {
					t.Fatalf("short current output %q", out.String())
				}
				return
			}
			if !strings.Contains(out.String(), "example") {
				t.Fatalf("missing context name: %q", out.String())
			}
			switch args[1] {
			case "init", "update":
				for _, count := range []string{"Files copied     5", "Files seen       3", "Objects decoded  4"} {
					if !strings.Contains(out.String(), count) {
						t.Fatalf("missing count %q: %q", count, out.String())
					}
				}
			case "list":
				if strings.Index(out.String(), "example ") > strings.Index(out.String(), "z ") || !strings.Contains(out.String(), "initializing") || !reflect.DeepEqual(before, record.result.list.Contexts) {
					t.Fatal("list order, mode or immutability", out.String())
				}
			case "delete":
				if !strings.Contains(out.String(), "deleted") || !strings.Contains(out.String(), "Current cleared  true") {
					t.Fatal(out.String())
				}
			default:
				if !strings.Contains(out.String(), "Mode              ready\n  Current           true\n") {
					t.Fatal("missing current details", out.String())
				}
			}
		})
	}
}

func TestContextEmptyListAndPermanentDeletion(t *testing.T) {
	for _, tc := range []struct {
		args   []string
		result commandResult
		want   string
	}{
		{[]string{"context", "list"}, commandResult{list: &contexts.ListResult{}}, "[OK] No contexts\n"},
		{[]string{"context", "delete", "--name", "example", "--purge", "--yes"}, commandResult{deletion: &contexts.DeleteResult{Name: "example", Outcome: "deleted"}}, "Context deleted"},
	} {
		var out, errOut bytes.Buffer
		record := &dispatchRecord{result: tc.result}
		if code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(context.Background(), tc.args); code != 0 || !strings.Contains(out.String(), tc.want) || errOut.Len() != 0 {
			t.Fatal(code, out.String(), errOut.String())
		}
	}
}

func TestContextAdmissionWarningsArePrintedOnceAndEscaped(t *testing.T) {
	raw := "<value>\\path\n\x1b\xff"
	warnings := []diagnostic{
		{Severity: "warning", Code: "api.deferred", Message: raw, Source: &diagnostics.SourceLocation{Path: "z" + raw}, Field: raw, Remediation: raw},
		{Severity: "warning", Code: "api.selection", Message: "excluded", Source: &diagnostics.SourceLocation{Path: "a.yaml"}},
	}
	for _, args := range contextInvocations()[:2] {
		record := &dispatchRecord{result: syntheticContextResults()}
		record.result.admission.Diagnostics = warnings
		var out, errOut bytes.Buffer
		if code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(context.Background(), args); code != 0 || strings.Count(errOut.String(), "[WARN]") != 2 || strings.Contains(out.String(), "[WARN]") || strings.Contains(errOut.String(), "\x1b") || !strings.HasPrefix(errOut.String(), "[WARN] api.selection a.yaml:") || !strings.Contains(errOut.String(), escapeDisplayLine(raw)) {
			t.Fatal(code, out.String(), errOut.String())
		}
		if warnings[0].Message != raw || warnings[0].Source.Path != "z"+raw {
			t.Fatal("presentation mutated diagnostics")
		}
	}
	result := syntheticContextResults()
	result.current.Context.Name = raw
	var out bytes.Buffer
	escaped := escapeDisplayLine(raw)
	if err := writeContextCurrent(&out, result.current, false); err != nil {
		t.Fatal("context identity display", out.String(), err)
	}
	if strings.Count(out.String(), escaped) != 1 || strings.Contains(out.String(), raw) {
		t.Fatal("context identity display", out.String())
	}
}

func TestContextFailuresNeverPrintPartialResults(t *testing.T) {
	for _, args := range contextInvocations() {
		for _, failure := range []error{nil, contexts.StateError("current selection is absent"), contexts.UnsafeDelete("recovery evidence prevents deletion"), errors.New("private detail"), context.Canceled, context.DeadlineExceeded} {
			record := &dispatchRecord{err: failure}
			if failure != nil {
				record.result = syntheticContextResults()
			}
			var out, errOut bytes.Buffer
			code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(context.Background(), args)
			if code != 1 || out.Len() != 0 || errOut.Len() == 0 || strings.Contains(errOut.String(), "private detail") {
				t.Fatalf("%v: code=%d out=%q err=%q", args, code, out.String(), errOut.String())
			}
			if failure == nil && !strings.Contains(errOut.String(), "runtime.internal") {
				t.Fatal("nil result was not an internal failure", errOut.String())
			}
			if ds := diagnostics.Of(failure); len(ds) > 0 && !strings.Contains(errOut.String(), ds[0].Code) {
				t.Fatal("typed failure lost", errOut.String())
			}
		}
	}
}

func TestContextOutputFailuresDoNotProduceFallback(t *testing.T) {
	for _, args := range contextInvocations() {
		for _, writer := range []io.Writer{rejectingWriter{}, rejectingWriter{short: true}, contextErrorWriter{}} {
			record := &dispatchRecord{result: syntheticContextResults()}
			var errOut bytes.Buffer
			if code := New(Config{Out: writer, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(context.Background(), args); code != 1 || errOut.Len() != 0 {
				t.Fatal("output failure produced fallback", args, code, errOut.String())
			}
		}
	}
	record := &dispatchRecord{result: syntheticContextResults()}
	record.result.admission.Diagnostics = []diagnostic{{Severity: "warning", Code: "api.deferred", Message: "pending"}}
	var out bytes.Buffer
	if code := New(Config{Out: &out, ErrOut: rejectingWriter{}, Services: dispatchSpies(record)}).Run(context.Background(), contextInvocations()[0]); code != 1 || out.Len() != 0 {
		t.Fatal("warning failure printed success", code, out.String())
	}
}

type contextErrorWriter struct{}

func (contextErrorWriter) Write([]byte) (int, error) { return 0, context.Canceled }

func TestContextInformationalAndMalformedInvocationsRemainInert(t *testing.T) {
	for _, args := range [][]string{
		{"context", "init", "--help"}, {"context", "update", "-f", "input", "--unknown"},
		{"context", "init", "--name", "example", "-f", "one", "-f", "two"},
		{"context", "delete", "--name", "example", "--purge=false"},
		{"context", "list", "--output", "json"}, {"context", "current", "--name", "example"},
		{"render", "effective", "--help"}, {"render", "effective", "-f", "input"},
		{"__bootwright_complete", "context", ""}, {"version"},
	} {
		record := &dispatchRecord{result: syntheticContextResults()}
		New(Config{Services: dispatchSpies(record)}).Run(context.Background(), args)
		if record.calls != 0 {
			t.Fatal("unexpected application effect", args)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, args := range contextInvocations() {
		record := &dispatchRecord{result: syntheticContextResults()}
		if code := New(Config{Services: dispatchSpies(record)}).Run(ctx, args); code != 1 || record.calls != 0 {
			t.Fatal("canceled invocation dispatched", args, code)
		}
	}
}

func TestNewJourneysDiscardResultsWhenServiceCancels(t *testing.T) {
	invocations := append(contextInvocations(), []string{"render", "effective", "--output", "json"})
	for _, args := range invocations {
		ctx, cancel := context.WithCancel(context.Background())
		record := &dispatchRecord{result: syntheticContextResults(), afterCall: cancel}
		record.result.effective = syntheticEffectiveResult()
		var out, errOut bytes.Buffer
		code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(ctx, args)
		cancel()
		if code != 1 || record.calls != 1 || !strings.Contains(out.String()+errOut.String(), "runtime.canceled") || strings.Contains(out.String()+errOut.String(), "ctx-synthetic") || args[0] == "context" && out.Len() != 0 {
			t.Fatal("canceled service leaked result", args, code, out.String(), errOut.String())
		}
	}
}

func TestContextInitWithoutInputUsesDefaults(t *testing.T) {
	record := &dispatchRecord{result: syntheticContextResults()}
	record.result.admission = &contexts.AdmissionResult{Context: contexts.Summary{Name: "test", Mode: contexts.Ready, Current: true}}
	var out, errOut bytes.Buffer
	code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(context.Background(), []string{"context", "init", "--name", "test"})
	request, ok := record.request.(contexts.InitRequest)
	if code != 0 || !ok || request != (contexts.InitRequest{Name: "test"}) || errOut.Len() != 0 || !strings.Contains(out.String(), "Input configured  false") || strings.Contains(out.String(), "Files copied") {
		t.Fatalf("default init contract: code=%d request=%#v stdout=%q stderr=%q", code, record.request, out.String(), errOut.String())
	}
}

func TestContextConfigurationAndInputFlagsAreIndependent(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want any
	}{
		{[]string{"context", "init", "--name", "test", "-f", "context.yaml", "--input-dir", "input"}, contexts.InitRequest{Name: "test", ConfigurationFile: "context.yaml", InputDirectory: "input"}},
		{[]string{"context", "update", "--name", "test", "-f", "context.yaml"}, contexts.UpdateRequest{Name: "test", ConfigurationFile: "context.yaml"}},
		{[]string{"context", "update", "--name", "test", "--input-dir", "input", "--yes"}, contexts.UpdateRequest{Name: "test", InputDirectory: "input", SkipConfirmation: true}},
	} {
		record := &dispatchRecord{result: syntheticContextResults()}
		code := New(Config{Services: dispatchSpies(record)}).Run(context.Background(), tc.args)
		if code != 0 || !reflect.DeepEqual(record.request, tc.want) {
			t.Fatal("Context and Environment sources were confused", tc.args, code, record.request)
		}
	}
	for _, args := range [][]string{
		{"context", "init", "--name", "test", "--yes"},
		{"context", "init", "--name", "test", "--input-dir="},
		{"context", "update", "--name", "test"},
		{"context", "delete", "--name", "test", "--purge", "--abandon-resources"},
		{"secret", "encryption", "init", "--type", "local-keyring"},
	} {
		record := &dispatchRecord{}
		if code := New(Config{Services: dispatchSpies(record)}).Run(context.Background(), args); code != 2 || record.calls != 0 {
			t.Fatal("removed or incomplete invocation reached application", args, code, record.calls)
		}
	}
}
