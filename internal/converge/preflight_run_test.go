package converge

import (
	"bytes"
	"strconv"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
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

func TestPreflightForksFollowsSelectionUpToTheClamp(t *testing.T) {
	state := v1alpha1.State{}
	if got := PreflightForks(state, "host-a:host-b:host-c"); got != 3 {
		t.Fatalf("preflight forks for a 3-host limit = %d, want 3", got)
	}
	if got := PreflightForks(state, ""); got != 1 {
		t.Fatalf("preflight forks for an empty selection = %d, want 1", got)
	}
	var wide []string
	for i := 0; i < PreflightMaxForks+7; i++ {
		wide = append(wide, "host-"+strconv.Itoa(i))
	}
	if got := PreflightForks(state, strings.Join(wide, ":")); got != PreflightMaxForks {
		t.Fatalf("preflight forks for %d hosts = %d, want the clamp %d", len(wide), got, PreflightMaxForks)
	}
	if got := workflow.AnsibleForksForLimit(state, strings.Join(wide, ":")); got <= PreflightMaxForks {
		t.Fatalf("unclamped fork helper returned %d; the clamp test no longer exercises the ceiling", got)
	}
}

func TestDestroyLogPathIsStablePerBaseName(t *testing.T) {
	got := workflow.DestroyLogPath("/runs", "infra-destroy")
	if !strings.HasSuffix(got, "/runs/destroy/infra-destroy/ansible-output.log") {
		t.Fatalf("destroy log path = %q", got)
	}
}
