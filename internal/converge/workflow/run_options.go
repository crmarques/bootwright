package workflow

import (
	"os"
	"runtime"
	"strconv"
	"strings"
)

const (
	ParallelismEnvVar         = "BOOTWRIGHT_APPLY_PARALLELISM"
	ParallelismPerHostEnvVar  = "BOOTWRIGHT_APPLY_PARALLELISM_PER_HOST"
	ParallelismRedfishEnvVar  = "BOOTWRIGHT_APPLY_PARALLELISM_REDFISH"
	ParallelismClustersEnvVar = "BOOTWRIGHT_APPLY_PARALLELISM_CLUSTERS"
)

const (
	DefaultParallelismPerHost = 4
	DefaultParallelismRedfish = 8

	minDefaultParallelism = 8
	maxDefaultParallelism = 32
)

func DefaultParallelism() int {
	value := runtime.NumCPU() * 2
	if value < minDefaultParallelism {
		return minDefaultParallelism
	}
	if value > maxDefaultParallelism {
		return maxDefaultParallelism
	}
	return value
}

func ResolveApplyConcurrencyLimits(limits ConcurrencyLimits, tasks []ApplyTask) ConcurrencyLimits {
	autoGlobal := autoParallelism(len(tasks))
	autoPerHost := hostSlotAutoParallelism(tasks)
	autoRedfish := nodeBootTaskCount(tasks)
	if autoRedfish < 1 {
		autoRedfish = 1
	}
	limits.Parallelism = boundedLimit(requestedLimit(limits.Parallelism, ParallelismEnvVar), DefaultParallelism(), autoGlobal, 1)
	limits.ParallelismPerHost = boundedLimit(requestedLimit(limits.ParallelismPerHost, ParallelismPerHostEnvVar), DefaultParallelismPerHost, autoPerHost, hostSlotDispatchFloor(tasks))
	limits.ParallelismRedfish = boundedLimit(requestedLimit(limits.ParallelismRedfish, ParallelismRedfishEnvVar), DefaultParallelismRedfish, autoRedfish, 1)
	limits.ParallelismClusters = resolveClusterInstallLimit(requestedLimit(limits.ParallelismClusters, ParallelismClustersEnvVar), clusterInstallAutoParallelism(TaskLedgerEntries(tasks)))
	limits.AutoParallelism = autoGlobal
	limits.AutoParallelismPerHost = autoPerHost
	limits.AutoParallelismRedfish = autoRedfish
	return limits
}

func resolveClusterInstallLimit(requested, auto int) int {
	if auto < 1 {
		return 0
	}
	return boundedLimit(requested, auto, auto, 1)
}

func clusterInstallAutoParallelism(entries []TaskLedgerEntry) int {
	clusters := map[string]bool{}
	for _, entry := range entries {
		if cluster := clusterInstallKey(entry); cluster != "" {
			clusters[cluster] = true
		}
	}
	return len(clusters)
}

func clusterInstallKey(entry TaskLedgerEntry) string {
	switch entry.Kind {
	case ApplyTaskKindClusterISO, ApplyTaskKindNodeBoot, ApplyTaskKindBootstrapWait, ApplyTaskKindInstallWait:
		return entry.Cluster
	default:
		return ""
	}
}

func requestedLimit(value int, envVar string) int {
	if value > 0 {
		return value
	}
	return concurrencyEnvLimit(envVar)
}

func concurrencyEnvLimit(name string) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return 0
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return 0
	}
	return value
}

func boundedLimit(requested, fallback, auto, floor int) int {
	value := requested
	if value < 1 {
		value = fallback
	}
	if value < 1 || value > auto {
		value = auto
	}
	if value < floor {
		value = floor
	}
	return value
}

func autoParallelism(taskCount int) int {
	if taskCount < 1 {
		return 1
	}
	return taskCount
}

func nodeBootTaskCount(tasks []ApplyTask) int {
	count := 0
	for _, task := range tasks {
		if task.RedfishSlots > 0 {
			count += task.RedfishSlots
		}
	}
	return count
}

func hostSlotAutoParallelism(tasks []ApplyTask) int {
	counts := map[string]int{}
	maxCount := 1
	for _, task := range tasks {
		key, count := taskHostSlot(task)
		if key == "" {
			continue
		}
		counts[key] += count
		if counts[key] > maxCount {
			maxCount = counts[key]
		}
	}
	return maxCount
}

func hostSlotDispatchFloor(tasks []ApplyTask) int {
	floor := 1
	for _, task := range tasks {
		if _, count := taskHostSlot(task); count > floor {
			floor = count
		}
	}
	return floor
}
