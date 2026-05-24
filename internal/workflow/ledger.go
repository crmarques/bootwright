package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/crmarques/bootwright/internal/safefs"
)

const LedgerRelativePath = "workflow/current-apply.json"

type RunStatus string

const (
	RunStatusRunning   RunStatus = "running"
	RunStatusOK        RunStatus = "ok"
	RunStatusFailed    RunStatus = "failed"
	RunStatusCancelled RunStatus = "cancelled"
)

type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusReady     TaskStatus = "ready"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusBlocked   TaskStatus = "blocked"
	TaskStatusSkipped   TaskStatus = "skipped"
	TaskStatusOK        TaskStatus = "ok"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusCancelled TaskStatus = "cancelled"
)

type ConcurrencyLimits struct {
	Parallelism        int `json:"parallelism"`
	ParallelismPerHost int `json:"parallelismPerHost"`
	ParallelismRedfish int `json:"parallelismRedfish"`
}

type RunLedger struct {
	RunID     string            `json:"runId"`
	Target    string            `json:"target"`
	Scope     string            `json:"scope,omitempty"`
	Status    RunStatus         `json:"status"`
	StartedAt time.Time         `json:"startedAt"`
	EndedAt   *time.Time        `json:"endedAt,omitempty"`
	Limits    ConcurrencyLimits `json:"limits"`
	Tasks     []TaskLedgerEntry `json:"tasks"`
}

type TaskLedgerEntry struct {
	ID             string     `json:"id"`
	Kind           string     `json:"kind"`
	Label          string     `json:"label"`
	Cluster        string     `json:"cluster,omitempty"`
	Node           string     `json:"node,omitempty"`
	Host           string     `json:"host,omitempty"`
	ResourceKeys   []string   `json:"resourceKeys,omitempty"`
	Status         TaskStatus `json:"status"`
	Dependencies   []string   `json:"dependencies,omitempty"`
	StartedAt      *time.Time `json:"startedAt,omitempty"`
	EndedAt        *time.Time `json:"endedAt,omitempty"`
	LogPath        string     `json:"logPath,omitempty"`
	ClusterLogPath string     `json:"clusterLogPath,omitempty"`
	Failure        string     `json:"failure,omitempty"`
	SkippedReason  string     `json:"skippedReason,omitempty"`
}

type ProgressCount struct {
	Status TaskStatus
	Count  int
}

func NewRunLedger(runID, target, scope string, limits ConcurrencyLimits, tasks []TaskLedgerEntry, now time.Time) RunLedger {
	entries := make([]TaskLedgerEntry, len(tasks))
	copy(entries, tasks)
	for i := range entries {
		if entries[i].Status == "" {
			entries[i].Status = TaskStatusPending
		}
	}
	return RunLedger{
		RunID:     runID,
		Target:    target,
		Scope:     scope,
		Status:    RunStatusRunning,
		StartedAt: now.UTC(),
		Limits:    limits,
		Tasks:     entries,
	}
}

func LedgerPath(stateDir string) string {
	return filepath.Join(stateDir, LedgerRelativePath)
}

func LoadRunLedger(stateDir string) (RunLedger, bool, error) {
	path := LedgerPath(stateDir)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return RunLedger{}, false, nil
	}
	if err != nil {
		return RunLedger{}, false, fmt.Errorf("read apply ledger: %w", err)
	}
	var ledger RunLedger
	if err := json.Unmarshal(data, &ledger); err != nil {
		return RunLedger{}, true, fmt.Errorf("decode apply ledger %s: %w", path, err)
	}
	return ledger, true, nil
}

func SaveRunLedger(stateDir string, ledger RunLedger) error {
	path := LedgerPath(stateDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create apply ledger directory: %w", err)
	}
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("chmod apply ledger directory: %w", err)
	}
	data, err := json.MarshalIndent(ledger, "", "  ")
	if err != nil {
		return fmt.Errorf("encode apply ledger: %w", err)
	}
	data = append(data, '\n')
	if err := safefs.AtomicWriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write apply ledger: %w", err)
	}
	return nil
}

func (l RunLedger) Terminal() bool {
	switch l.Status {
	case RunStatusOK, RunStatusFailed, RunStatusCancelled:
		return true
	default:
		return false
	}
}

func (l RunLedger) Active() bool {
	return l.Status == RunStatusRunning
}

