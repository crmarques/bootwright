package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/crmarques/bootwright/internal/host/safefs"
)

const (
	LedgerRelativePath = "current.json"
	LeaseRelativePath  = "current.lease.json"

	ApplyLeaseStaleAfter        = 2 * time.Minute
	ApplyLeaseHeartbeatInterval = 15 * time.Second
)

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
	Parallelism            int `json:"parallelism"`
	ParallelismPerHost     int `json:"parallelismPerHost"`
	ParallelismRedfish     int `json:"parallelismRedfish"`
	ParallelismClusters    int `json:"parallelismClusters,omitempty"`
	AutoParallelism        int `json:"autoParallelism,omitempty"`
	AutoParallelismPerHost int `json:"autoParallelismPerHost,omitempty"`
	AutoParallelismRedfish int `json:"autoParallelismRedfish,omitempty"`
}

func (c ConcurrencyLimits) ParallelismUnbounded() bool {
	return limitUnbounded(c.Parallelism, c.AutoParallelism)
}

func (c ConcurrencyLimits) ParallelismPerHostUnbounded() bool {
	return limitUnbounded(c.ParallelismPerHost, c.AutoParallelismPerHost)
}

func (c ConcurrencyLimits) ParallelismRedfishUnbounded() bool {
	return limitUnbounded(c.ParallelismRedfish, c.AutoParallelismRedfish)
}

func limitUnbounded(value, auto int) bool {
	return auto > 0 && value >= auto
}

type RunLedger struct {
	RunID          string            `json:"runId"`
	Target         string            `json:"target"`
	Scope          string            `json:"scope,omitempty"`
	Machines       []string          `json:"machines,omitempty"`
	InvocationArgs []string          `json:"invocationArgs,omitempty"`
	Recovery       RunRecoveryPlan   `json:"recovery,omitempty"`
	Status         RunStatus         `json:"status"`
	StartedAt      time.Time         `json:"startedAt"`
	EndedAt        *time.Time        `json:"endedAt,omitempty"`
	Limits         ConcurrencyLimits `json:"limits"`
	Tasks          []TaskLedgerEntry `json:"tasks"`

	taskIndex map[string]int
}

type RunLease struct {
	RunID        string    `json:"runId"`
	Hostname     string    `json:"hostname,omitempty"`
	PID          int       `json:"pid"`
	ProcessStart string    `json:"processStart,omitempty"`
	StartedAt    time.Time `json:"startedAt"`
	HeartbeatAt  time.Time `json:"heartbeatAt"`
}

type RunActivityState string

const (
	RunActivityTerminal RunActivityState = "terminal"
	RunActivityActive   RunActivityState = "active"
	RunActivityStale    RunActivityState = "stale"
)

type RunActivity struct {
	State  RunActivityState
	Detail string
	Lease  *RunLease
}

