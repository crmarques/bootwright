package cli

import (
	"bytes"
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
			want := "{\"schemaVersion\":\"v1alpha1\",\"command\":\"validate\",\"ok\":false,\"exitCode\":1,\"result\":null,\"diagnostics\":[{\"severity\":\"error\",\"code\":\"cli.not-implemented\",\"message\":\"bootwright validate is not implemented\"}],\"logs\":[]}\n"
			if out.String() != want || errOut.Len() != 0 {
				t.Fatalf("JSON output = %q, stderr = %q", out.String(), errOut.String())
			}
		} else if out.Len() != 0 || errOut.String() != "[FAIL] cli.not-implemented: bootwright validate is not implemented\n" {
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
		want := "[FAIL] api.required input.yaml:4:3: metadata.name is required [NetworkConfig/management] ($.metadata.name); next: set metadata.name to a unique DNS label\n"
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
