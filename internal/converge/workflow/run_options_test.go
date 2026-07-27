package workflow

import (
	"fmt"
	"runtime"
	"testing"
)

func wideApplyTaskGraph(count int) []ApplyTask {
	tasks := make([]ApplyTask, 0, count)
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("task-%02d", i)
		tasks = append(tasks, ApplyTask{
			Entry: TaskLedgerEntry{
				ID:            id,
				Kind:          ApplyTaskKindNodeBoot,
				Label:         id,
				Status:        TaskStatusPending,
				HostSlotKey:   "host:bastion:machine",
				HostSlotCount: 1,
			},
			RedfishSlots: 1,
		})
	}
	return tasks
}

func TestResolveApplyConcurrencyLimitsCapsAWideGraph(t *testing.T) {
	tasks := wideApplyTaskGraph(40)
	limits := ResolveApplyConcurrencyLimits(ConcurrencyLimits{}, tasks)

	if limits.AutoParallelism != 40 || limits.AutoParallelismPerHost != 40 || limits.AutoParallelismRedfish != 40 {
		t.Fatalf("auto limits = %+v, want 40/40/40 for a 40-task single-host graph", limits)
	}
	if limits.Parallelism >= limits.AutoParallelism {
		t.Fatalf("global parallelism = %d, want a real cap below the trivial auto value %d", limits.Parallelism, limits.AutoParallelism)
	}
	if limits.ParallelismPerHost != DefaultParallelismPerHost {
		t.Fatalf("per-host parallelism = %d, want the %d default cap", limits.ParallelismPerHost, DefaultParallelismPerHost)
	}
	if limits.ParallelismRedfish != DefaultParallelismRedfish {
		t.Fatalf("Redfish parallelism = %d, want the %d default cap", limits.ParallelismRedfish, DefaultParallelismRedfish)
	}
	if limits.ParallelismUnbounded() || limits.ParallelismPerHostUnbounded() || limits.ParallelismRedfishUnbounded() {
		t.Fatalf("a capped wide graph must report every limit as bounded: %+v", limits)
	}
	if again := ResolveApplyConcurrencyLimits(limits, tasks); again != limits {
		t.Fatalf("resolving already-resolved wide-graph limits changed them: %+v -> %+v", limits, again)
	}
}

func TestResolveApplyConcurrencyLimitsKeepsALowerRequestedValue(t *testing.T) {
	tasks := wideApplyTaskGraph(40)
	limits := ResolveApplyConcurrencyLimits(ConcurrencyLimits{Parallelism: 3, ParallelismPerHost: 1, ParallelismRedfish: 2}, tasks)
	if limits.Parallelism != 3 || limits.ParallelismPerHost != 1 || limits.ParallelismRedfish != 2 {
		t.Fatalf("limits = %+v, want the lower requested values 3/1/2 to win over the defaults", limits)
	}
	if again := ResolveApplyConcurrencyLimits(limits, tasks); again != limits {
		t.Fatalf("resolving already-resolved limits changed them: %+v -> %+v", limits, again)
	}
}

func TestResolveApplyConcurrencyLimitsNeverExceedsWhatTheGraphCanUse(t *testing.T) {
	tasks := wideApplyTaskGraph(2)
	limits := ResolveApplyConcurrencyLimits(ConcurrencyLimits{}, tasks)
	if limits.Parallelism != 2 || limits.ParallelismPerHost != 2 || limits.ParallelismRedfish != 2 {
		t.Fatalf("limits = %+v, want every limit clamped to the 2-task graph", limits)
	}
	if !limits.ParallelismUnbounded() || !limits.ParallelismPerHostUnbounded() || !limits.ParallelismRedfishUnbounded() {
		t.Fatalf("a graph smaller than every default cap must still report unbounded: %+v", limits)
	}
}