type TaskLedgerEntry struct {
	ID                   string     `json:"id"`
	Kind                 string     `json:"kind"`
	Label                string     `json:"label"`
	Cluster              string     `json:"cluster,omitempty"`
	ClusterKind          string     `json:"clusterKind,omitempty"`
	Addon                string     `json:"addon,omitempty"`
	Node                 string     `json:"node,omitempty"`
	Nodes                []string   `json:"nodes,omitempty"`
	Host                 string     `json:"host,omitempty"`
	ResourceKeys         []string   `json:"resourceKeys,omitempty"`
	HostSlotKey          string     `json:"hostSlotKey,omitempty"`
	HostSlotCount        int        `json:"hostSlotCount,omitempty"`
	Status               TaskStatus `json:"status"`
	Dependencies         []string   `json:"dependencies,omitempty"`
	SuccessDependencies  []string   `json:"successDependencies,omitempty"`
	OrderingDependencies []string   `json:"orderingDependencies,omitempty"`
	ReadyAt              *time.Time `json:"readyAt,omitempty"`
	BlockedOn            []string   `json:"blockedOn,omitempty"`
	StartedAt            *time.Time `json:"startedAt,omitempty"`
	EndedAt              *time.Time `json:"endedAt,omitempty"`
	LogPath              string     `json:"logPath,omitempty"`
	ClusterLogPath       string     `json:"clusterLogPath,omitempty"`
	Failure              string     `json:"failure,omitempty"`
	SkippedReason        string     `json:"skippedReason,omitempty"`
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
	index := make(map[string]int, len(entries))
	for i := range entries {
		if _, seen := index[entries[i].ID]; !seen {
			index[entries[i].ID] = i
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
		taskIndex: index,
	}
}

func LedgerPath(runsDir string) string {
	return filepath.Join(runsDir, LedgerRelativePath)
}

func LeasePath(runsDir string) string {
	return filepath.Join(runsDir, LeaseRelativePath)
}

func LoadRunLedger(runsDir string) (RunLedger, bool, error) {
	path := LedgerPath(runsDir)
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

func NewRunLease(runID string, now time.Time) RunLease {
	hostname, _ := os.Hostname()
	now = now.UTC()
	pid := os.Getpid()
	startToken, _ := processStartToken(pid)
	return RunLease{
		RunID:        runID,
		Hostname:     hostname,
		PID:          pid,
		ProcessStart: startToken,
		StartedAt:    now,
		HeartbeatAt:  now,
	}
}

func LoadRunLease(runsDir string) (RunLease, bool, error) {
	path := LeasePath(runsDir)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return RunLease{}, false, nil
	}
	if err != nil {
		return RunLease{}, false, fmt.Errorf("read apply lease: %w", err)
	}
	var lease RunLease
	if err := json.Unmarshal(data, &lease); err != nil {
		return RunLease{}, true, fmt.Errorf("decode apply lease %s: %w", path, err)
	}
	return lease, true, nil
}

func runLeaseBytes(lease RunLease) ([]byte, error) {
	data, err := json.MarshalIndent(lease, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode apply lease: %w", err)
	}
	return append(data, '\n'), nil
}

func removeRunLease(runsDir string) error {
	err := os.Remove(LeasePath(runsDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("remove apply lease: %w", err)
	}
	return nil
}

var ErrLeaseNotOwned = errors.New("apply lease is held by another run")

func isLeaseNotOwned(err error) bool {
	return errors.Is(err, ErrLeaseNotOwned)
}

var (
	runLeaseLockAttempted = func() {}
	runLeaseOwnerChecked  = func(string) {}
)

func withRunLeaseLock(runsDir string, action func() error) (result error) {
	lockPath := LeasePath(runsDir) + ".lock"
	if err := safefs.EnsureDir(filepath.Dir(lockPath), 0o700); err != nil {
		return fmt.Errorf("create apply lease lock directory: %w", err)
	}
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open apply lease lock: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return fmt.Errorf("chmod apply lease lock: %w", err)
	}
	runLeaseLockAttempted()
	if err := lockRunLeaseFile(file); err != nil {
		_ = file.Close()
		return fmt.Errorf("lock apply lease transaction: %w", err)
	}
	defer func() {
		if err := unlockRunLeaseFile(file); err != nil {
			result = errors.Join(result, fmt.Errorf("unlock apply lease transaction: %w", err))
		}
		if err := file.Close(); err != nil {
			result = errors.Join(result, fmt.Errorf("close apply lease lock: %w", err))
		}
	}()
	return action()
}

func SaveRunLeaseIfOwner(runsDir string, lease RunLease) error {
	return withRunLeaseLock(runsDir, func() error {
		existing, found, err := LoadRunLease(runsDir)
		if err != nil {
			return err
		}
		if !found || existing.RunID != lease.RunID {
			return ErrLeaseNotOwned
		}
		runLeaseOwnerChecked("save")
		data, err := runLeaseBytes(lease)
		if err != nil {
			return err
		}
		if err := safefs.WriteFileEnsuringDir(LeasePath(runsDir), data, 0o600); err != nil {
			return fmt.Errorf("write apply lease: %w", err)
		}
		return nil
	})
}

func RemoveRunLeaseIfOwner(runsDir, runID string) error {
	return withRunLeaseLock(runsDir, func() error {
		existing, found, err := LoadRunLease(runsDir)
		if err != nil {
			return err
		}
		if !found {
			return nil
		}
		if existing.RunID != runID {
			return ErrLeaseNotOwned
		}
		runLeaseOwnerChecked("remove")
		return removeRunLease(runsDir)
	})
}

func leaseLiveness(lease RunLease, now time.Time) (RunActivityState, string) {
	if localLeaseProcessAlive(lease) {
		return RunActivityActive, "apply lease process is running"
	}
	if lease.HeartbeatAt.IsZero() {
		return RunActivityStale, "apply lease has no heartbeat"
	}
	if localLeaseProcessMissing(lease) {
		return RunActivityStale, "apply lease process is not running"
	}
	if now.UTC().Sub(lease.HeartbeatAt.UTC()) > ApplyLeaseStaleAfter {
		return RunActivityStale, "apply lease heartbeat is stale"
	}
	return RunActivityActive, "apply lease heartbeat is fresh"
}

func leaseFresh(lease RunLease, now time.Time) bool {
	state, _ := leaseLiveness(lease, now)
	return state == RunActivityActive
}

func AcquireRunLease(runsDir string, lease RunLease, now time.Time) error {
	return withRunLeaseLock(runsDir, func() error {
		path := LeasePath(runsDir)
		existing, found, err := LoadRunLease(runsDir)
		if err != nil {
			return err
		}
		if found {
			if leaseFresh(existing, now) {
				return fmt.Errorf("a mutating run (%s) is still running; inspect it with bootwright status --watch", existing.RunID)
			}
			hostname, hostnameErr := os.Hostname()
			if hostnameErr != nil || strings.TrimSpace(hostname) == "" || existing.Hostname != hostname {
				ownerHost := strings.TrimSpace(existing.Hostname)
				if ownerHost == "" {
					ownerHost = "unknown host"
				}
				return fmt.Errorf("refusing automatic takeover of stale mutating run lease %s for run %s owned by %s: this controller cannot prove that controller's process has stopped; inspect that controller and lease, then repair it or remove the lease only after proving no such run remains active, using `sudo rm -f %s`", path, existing.RunID, ownerHost, path)
			}
			processStopped, processDetail := localLeaseProcessProvenStopped(existing)
			if !processStopped {
				return fmt.Errorf("refusing automatic takeover of stale mutating run lease %s for run %s owned by this controller: %s; this controller cannot prove the original process stopped; inspect the process and lease, then repair it or remove the lease only after proving no such run remains active, using `sudo rm -f %s`", path, existing.RunID, processDetail, path)
			}
			staleClaim := path + ".stale-" + lease.RunID
			if err := os.Rename(path, staleClaim); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("claim stale apply lease: %w", err)
			}
			_ = os.Remove(staleClaim)
		}
		data, err := runLeaseBytes(lease)
		if err != nil {
			return err
		}
		if err := safefs.WriteNewFile(path, data, 0o600); err != nil {
			if errors.Is(err, os.ErrExist) {
				return fmt.Errorf("a mutating run acquired the apply lease concurrently; inspect it with bootwright status --watch")
			}
			return fmt.Errorf("acquire apply lease: %w", err)
		}
		return nil
	})
}

