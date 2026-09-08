package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
)

func TestValidationSuccessHasExactStableRepresentations(t *testing.T) {
	for _, jsonMode := range []bool{false, true} {
		var out, errOut bytes.Buffer
		record := &dispatchRecord{report: &compilation.Report{Counts: compilation.Counts{FilesSeen: 2, ObjectsDecoded: 4}}}
		args := []string{"validate", "-f", "synthetic", "--context", "ignored"}
		if jsonMode {
			args = append(args, "--output", "json")
		}
		code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(context.Background(), args)
		if code != 0 || errOut.Len() != 0 || record.calls != 1 || record.request.(compilation.ValidateRequest).ContextName != "" {
			t.Fatalf("code=%d out=%q err=%q record=%#v", code, out.String(), errOut.String(), record)
		}
		want := "[OK] Desired state is valid (files seen: 2, objects decoded: 4)\n"
		if jsonMode {
			want = "{\"schemaVersion\":\"v1alpha1\",\"command\":\"validate\",\"ok\":true,\"exitCode\":0,\"result\":{\"counts\":{\"filesSeen\":2,\"objectsDecoded\":4},\"excludedContainerClusters\":[],\"excludedStorageClusters\":[],\"excludedResourceFiles\":[],\"advisories\":[]},\"diagnostics\":[],\"logs\":[]}\n"
		}
		if out.String() != want {
			t.Fatalf("got %q, want %q", out.String(), want)
		}
	}
}

func TestValidationWarningsAreSortedEscapedOnceAndPreserved(t *testing.T) {
	raw := "<value>\\path\n\x1b\xff"
	warnings := []diagnostic{
		{Severity: "warning", Code: "api.deferred", Message: raw, Source: &desiredstate.SourceLocation{Path: "z" + raw, Document: 2, Line: 4, Column: 3}, Object: &desiredstate.ObjectIdentity{APIVersion: raw, Kind: raw, Name: raw}, Field: raw, Remediation: raw},
		{Severity: "warning", Code: "api.excluded", Message: "excluded", Source: &desiredstate.SourceLocation{Path: "a.yaml"}},
	}
	report := &compilation.Report{Counts: compilation.Counts{FilesSeen: 3, ObjectsDecoded: 2}, ExcludedContainerClusters: []string{"z", "a", "a"}, ExcludedStorageClusters: []string{raw}, ExcludedResourceFiles: []string{"z.yaml", "a.yaml"}, Advisories: warnings[:1], Diagnostics: warnings}
	before, _ := json.Marshal(report)
	var jsonOut, jsonErr bytes.Buffer
	if err := writeValidation(&jsonOut, &jsonErr, "validate", report, true); err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Result      compilation.Report `json:"result"`
		Diagnostics []diagnostic       `json:"diagnostics"`
	}
	if err := json.Unmarshal(jsonOut.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if jsonErr.Len() != 0 || strings.Count(jsonOut.String(), "\n") != 1 || strings.Contains(jsonOut.String(), `\u003c`) {
		t.Fatal("JSON stream contract", jsonOut.String(), jsonErr.String())
	}
	if len(envelope.Diagnostics) != 2 || envelope.Diagnostics[0].Source.Path != "a.yaml" || len(envelope.Result.Advisories) != 1 || !reflect.DeepEqual(envelope.Result.Advisories[0], envelope.Diagnostics[1]) {
		t.Fatal("warning membership or order", envelope)
	}
	d := envelope.Diagnostics[1]
	want := escapeDisplayLine(raw)
	for _, value := range []string{d.Message, d.Object.APIVersion, d.Object.Kind, d.Object.Name, d.Field, d.Remediation, envelope.Result.ExcludedStorageClusters[0]} {
		if value != want {
			t.Fatalf("escaping=%q, want %q", value, want)
		}
	}
	if d.Source.Path != "z"+want || !reflect.DeepEqual(envelope.Result.ExcludedContainerClusters, []string{"a", "z"}) || !reflect.DeepEqual(envelope.Result.ExcludedResourceFiles, []string{"a.yaml", "z.yaml"}) {
		t.Fatal(envelope)
	}
	var out, errOut bytes.Buffer
	if err := writeValidation(&out, &errOut, "validate", report, false); err != nil {
		t.Fatal(err)
	}
	if strings.Count(errOut.String(), "[WARN]") != 2 || strings.Count(errOut.String(), "\n") != 2 || strings.Contains(errOut.String(), "\x1b") || !strings.HasPrefix(errOut.String(), "[WARN] api.excluded a.yaml: excluded\n") {
		t.Fatal("human warning contract", errOut.String())
	}
	after, _ := json.Marshal(report)
	if !bytes.Equal(before, after) {
		t.Fatal("presentation mutated compiler report")
	}
}

