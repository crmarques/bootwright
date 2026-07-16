package workflow

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/crmarques/bootwright/internal/converge/ansible"
	"github.com/crmarques/bootwright/internal/render"
)

const (
	applyLogName          = "bootwright.log"
	applyClusterLogPrefix = "bootwright-"
)

type applyLogSet struct {
	files    []*os.File
	run      io.Writer
	clusters map[string]io.Writer
}

func openApplyLogs(runsDir string, ledger RunLedger) (*applyLogSet, error) {
	logs := &applyLogSet{clusters: map[string]io.Writer{}}
	runFile, err := openApplyLogFile(ApplyRunLogPath(runsDir, ledger.RunID), "run log")
	if err != nil {
		return nil, err
	}
	logs.files = append(logs.files, runFile)
	logs.run = &lockedApplyWriter{mu: &sync.Mutex{}, w: runFile}
	for _, name := range ledger.ClusterNames() {
		file, err := openApplyLogFile(ApplyClusterLogPath(runsDir, ledger.RunID, name), "cluster log")
		if err != nil {
			logs.Close()
			return nil, err
		}
		logs.files = append(logs.files, file)
		logs.clusters[name] = &lockedApplyWriter{mu: &sync.Mutex{}, w: file}
	}
	return logs, nil
}

func openApplyLogFile(path string, label string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create apply %s directory: %w", label, err)
	}
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("chmod apply %s directory: %w", label, err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create apply %s: %w", label, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("chmod apply %s: %w", label, err)
	}
	return file, nil
}

func (s *applyLogSet) Writer(cluster string) io.Writer {
	if s == nil {
		return io.Discard
	}
	if cluster != "" {
		if writer := s.clusters[cluster]; writer != nil {
			return writer
		}
	}
	if s.run != nil {
		return s.run
	}
	return io.Discard
}

func (s *applyLogSet) writeRunLine(line string) {
	if s == nil || s.run == nil {
		return
	}
	_, _ = io.WriteString(s.run, line+"\n")
}

func applyClusterInitiatedLine(now time.Time, cluster, logPath string) string {
	return fmt.Sprintf("%s %s apply initiated. flow logs in: %s", applyLogTimestamp(now), cluster, logPath)
}

func applyClusterFinishedLine(now time.Time, cluster string, ok bool) string {
	outcome := "finished successfully"
	if !ok {
		outcome = "failed"
	}
	return fmt.Sprintf("%s %s apply %s", applyLogTimestamp(now), cluster, outcome)
}

func applyLogTimestamp(now time.Time) string {
	return now.UTC().Format(time.RFC3339)
}

func clusterApplyTerminal(ledger RunLedger, cluster string) (done bool, ok bool) {
	tasks := ledger.TasksForCluster(cluster)
	if len(tasks) == 0 {
		return false, false
	}
	ok = true
	for _, task := range tasks {
		if !taskTerminal(task.Status) {
			return false, false
		}
		switch task.Status {
		case TaskStatusOK, TaskStatusSkipped:
		default:
			ok = false
		}
	}
	return true, ok
}

func flushFinishedClusterMarkers(logs *applyLogSet, ledger RunLedger, initiated, finished map[string]bool, now time.Time) {
	for cluster := range initiated {
		if finished[cluster] {
			continue
		}
		done, ok := clusterApplyTerminal(ledger, cluster)
		if !done {
			continue
		}
		finished[cluster] = true
		logs.writeRunLine(applyClusterFinishedLine(now, cluster, ok))
	}
}

func (s *applyLogSet) Close() error {
	if s == nil {
		return nil
	}
	var firstErr error
	for _, file := range s.files {
		if err := file.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

type lockedApplyWriter struct {
	mu *sync.Mutex
	w  io.Writer
}

func (w *lockedApplyWriter) Write(p []byte) (int, error) {
	if w == nil || w.w == nil {
		return len(p), nil
	}
	if w.mu != nil {
		w.mu.Lock()
		defer w.mu.Unlock()
	}
	return w.w.Write(p)
}

func TaskLogPath(runsDir, runID, taskID string) string {
	return filepath.Join(runsDir, "history", runID, "tasks", taskID, ansible.OutputLogName)
}

func ApplyRunLogPath(runsDir, runID string) string {
	return filepath.Join(runsDir, "history", runID, applyLogName)
}

func ApplyClusterLogPath(runsDir, runID, cluster string) string {
	return filepath.Join(runsDir, "history", runID, applyClusterLogPrefix+cluster+".log")
}

func OpenShiftInstallerLogPath(clustersDir, cluster string) string {
	return filepath.Join(clustersDir, cluster, "runtime", render.RuntimeRelativeDir, ".openshift_install.log")
}

func PreflightLogPath(runsDir, scopeName string) string {
	return filepath.Join(runsDir, "preflight", scopeName, ansible.OutputLogName)
}

func DestroyLogPath(runsDir, baseName string) string {
	return filepath.Join(runsDir, "destroy", baseName, ansible.OutputLogName)
}

func BastionSetupLogPath(runsDir string) string {
	return filepath.Join(runsDir, "bastion", "setup", ansible.OutputLogName)
}