func localLeaseProcessProvenStopped(lease RunLease) (bool, string) {
	if lease.PID <= 0 {
		return true, "lease has no process ID"
	}
	if !runLeaseProcessAlive(lease.PID) {
		return true, fmt.Sprintf("process %d is not running", lease.PID)
	}
	if lease.ProcessStart == "" {
		return false, fmt.Sprintf("process %d is still running and the lease has no process-start identity", lease.PID)
	}
	currentStart, ok := runLeaseProcessStartToken(lease.PID)
	if !ok || currentStart == "" {
		return false, fmt.Sprintf("process %d is still running and its current process-start identity is unreadable", lease.PID)
	}
	if currentStart == lease.ProcessStart {
		return false, fmt.Sprintf("process %d still matches the lease process-start identity", lease.PID)
	}
	return true, fmt.Sprintf("process %d has been reused by a different process", lease.PID)
}

func runLedgerBytes(ledger RunLedger) ([]byte, error) {
	data, err := json.MarshalIndent(ledger, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func SaveRunLedger(runsDir string, ledger RunLedger) error {
	data, err := runLedgerBytes(ledger)
	if err != nil {
		return fmt.Errorf("encode apply ledger: %w", err)
	}
	return saveRunLedgerBytes(runsDir, data)
}

func saveRunLedgerBytes(runsDir string, data []byte) error {
	if err := safefs.WriteFileEnsuringDir(LedgerPath(runsDir), data, 0o600); err != nil {
		return fmt.Errorf("write apply ledger: %w", err)
	}
	return nil
}

func ArchiveRunLedger(runsDir string, ledger RunLedger) error {
	data, err := runLedgerBytes(ledger)
	if err != nil {
		return fmt.Errorf("encode run ledger archive: %w", err)
	}
	return archiveRunLedgerBytes(runsDir, ledger.RunID, data)
}

func archiveRunLedgerBytes(runsDir, runID string, data []byte) error {
	if err := validateRunIDPathSegment(runID); err != nil {
		return fmt.Errorf("archive run ledger: %w", err)
	}
	path := filepath.Join(runsDir, "history", runID, "ledger.json")
	if err := safefs.WriteFileEnsuringDir(path, data, 0o600); err != nil {
		return fmt.Errorf("write run ledger archive: %w", err)
	}
	return nil
}

func ArchivedRunLedgerPath(runsDir, runID string) string {
	return filepath.Join(runsDir, "history", runID, "ledger.json")
}

func LoadArchivedRunLedger(runsDir, runID string) (RunLedger, bool, error) {
	if err := validateRunIDPathSegment(runID); err != nil {
		return RunLedger{}, false, fmt.Errorf("load archived run ledger: %w", err)
	}
	path := ArchivedRunLedgerPath(runsDir, runID)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return RunLedger{}, false, nil
	}
	if err != nil {
		return RunLedger{}, false, fmt.Errorf("read archived run ledger: %w", err)
	}
	var ledger RunLedger
	if err := json.Unmarshal(data, &ledger); err != nil {
		return RunLedger{}, true, fmt.Errorf("decode archived run ledger %s: %w", path, err)
	}
	return ledger, true, nil
}

func validateRunIDPathSegment(runID string) error {
	if strings.TrimSpace(runID) == "" {
		return fmt.Errorf("run id is empty")
	}
	if strings.TrimSpace(runID) != runID || runID == "." || runID == ".." || filepath.Clean(runID) != runID || filepath.Base(runID) != runID || strings.ContainsRune(runID, '\x00') {
		return fmt.Errorf("run id %q is not one clean non-dot path segment", runID)
	}
	return nil
}

func ArchivedRunIDs(runsDir string) []string {
	entries, err := os.ReadDir(filepath.Join(runsDir, "history"))
	if err != nil {
		return nil
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := os.Stat(ArchivedRunLedgerPath(runsDir, entry.Name())); err != nil {
			continue
		}
		ids = append(ids, entry.Name())
	}
	sort.Strings(ids)
	return ids
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

func AssessRunActivity(runsDir string, ledger RunLedger, now time.Time) (RunActivity, error) {
	if !ledger.Active() {
		return RunActivity{State: RunActivityTerminal}, nil
	}
	lease, found, err := LoadRunLease(runsDir)
	if err != nil {
		return RunActivity{}, err
	}
	if !found {
		if !ledger.StartedAt.IsZero() && now.UTC().Sub(ledger.StartedAt.UTC()) <= ApplyLeaseStaleAfter {
			return RunActivity{State: RunActivityActive, Detail: "apply lease is missing but run started recently"}, nil
		}
		return RunActivity{State: RunActivityStale, Detail: "apply lease is missing"}, nil
	}
	if lease.RunID != ledger.RunID {
		return RunActivity{State: RunActivityStale, Detail: fmt.Sprintf("apply lease belongs to %s", lease.RunID), Lease: &lease}, nil
	}
	state, detail := leaseLiveness(lease, now)
	return RunActivity{State: state, Detail: detail, Lease: &lease}, nil
}

var (
	runLeaseProcessAlive      = processAlive
	runLeaseProcessStartToken = processStartToken
)

func localLeaseProcessMissing(lease RunLease) bool {
	if lease.Hostname == "" {
		return false
	}
	hostname, err := os.Hostname()
	if err != nil || hostname == "" || lease.Hostname != hostname {
		return false
	}
	return lease.PID <= 0 || !runLeaseProcessAlive(lease.PID)
}

func localLeaseProcessAlive(lease RunLease) bool {
	if lease.Hostname == "" {
		return false
	}
	hostname, err := os.Hostname()
	if err != nil || hostname == "" || lease.Hostname != hostname {
		return false
	}
	if lease.PID <= 0 || !runLeaseProcessAlive(lease.PID) {
		return false
	}
	if lease.ProcessStart == "" {
		return false
	}
	token, ok := runLeaseProcessStartToken(lease.PID)
	return ok && token == lease.ProcessStart
}

func CancelRunLedger(runsDir string, ledger RunLedger, reason string, now time.Time) (RunLedger, error) {
	for i := range ledger.Tasks {
		if taskTerminal(ledger.Tasks[i].Status) {
			continue
		}
		ledger.Tasks[i].Status = TaskStatusCancelled
		t := now.UTC()
		ledger.Tasks[i].EndedAt = &t
		ledger.Tasks[i].SkippedReason = reason
	}
	ledger.Finish(RunStatusCancelled, now)
	if err := SaveRunLedger(runsDir, ledger); err != nil {
		return ledger, err
	}
	if err := RemoveRunLeaseIfOwner(runsDir, ledger.RunID); err != nil && !isLeaseNotOwned(err) {
		return ledger, err
	}
	return ledger, nil
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
	if i, ok := l.taskPosition(id); ok {
		return l.Tasks[i], true
	}
	return TaskLedgerEntry{}, false
}

func (l RunLedger) taskPosition(id string) (int, bool) {
	if i, ok := l.taskIndex[id]; ok && i >= 0 && i < len(l.Tasks) && l.Tasks[i].ID == id {
		return i, true
	}
	for i := range l.Tasks {
		if l.Tasks[i].ID == id {
			return i, true
		}
	}
	return -1, false
}

func (l *RunLedger) MarkReady(id string) {
	l.updateTask(id, func(task *TaskLedgerEntry) {
		task.Status = TaskStatusReady
	})
}

const maxBlockedOnReasons = 8

func (l *RunLedger) MarkDependencyReady(id string, now time.Time) {
	l.updateTask(id, func(task *TaskLedgerEntry) {
		if task.ReadyAt != nil {
			return
		}
		t := now.UTC()
		task.ReadyAt = &t
	})
}

func (l *RunLedger) RecordBlockedOn(id, reason string) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return
	}
	l.updateTask(id, func(task *TaskLedgerEntry) {
		for i, existing := range task.BlockedOn {
			if existing != reason {
				continue
			}
			task.BlockedOn = append(append(task.BlockedOn[:i:i], task.BlockedOn[i+1:]...), reason)
			return
		}
		if len(task.BlockedOn) >= maxBlockedOnReasons {
			task.BlockedOn = task.BlockedOn[1:]
		}
		task.BlockedOn = append(task.BlockedOn, reason)
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
			for _, dep := range taskBlockingDependencyIDs(*task) {
				if blocked[dep] {
					task.Status = TaskStatusBlocked
					t := now.UTC()
					task.EndedAt = &t
					task.SkippedReason = blockedDependencyReason(*l, dep, failedID)
					blocked[task.ID] = true
					changed = true
					break
				}
			}
		}
	}
}