func (l RunLedger) ProgressCounts() []ProgressCount {
	counts := map[TaskStatus]int{}
	for _, task := range l.Tasks {
		counts[task.Status]++
	}
	order := []TaskStatus{
		TaskStatusOK,
		TaskStatusRunning,
		TaskStatusReady,
		TaskStatusPending,
		TaskStatusBlocked,
		TaskStatusSkipped,
		TaskStatusFailed,
		TaskStatusCancelled,
	}
	out := make([]ProgressCount, 0, len(order))
	for _, status := range order {
		if counts[status] > 0 {
			out = append(out, ProgressCount{Status: status, Count: counts[status]})
		}
	}
	return out
}

func (l RunLedger) Task(id string) (TaskLedgerEntry, bool) {
	for _, task := range l.Tasks {
		if task.ID == id {
			return task, true
		}
	}
	return TaskLedgerEntry{}, false
}

func (l *RunLedger) MarkReady(id string) {
	l.updateTask(id, func(task *TaskLedgerEntry) {
		task.Status = TaskStatusReady
	})
}

func (l *RunLedger) MarkRunning(id, logPath string, now time.Time) {
	l.updateTask(id, func(task *TaskLedgerEntry) {
		task.Status = TaskStatusRunning
		t := now.UTC()
		task.StartedAt = &t
		task.EndedAt = nil
		task.LogPath = logPath
		task.Failure = ""
		task.SkippedReason = ""
	})
}

func (l *RunLedger) MarkOK(id string, now time.Time) {
	l.updateTask(id, func(task *TaskLedgerEntry) {
		task.Status = TaskStatusOK
		t := now.UTC()
		task.EndedAt = &t
	})
}

func (l *RunLedger) MarkFailed(id, failure string, now time.Time) {
	l.updateTask(id, func(task *TaskLedgerEntry) {
		task.Status = TaskStatusFailed
		t := now.UTC()
		task.EndedAt = &t
		task.Failure = failure
	})
	l.BlockDependents(id, now)
}

func (l *RunLedger) MarkSkipped(id, reason string, now time.Time) {
	l.updateTask(id, func(task *TaskLedgerEntry) {
		task.Status = TaskStatusSkipped
		t := now.UTC()
		task.EndedAt = &t
		task.SkippedReason = reason
	})
}

func (l *RunLedger) Finish(status RunStatus, now time.Time) {
	l.Status = status
	t := now.UTC()
	l.EndedAt = &t
}

func (l *RunLedger) BlockDependents(failedID string, now time.Time) {
	blocked := map[string]bool{failedID: true}
	changed := true
	for changed {
		changed = false
		for i := range l.Tasks {
			task := &l.Tasks[i]
			if taskTerminal(task.Status) {
				continue
			}
			for _, dep := range task.Dependencies {
				if blocked[dep] {
					task.Status = TaskStatusBlocked
					t := now.UTC()
					task.EndedAt = &t
					task.SkippedReason = "dependency " + dep + " failed"
					blocked[task.ID] = true
					changed = true
					break
				}
			}
		}
	}
}

func (l RunLedger) FailedTasks() []TaskLedgerEntry {
	return l.tasksByStatus(TaskStatusFailed)
}

func (l RunLedger) BlockedTasks() []TaskLedgerEntry {
	return l.tasksByStatus(TaskStatusBlocked)
}

func (l RunLedger) RunningTasks() []TaskLedgerEntry {
	return l.tasksByStatus(TaskStatusRunning)
}

func (l RunLedger) ClusterNames() []string {
	seen := map[string]bool{}
	for _, task := range l.Tasks {
		if task.Cluster != "" {
			seen[task.Cluster] = true
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (l RunLedger) TasksForCluster(cluster string) []TaskLedgerEntry {
	var out []TaskLedgerEntry
	for _, task := range l.Tasks {
		if task.Cluster == cluster {
			out = append(out, task)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (l RunLedger) tasksByStatus(status TaskStatus) []TaskLedgerEntry {
	var out []TaskLedgerEntry
	for _, task := range l.Tasks {
		if task.Status == status {
			out = append(out, task)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (l *RunLedger) updateTask(id string, fn func(*TaskLedgerEntry)) {
	for i := range l.Tasks {
		if l.Tasks[i].ID == id {
			fn(&l.Tasks[i])
			return
		}
	}
}

func taskTerminal(status TaskStatus) bool {
	switch status {
	case TaskStatusOK, TaskStatusFailed, TaskStatusBlocked, TaskStatusSkipped, TaskStatusCancelled:
		return true
	default:
		return false
	}
}
