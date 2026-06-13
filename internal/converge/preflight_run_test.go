package converge

import (
	"bytes"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func TestPreflightRunnerIsFileOnlyByDefault(t *testing.T) {
	var stdout, stderr bytes.Buffer
	runner := preflightRunner(&stdout, &stderr, false)
	if runner.Stdout != nil || runner.Stderr != nil {
		t.Fatalf("default preflight runner must not write to the terminal: %+v", runner)
	}

	streaming := preflightRunner(&stdout, &stderr, true)
	if streaming.Stdout == nil || streaming.Stderr == nil {
		t.Fatalf("--stream-ansible preflight runner must tee to the terminal: %+v", streaming)
	}
}

func TestDestroyLogPathIsStablePerBaseName(t *testing.T) {
	got := workflow.DestroyLogPath("/runs", "infra-destroy")
	if !strings.HasSuffix(got, "/runs/destroy/infra-destroy/ansible-output.log") {
		t.Fatalf("destroy log path = %q", got)
	}
}