func blockedDependencyReason(ledger RunLedger, dep, failedID string) string {
	if dep == failedID {
		return "dependency " + dep + " failed"
	}
	if depTask, ok := ledger.Task(dep); ok && depTask.Status == TaskStatusFailed {
		return "dependency " + dep + " failed"
	}
	return "dependency " + dep + " is blocked by failed " + failedID
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
	if i, ok := l.taskPosition(id); ok {
		fn(&l.Tasks[i])
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

type TaskTiming struct {
	Entry       TaskLedgerEntry
	Duration    time.Duration
	QueueWait   time.Duration
	BlockedWait time.Duration
}

type CriticalPathHop struct {
	Timing     TaskTiming
	Cumulative time.Duration
}

type CriticalPath struct {
	Hops  []CriticalPathHop
	Total time.Duration
}

func (l RunLedger) WallClock() time.Duration {
	if l.StartedAt.IsZero() || l.EndedAt == nil {
		return 0
	}
	return nonNegative(l.EndedAt.Sub(l.StartedAt))
}

func (l RunLedger) TaskTimings() []TaskTiming {
	ends := map[string]time.Time{}
	for _, task := range l.Tasks {
		if task.EndedAt != nil {
			ends[task.ID] = task.EndedAt.UTC()
		}
	}
	out := make([]TaskTiming, 0, len(l.Tasks))
	for _, task := range l.Tasks {
		out = append(out, TaskTiming{
			Entry:       task,
			Duration:    taskDuration(task),
			QueueWait:   taskQueueWait(task, ends),
			BlockedWait: taskBlockedWait(task),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Duration != out[j].Duration {
			return out[i].Duration > out[j].Duration
		}
		return out[i].Entry.ID < out[j].Entry.ID
	})
	return out
}

func (l RunLedger) CriticalPath() CriticalPath {
	ordered := l.TaskTimings()
	timings := make(map[string]TaskTiming, len(ordered))
	for _, timing := range ordered {
		timings[timing.Entry.ID] = timing
	}
	best := map[string]time.Duration{}
	prev := map[string]string{}
	visiting := map[string]bool{}
	var solve func(string) time.Duration
	solve = func(id string) time.Duration {
		if value, ok := best[id]; ok {
			return value
		}
		timing, ok := timings[id]
		if !ok || visiting[id] {
			return 0
		}
		visiting[id] = true
		chosen := false
		bestDep := time.Duration(0)
		bestPrev := ""
		for _, dep := range taskDependencyIDs(timing.Entry) {
			if _, known := timings[dep]; !known {
				continue
			}
			total := solve(dep)
			if !chosen || total > bestDep || (total == bestDep && dep < bestPrev) {
				chosen, bestDep, bestPrev = true, total, dep
			}
		}
		visiting[id] = false
		total := bestDep + timing.Duration
		best[id] = total
		prev[id] = bestPrev
		return total
	}
	endID := ""
	total := time.Duration(0)
	for _, timing := range ordered {
		got := solve(timing.Entry.ID)
		if endID == "" || got > total {
			endID, total = timing.Entry.ID, got
		}
	}
	if endID == "" || total <= 0 {
		return CriticalPath{}
	}
	ids := make([]string, 0, len(ordered))
	for id := endID; id != ""; id = prev[id] {
		ids = append(ids, id)
		if len(ids) > len(ordered) {
			break
		}
	}
	hops := make([]CriticalPathHop, 0, len(ids))
	cumulative := time.Duration(0)
	for i := len(ids) - 1; i >= 0; i-- {
		timing := timings[ids[i]]
		cumulative += timing.Duration
		hops = append(hops, CriticalPathHop{Timing: timing, Cumulative: cumulative})
	}
	return CriticalPath{Hops: hops, Total: total}
}

func taskDuration(task TaskLedgerEntry) time.Duration {
	if task.StartedAt == nil || task.EndedAt == nil {
		return 0
	}
	return nonNegative(task.EndedAt.Sub(*task.StartedAt))
}

func taskQueueWait(task TaskLedgerEntry, ends map[string]time.Time) time.Duration {
	if task.StartedAt == nil {
		return 0
	}
	latest := time.Time{}
	for _, dep := range taskDependencyIDs(task) {
		end, ok := ends[dep]
		if ok && end.After(latest) {
			latest = end
		}
	}
	if latest.IsZero() {
		return 0
	}
	return nonNegative(task.StartedAt.UTC().Sub(latest))
}

func taskBlockedWait(task TaskLedgerEntry) time.Duration {
	if task.StartedAt == nil || task.ReadyAt == nil {
		return 0
	}
	return nonNegative(task.StartedAt.Sub(*task.ReadyAt))
}

func nonNegative(d time.Duration) time.Duration {
	if d < 0 {
		return 0
	}
	return d
}
