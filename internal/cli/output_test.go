package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestFailureRepresentations(t *testing.T) {
	for _, jsonMode := range []bool{false, true} {
		var out, errOut bytes.Buffer
		err := writeFailure(&out, &errOut, "validate", "cli.not-implemented", "bootwright validate is not implemented", 1, jsonMode, nil)
		if err != nil {
			t.Fatal(err)
		}
		if jsonMode {
			want := "{\"schemaVersion\":\"v1alpha1\",\"command\":\"validate\",\"ok\":false,\"exitCode\":1,\"result\":null,\"diagnostics\":[{\"severity\":\"error\",\"code\":\"cli.not-implemented\",\"message\":\"bootwright validate is not implemented\",\"remediation\":\"run bootwright help <command> to see which forms this build runs\"}],\"logs\":[]}\n"
			if out.String() != want || errOut.Len() != 0 {
				t.Fatalf("JSON output = %q, stderr = %q", out.String(), errOut.String())
			}
		} else if out.Len() != 0 || errOut.String() != "  error  cli.not-implemented\n    bootwright validate is not implemented\n    Next: run bootwright help <command> to see which forms this build runs\n" {
			t.Fatalf("human output = %q, stderr = %q", out.String(), errOut.String())
		}
	}
}

func TestDiagnosticRemediationRepresentations(t *testing.T) {
	diagnostics := []diagnostic{{
		Severity: "error",
		Code:     "api.required",
		Message:  "metadata.name is required",
		Source: &diagnostics.SourceLocation{
			Path:     "input.yaml",
			Document: 1,
			Line:     4,
			Column:   3,
		},
		Object: &diagnostics.ObjectIdentity{
			APIVersion: "bootwright.io/v1alpha1",
			Kind:       "NetworkConfig",
			Name:       "management",
		},
		Field:       "$.metadata.name",
		Remediation: "set metadata.name to a unique DNS label",
	}}
	for _, jsonMode := range []bool{false, true} {
		var out, errOut bytes.Buffer
		if err := writeDiagnostics(&out, &errOut, "validate", diagnostics, 1, jsonMode, nil); err != nil {
			t.Fatal(err)
		}
		if jsonMode {
			want := "{\"schemaVersion\":\"v1alpha1\",\"command\":\"validate\",\"ok\":false,\"exitCode\":1,\"result\":null,\"diagnostics\":[{\"severity\":\"error\",\"code\":\"api.required\",\"message\":\"metadata.name is required\",\"source\":{\"path\":\"input.yaml\",\"document\":1,\"line\":4,\"column\":3},\"object\":{\"apiVersion\":\"bootwright.io/v1alpha1\",\"kind\":\"NetworkConfig\",\"name\":\"management\"},\"field\":\"$.metadata.name\",\"remediation\":\"set metadata.name to a unique DNS label\"}],\"logs\":[]}\n"
			if out.String() != want || errOut.Len() != 0 {
				t.Fatalf("JSON output = %q, stderr = %q", out.String(), errOut.String())
			}
			continue
		}
		want := "  error  NetworkConfig/management\n    api.required\n    input.yaml:4:3\n    metadata.name is required\n    Field: $.metadata.name\n    Next: set metadata.name to a unique DNS label\n"
		if out.Len() != 0 || errOut.String() != want {
			t.Fatalf("human output = %q, stderr = %q", out.String(), errOut.String())
		}
	}
}

func TestSafeDisplay(t *testing.T) {
	input := "a\\b\n\r\t\x00\x1b\x7f\u0085\u2028\u202e\U000e0001 café \xff"
	want := `a\\b\n\r\t\u0000\u001b\u007f\u0085\u2028\u202e\udb40\udc01 café \xff`
	if got := escapeDisplayLine(input); got != want {
		t.Fatalf("escaped = %q, want %q", got, want)
	}
	var out, errOut bytes.Buffer
	if err := writeFailure(&out, &errOut, "validate", "cli.usage", "<value>\n\x1b", 2, true, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), `\u003c`) || !strings.Contains(out.String(), `<value>\\n\\u001b`) || errOut.Len() != 0 {
		t.Fatalf("unsafe or HTML-escaped JSON: %q", out.String())
	}
}

type failedOutput struct {
	calls int
	err   error
}

func (w *failedOutput) Write([]byte) (int, error) {
	w.calls++
	return 0, w.err
}

func TestFailureWriterDoesNotFallback(t *testing.T) {
	want := errors.New("writer unavailable")
	out := &failedOutput{err: want}
	var errOut bytes.Buffer
	if err := writeFailure(out, &errOut, "validate", "cli.usage", "invalid invocation", 2, true, nil); !errors.Is(err, want) {
		t.Fatalf("error = %v, want writer error", err)
	}
	if out.calls != 1 || errOut.Len() != 0 {
		t.Fatalf("writes = %d, fallback = %q", out.calls, errOut.String())
	}
}

func TestEveryErrorDiagnosticIsWrittenWithARemediation(t *testing.T) {
	codes := diagnostics.DefaultRemediationCodes()
	if len(codes) < 60 {
		t.Fatalf("read %d codes with a default remediation", len(codes))
	}
	for _, code := range codes {
		var out, errOut bytes.Buffer
		reported := []diagnostic{{Severity: "error", Code: code, Message: "m"}, {Severity: "error", Code: code, Message: "n", Remediation: "given"}}
		if err := writeDiagnostics(&out, &errOut, "plan", reported, 1, true, nil); err != nil {
			t.Fatal(err)
		}
		var envelope struct{ Diagnostics []diagnostic }
		if err := json.Unmarshal(out.Bytes(), &envelope); err != nil || len(envelope.Diagnostics) != 2 {
			t.Fatalf("%s: %v %s", code, err, out.String())
		}
		for _, written := range envelope.Diagnostics {
			if written.Remediation == "" {
				t.Errorf("%s: an error diagnostic was written with no remediation", code)
			}
		}
		if got := envelope.Diagnostics[1].Remediation; got != "given" {
			t.Errorf("%s: an emitter's own remediation was replaced by %q", code, got)
		}
	}
}

func TestUsageAndWarningDiagnosticsKeepNoDefaultRemediation(t *testing.T) {
	var out, errOut bytes.Buffer
	reported := []diagnostic{{Severity: "error", Code: "cli.usage", Message: "m"}, {Severity: "warning", Code: "runtime.internal", Message: "n"}}
	if err := writeDiagnostics(&out, &errOut, "plan", reported, 2, true, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "remediation") {
		t.Fatalf("a usage error or a warning was given a default remediation: %s", out.String())
	}
}
