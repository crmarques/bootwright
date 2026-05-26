package workflow

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/ansible"
)

func TestRunApplyTaskGraphUsesRunnerFactory(t *testing.T) {
	dir := t.TempDir()
	state := minimalState()
	runner := &fakeRunner{}
	calls := 0
	factory := func(stdout io.Writer, stderr io.Writer) ansible.Runner {
		calls++
		if stdout == nil || stderr == nil {
			t.Fatal("runner factory received nil task writers")
		}
		return runner
	}
	task := ApplyTask{
		Entry: TaskLedgerEntry{
			ID:     "provider.service-host",
			Kind:   ApplyTaskKindProvider,
			Label:  "provider services service-host",
			Status: TaskStatusPending,
		},
		Playbook: "playbooks/layers/providers/apply.yml",
		State:    state,
	}
	_, err := RunApplyTaskGraph(context.Background(), io.Discard, io.Discard, filepath.Join(dir, "state"), RunOptions{
		State:             state,
		StateDir:          filepath.Join(dir, "state"),
		RuntimeDir:        filepath.Join(dir, "runtime"),
		SecretsDir:        filepath.Join(dir, "secrets"),
		HostStateDir:      filepath.Join(dir, "host-state"),
		BundleDir:         filepath.Join(dir, "bundle"),
		ArtifactsBaseName: "provider",
	}, ApplyTarget{Name: "infra", PhaseNames: []string{ApplyPhaseProvider}}, "", []ApplyTask{task}, ConcurrencyLimits{Parallelism: 1}, nil, factory)
	if err != nil {
		t.Fatalf("RunApplyTaskGraph: %v", err)
	}
	if calls != 1 {
		t.Fatalf("runner factory calls = %d, want 1", calls)
	}
	if !runner.runCalled {
		t.Fatal("fake runner was not invoked")
	}
	if !strings.HasSuffix(runner.lastSpec.Playbook, "playbooks/layers/providers/apply.yml") {
		t.Fatalf("playbook = %q", runner.lastSpec.Playbook)
	}
}
