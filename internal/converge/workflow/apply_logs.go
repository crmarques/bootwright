package workflow

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/crmarques/bootwright/internal/converge/ansible"
	"github.com/crmarques/bootwright/internal/render"
)

const applyLogName = "bootwright.log"

type applyLogSet struct {
	files    []*os.File
	run      io.Writer
	clusters map[string]io.Writer
}

func openApplyLogs(runsDir string, clustersDir string, ledger RunLedger) (*applyLogSet, error) {
	logs := &applyLogSet{clusters: map[string]io.Writer{}}
	runFile, err := openApplyLogFile(ApplyRunLogPath(runsDir, ledger.RunID), "run log")
	if err != nil {
		return nil, err
	}
	logs.files = append(logs.files, runFile)
	logs.run = &lockedApplyWriter{mu: &sync.Mutex{}, w: runFile}
	for _, name := range ledger.ClusterNames() {
		file, err := openApplyLogFile(ApplyClusterLogPath(clustersDir, ledger.RunID, name), "cluster log")
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
	var writers []io.Writer
	if s.run != nil {
		writers = append(writers, s.run)
	}
	if cluster != "" {
		if writer := s.clusters[cluster]; writer != nil {
			writers = append(writers, writer)
		}
	}
	switch len(writers) {
	case 0:
		return io.Discard
	case 1:
		return writers[0]
	default:
		return io.MultiWriter(writers...)
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

func taskLogWriter(path string) (io.Writer, func() error, error) {
	file, err := openApplyLogFile(path, "task log")
	if err != nil {
		return nil, nil, err
	}
	return &lockedApplyWriter{mu: &sync.Mutex{}, w: file}, file.Close, nil
}

func TaskLogPath(runsDir, runID, taskID string) string {
	return filepath.Join(runsDir, "history", runID, "tasks", taskID, ansible.OutputLogName)
}

func ApplyRunLogPath(runsDir, runID string) string {
	return filepath.Join(runsDir, "history", runID, applyLogName)
}

func ApplyClusterLogPath(clustersDir, runID, cluster string) string {
	return filepath.Join(clustersDir, cluster, "runs", runID, applyLogName)
}

func OpenShiftInstallerLogPath(clustersDir, cluster string) string {
	return filepath.Join(clustersDir, cluster, "runtime", render.RuntimeRelativeDir, ".openshift_install.log")
}