func TestResolveApplyConcurrencyLimitsReadsEnvironmentOverrides(t *testing.T) {
	tasks := wideApplyTaskGraph(40)
	t.Setenv(ParallelismEnvVar, "5")
	t.Setenv(ParallelismPerHostEnvVar, "2")
	t.Setenv(ParallelismRedfishEnvVar, "3")
	limits := ResolveApplyConcurrencyLimits(ConcurrencyLimits{}, tasks)
	if limits.Parallelism != 5 || limits.ParallelismPerHost != 2 || limits.ParallelismRedfish != 3 {
		t.Fatalf("limits = %+v, want the environment overrides 5/2/3", limits)
	}
	if again := ResolveApplyConcurrencyLimits(limits, tasks); again != limits {
		t.Fatalf("environment-overridden limits must resolve idempotently: %+v -> %+v", limits, again)
	}
	explicit := ResolveApplyConcurrencyLimits(ConcurrencyLimits{Parallelism: 1}, tasks)
	if explicit.Parallelism != 1 {
		t.Fatalf("global parallelism = %d, want the caller-supplied 1 to win over the environment override", explicit.Parallelism)
	}
}

func TestResolveApplyConcurrencyLimitsIgnoresUnusableEnvironmentValues(t *testing.T) {
	tasks := wideApplyTaskGraph(40)
	for _, raw := range []string{"", "  ", "0", "-4", "many"} {
		t.Setenv(ParallelismEnvVar, raw)
		limits := ResolveApplyConcurrencyLimits(ConcurrencyLimits{}, tasks)
		if limits.Parallelism != DefaultParallelism() {
			t.Fatalf("%s=%q resolved global parallelism to %d, want the %d default", ParallelismEnvVar, raw, limits.Parallelism, DefaultParallelism())
		}
	}
	t.Setenv(ParallelismEnvVar, "1000")
	if got := ResolveApplyConcurrencyLimits(ConcurrencyLimits{}, tasks).Parallelism; got != 40 {
		t.Fatalf("an environment override above the graph maximum resolved to %d, want 40", got)
	}
}

func TestResolveApplyConcurrencyLimitsKeepsEveryTaskDispatchable(t *testing.T) {
	tasks := []ApplyTask{
		{
			Entry:         TaskLedgerEntry{ID: "fat", HostSlotKey: "host:bastion:machine", HostSlotCount: DefaultParallelismPerHost + 2},
			HostSlotKey:   "host:bastion:machine",
			HostSlotCount: DefaultParallelismPerHost + 2,
		},
		{Entry: TaskLedgerEntry{ID: "thin", HostSlotKey: "host:bastion:machine", HostSlotCount: 1}},
	}
	limits := ResolveApplyConcurrencyLimits(ConcurrencyLimits{ParallelismPerHost: 1}, tasks)
	if limits.ParallelismPerHost < DefaultParallelismPerHost+2 {
		t.Fatalf("per-host limit = %d, want at least the %d slots a single task demands", limits.ParallelismPerHost, DefaultParallelismPerHost+2)
	}
	if limits.ParallelismPerHost > limits.AutoParallelismPerHost {
		t.Fatalf("per-host limit = %d exceeds the %d slots the graph can use", limits.ParallelismPerHost, limits.AutoParallelismPerHost)
	}
	if !taskHostSlotAvailable(tasks[0], map[string]int{}, limits.ParallelismPerHost) {
		t.Fatal("the widest single task must be dispatchable on an idle scheduler or the run deadlocks and blocks every task")
	}
	for _, task := range tasks {
		if !taskRedfishAvailable(task, 0, limits.ParallelismRedfish) {
			t.Fatalf("task %s must be dispatchable within the resolved Redfish budget", task.Entry.ID)
		}
		if !taskResourcesAvailable(task, map[string]int{}) {
			t.Fatalf("task %s must be dispatchable with no resource key held", task.Entry.ID)
		}
	}
}

func TestDefaultParallelismStaysWithinItsClamp(t *testing.T) {
	got := DefaultParallelism()
	if got < minDefaultParallelism || got > maxDefaultParallelism {
		t.Fatalf("DefaultParallelism() = %d, want it clamped to [%d,%d]", got, minDefaultParallelism, maxDefaultParallelism)
	}
	if cpus := runtime.NumCPU() * 2; cpus >= minDefaultParallelism && cpus <= maxDefaultParallelism && got != cpus {
		t.Fatalf("DefaultParallelism() = %d, want %d for %d CPUs", got, cpus, runtime.NumCPU())
	}
}
