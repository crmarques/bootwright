package workflow

import (
	"context"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/converge/ansible"
)

func TestRunApplyTaskGraphRecordsReadyAtAndBlockedOnForHostSlotContention(t *testing.T) {
	dir := t.TempDir()
	state := minimalState()
	runner := &recordingApplyRunner{delay: 25 * time.Millisecond}
	tasks := []ApplyTask{}
	for _, id := range []string{"machine-a", "machine-b"} {
		tasks = append(tasks, ApplyTask{
			Entry: TaskLedgerEntry{
				ID:            id,
				Kind:          ApplyTaskKindClusterInstall,
				Label:         id,
				Status:        TaskStatusPending,
				HostSlotKey:   "host:bastion:machine",
				HostSlotCount: 1,
			},
			Playbook:      id,
			State:         state,
			HostSlotKey:   "host:bastion:machine",
			HostSlotCount: 1,
		})
	}
	ledger, err := RunApplyTaskGraph(context.Background(), io.Discard, io.Discard, filepath.Join(dir, "runs"), RunOptions{
		State:              state,
		RenderedDir:        filepath.Join(dir, "rendered"),
		ClustersDir:        filepath.Join(dir, "clusters"),
		RunsDir:            filepath.Join(dir, "runs"),
		SecretsDir:         filepath.Join(dir, "secrets"),
		ManagedServicesDir: filepath.Join(dir, "managed-services"),
		ProviderStateDir:   filepath.Join(dir, "provider-state"),
		BundleDir:          filepath.Join(dir, "bundle"),
	}, ApplyTarget{Name: "infra", PhaseNames: []string{ApplyPhaseMachines}}, "", tasks, ConcurrencyLimits{Parallelism: 2, ParallelismPerHost: 1}, nil, func(io.Writer, io.Writer) ansible.Runner {
		return runner
	})
	if err != nil {
		t.Fatalf("RunApplyTaskGraph: %v", err)
	}

	blocked := ""
	for _, task := range ledger.Tasks {
		if task.ReadyAt == nil {
			t.Fatalf("task %s recorded no ReadyAt; dependency-ready time must be attributed", task.ID)
		}
		if task.StartedAt == nil || task.StartedAt.Before(*task.ReadyAt) {
			t.Fatalf("task %s StartedAt=%v must not precede ReadyAt=%v", task.ID, task.StartedAt, task.ReadyAt)
		}
		if len(task.BlockedOn) > 0 {
			blocked = task.ID
			if task.BlockedOn[0] != "host slot host:bastion:machine" {
				t.Fatalf("task %s BlockedOn = %v, want the host slot key that withheld dispatch", task.ID, task.BlockedOn)
			}
			if taskBlockedWait(task) <= 0 {
				t.Fatalf("task %s waited on a host slot but recorded no blocked time", task.ID)
			}
		}
	}
	if blocked == "" {
		t.Fatal("a single host slot serialized two tasks, so one of them must record what blocked it")
	}
}

func TestTaskDispatchBlockerReportsTheFirstUnavailableBudget(t *testing.T) {
	task := ApplyTask{
		Entry: TaskLedgerEntry{
			ID:            "t",
			ResourceKeys:  []string{"libvirt:host-a"},
			HostSlotKey:   "host:bastion:machine",
			HostSlotCount: 1,
		},
		RedfishSlots: 2,
	}
	if got := taskDispatchBlocker(task, 1, 2, map[string]int{}, map[string]int{}, 1); got != "redfish budget" {
		t.Fatalf("blocker with an exhausted Redfish budget = %q, want %q", got, "redfish budget")
	}
	if got := taskDispatchBlocker(task, 0, 2, map[string]int{"libvirt:host-a": 1}, map[string]int{}, 1); got != "resource libvirt:host-a" {
		t.Fatalf("blocker with a held resource key = %q", got)
	}
	if got := taskDispatchBlocker(task, 0, 2, map[string]int{}, map[string]int{"host:bastion:machine": 1}, 1); got != "host slot host:bastion:machine" {
		t.Fatalf("blocker with a full host slot = %q", got)
	}
	if got := taskDispatchBlocker(task, 0, 2, map[string]int{}, map[string]int{}, 1); got != "" {
		t.Fatalf("blocker for a dispatchable task = %q, want empty", got)
	}
}

func TestResolveApplyConcurrencyLimitsRecordsTrivialAutoValues(t *testing.T) {
	tasks := []ApplyTask{
		{Entry: TaskLedgerEntry{ID: "a", HostSlotKey: "host:bastion:machine", HostSlotCount: 1}, RedfishSlots: 2},
		{Entry: TaskLedgerEntry{ID: "b", HostSlotKey: "host:bastion:machine", HostSlotCount: 1}},
		{Entry: TaskLedgerEntry{ID: "c"}},
	}
	limits := ResolveApplyConcurrencyLimits(ConcurrencyLimits{}, tasks)
	if limits.AutoParallelism != 3 || limits.AutoParallelismPerHost != 2 || limits.AutoParallelismRedfish != 2 {
		t.Fatalf("auto limits = %+v, want 3/2/2", limits)
	}
	if !limits.ParallelismUnbounded() || !limits.ParallelismPerHostUnbounded() || !limits.ParallelismRedfishUnbounded() {
		t.Fatalf("an empty ConcurrencyLimits{} caps nothing, so every limit must report unbounded: %+v", limits)
	}
	capped := ResolveApplyConcurrencyLimits(ConcurrencyLimits{Parallelism: 2, ParallelismPerHost: 1, ParallelismRedfish: 1}, tasks)
	if capped.ParallelismUnbounded() || capped.ParallelismPerHostUnbounded() || capped.ParallelismRedfishUnbounded() {
		t.Fatalf("explicit caps below the auto values must not report unbounded: %+v", capped)
	}
	if got := ResolveApplyConcurrencyLimits(capped, tasks); got != capped {
		t.Fatalf("resolving already-resolved limits changed them: %+v -> %+v", capped, got)
	}
}
