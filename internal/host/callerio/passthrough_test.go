package callerio

import (
	"bytes"
	"context"
	"testing"
)

func TestRunPassthroughOverridesEnvAndStreamsStdout(t *testing.T) {
	t.Setenv("KUBECONFIG", "/should/be/replaced")
	var stdout, stderr bytes.Buffer
	code, err := RunPassthrough(context.Background(), "sh",
		[]string{"-c", `printf %s "$KUBECONFIG"`},
		[]string{"KUBECONFIG=/tmp/materialized-kubeconfig"},
		nil, &stdout, &stderr)
	if err != nil {
		t.Fatalf("RunPassthrough: %v (stderr=%q)", err, stderr.String())
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr=%q)", code, stderr.String())
	}
	if stdout.String() != "/tmp/materialized-kubeconfig" {
		t.Fatalf("child KUBECONFIG = %q, want the materialized override", stdout.String())
	}
}

func TestRunPassthroughPreservesExitCode(t *testing.T) {
	code, err := RunPassthrough(context.Background(), "sh",
		[]string{"-c", "exit 7"}, nil, nil, &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("RunPassthrough: %v", err)
	}
	if code != 7 {
		t.Fatalf("exit code = %d, want 7", code)
	}
}

func TestRunPassthroughReportsMissingBinary(t *testing.T) {
	if _, err := RunPassthrough(context.Background(), "bootwright-no-such-binary-xyz",
		nil, nil, nil, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("RunPassthrough resolved a nonexistent binary")
	}
}