func TestValidationFailuresNeverExposePartialReports(t *testing.T) {
	for _, failure := range []error{nil, errors.New("untrusted internal details"), &desiredstate.Failure{}, &desiredstate.Failure{Diagnostics: []diagnostic{{Severity: "error", Code: "api.required", Message: "required field", Source: &desiredstate.SourceLocation{Path: "input.yaml", Line: 2, Column: 3}, Field: "$.spec", Remediation: "supply the field"}}}, context.Canceled, context.DeadlineExceeded} {
		var out, errOut bytes.Buffer
		record := &dispatchRecord{err: failure}
		if failure != nil {
			record.report = &compilation.Report{Counts: compilation.Counts{FilesSeen: 999}}
		}
		code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(context.Background(), []string{"validate", "-f", "input.yaml", "--output", "json"})
		var envelope struct {
			OK          bool
			Result      any
			Diagnostics []diagnostic
		}
		if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if code != 1 || envelope.OK || envelope.Result != nil || errOut.Len() != 0 || strings.Contains(out.String(), "999") || strings.Contains(out.String(), "untrusted internal details") {
			t.Fatalf("failure leaked result: %d %s %s", code, out.String(), errOut.String())
		}
		if len(desiredstate.DiagnosticsOf(failure)) > 0 && envelope.Diagnostics[0].Remediation != "supply the field" {
			t.Fatal("typed diagnostic lost", envelope)
		}
	}
}

func TestValidationOutputFailuresAndCancellation(t *testing.T) {
	for _, short := range []bool{false, true} {
		for _, jsonMode := range []bool{false, true} {
			var errOut bytes.Buffer
			record := &dispatchRecord{report: &compilation.Report{}}
			args := []string{"validate", "-f", "input.yaml"}
			if jsonMode {
				args = append(args, "--output", "json")
			}
			code := New(Config{Out: rejectingWriter{short: short}, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(context.Background(), args)
			if code != 1 || errOut.Len() != 0 {
				t.Fatal("failed output used a fallback", code, errOut.String())
			}
		}
	}
	var out bytes.Buffer
	record := &dispatchRecord{report: &compilation.Report{Diagnostics: []diagnostic{{Severity: "warning", Code: "api.deferred", Message: "pending"}}}}
	if code := New(Config{Out: &out, ErrOut: rejectingWriter{}, Services: dispatchSpies(record)}).Run(context.Background(), []string{"validate", "-f", "input.yaml"}); code != 1 || out.Len() != 0 {
		t.Fatal("warning output failed but success was printed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	record = &dispatchRecord{report: &compilation.Report{}}
	if code := New(Config{Out: io.Discard, ErrOut: io.Discard, Services: dispatchSpies(record)}).Run(ctx, []string{"validate", "-f", "input.yaml"}); code != 1 || record.calls != 0 {
		t.Fatal("canceled invocation reached service")
	}
}

func TestValidationHelpAndMalformedCallsNeverDispatch(t *testing.T) {
	for _, args := range [][]string{{"validate", "-f", "unopened", "--help"}, {"validate", "-f", "unopened", "--unknown"}, {"validate", "-f", "unopened", "--output", "bad"}, {"__bootwright_complete", "validate", ""}, {"version"}} {
		record := &dispatchRecord{report: &compilation.Report{}}
		New(Config{Services: dispatchSpies(record)}).Run(context.Background(), args)
		if record.calls != 0 {
			t.Fatal("informational or malformed invocation reached service", args)
		}
	}
}
