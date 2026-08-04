package converge

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func purgeClusterRuntimeDir(clustersDir, cluster string, keepMachineState bool) error {
	if strings.TrimSpace(clustersDir) == "" || strings.TrimSpace(cluster) == "" {
		return nil
	}
	root := filepath.Join(clustersDir, cluster)
	if !keepMachineState {
		if err := os.RemoveAll(root); err != nil {
			return fmt.Errorf("remove cluster history directory: %w", err)
		}
		return nil
	}
	if err := removeDirContentsExcept(root, workflow.ClusterProviderStateDir(clustersDir, cluster)); err != nil {
		return fmt.Errorf("remove cluster history directory: %w", err)
	}
	return pruneEmptyClusterStateDirs(clustersDir, cluster)
}

func removeDirContentsExcept(root, keep string) error {
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var problems []error
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		switch {
		case path == keep:
		case entry.IsDir() && strings.HasPrefix(keep, path+string(os.PathSeparator)):
			problems = append(problems, removeDirContentsExcept(path, keep))
		default:
			problems = append(problems, os.RemoveAll(path))
		}
	}
	return errors.Join(problems...)
}

func pruneEmptyClusterStateDirs(clustersDir, cluster string) error {
	if strings.TrimSpace(clustersDir) == "" || strings.TrimSpace(cluster) == "" {
		return nil
	}
	if err := pruneEmptyDirs(filepath.Join(clustersDir, cluster)); err != nil {
		return fmt.Errorf("prune emptied cluster state directories: %w", err)
	}
	return nil
}

func pruneEmptyDirs(root string) error {
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var problems []error
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		problems = append(problems, pruneEmptyDirs(filepath.Join(root, entry.Name())))
	}
	if err := errors.Join(problems...); err != nil {
		return err
	}
	remaining, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	if len(remaining) > 0 {
		return nil
	}
	if err := os.Remove(root); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func purgeAllRunHistory(runsDir, keepRunID string) error {
	if strings.TrimSpace(runsDir) == "" {
		return nil
	}
	historyDir := filepath.Join(runsDir, "history")
	entries, err := os.ReadDir(historyDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("list run history: %w", err)
	}
	var problems []error
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == keepRunID {
			continue
		}
		if err := os.RemoveAll(filepath.Join(historyDir, entry.Name())); err != nil {
			problems = append(problems, fmt.Errorf("purge run history %s: %w", entry.Name(), err))
		}
	}
	return errors.Join(problems...)
}

func purgeRunHistoryForComponents(runsDir string, clusterNames, machineNames []string, keepRunID string) error {
	if strings.TrimSpace(runsDir) == "" || (len(clusterNames) == 0 && len(machineNames) == 0) {
		return nil
	}
	clusters := toPurgeNameSet(clusterNames)
	machines := toPurgeNameSet(machineNames)
	historyDir := filepath.Join(runsDir, "history")
	entries, err := os.ReadDir(historyDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("list run history: %w", err)
	}
	var problems []error
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == keepRunID {
			continue
		}
		runID := entry.Name()
		if err := purgeRunHistoryEntry(runsDir, historyDir, runID, clusters, machines); err != nil {
			problems = append(problems, fmt.Errorf("purge run history %s: %w", runID, err))
		}
	}
	return errors.Join(problems...)
}

func purgeRunHistoryEntry(runsDir, historyDir, runID string, clusters, machines map[string]bool) error {
	ledgerPath := filepath.Join(historyDir, runID, "ledger.json")
	data, err := os.ReadFile(ledgerPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read ledger: %w", err)
	}
	var ledger workflow.RunLedger
	if err := json.Unmarshal(data, &ledger); err != nil {
		return fmt.Errorf("decode ledger: %w", err)
	}
	matched := 0
	for _, task := range ledger.Tasks {
		if taskInPurgeScope(task, clusters, machines) {
			matched++
		}
	}
	if matched == 0 {
		return nil
	}
	runDir := filepath.Join(historyDir, runID)
	if matched == len(ledger.Tasks) {
		if err := os.RemoveAll(runDir); err != nil {
			return fmt.Errorf("remove run directory: %w", err)
		}
		return nil
	}
	var problems []error
	purgedClusterLogs := map[string]bool{}
	for _, task := range ledger.Tasks {
		if !taskInPurgeScope(task, clusters, machines) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(runDir, "tasks", task.ID)); err != nil && !os.IsNotExist(err) {
			problems = append(problems, err)
		}
		if task.Cluster == "" || purgedClusterLogs[task.Cluster] {
			continue
		}
		purgedClusterLogs[task.Cluster] = true
		if err := os.Remove(workflow.ApplyClusterLogPath(runsDir, runID, task.Cluster)); err != nil && !os.IsNotExist(err) {
			problems = append(problems, err)
		}
	}
	return errors.Join(problems...)
}

func taskInPurgeScope(task workflow.TaskLedgerEntry, clusters, machines map[string]bool) bool {
	if task.Cluster != "" && clusters[task.Cluster] {
		return true
	}
	if task.Node != "" && machines[task.Node] {
		return true
	}
	if !workflow.IsDestroyTaskKind(task.Kind) {
		return false
	}
	for _, cluster := range workflow.DestroyTaskClusterKeys(task) {
		if clusters[cluster] {
			return true
		}
	}
	machineKeys := workflow.DestroyTaskMachineKeys(task)
	if len(machineKeys) == 0 || len(machines) == 0 {
		return false
	}
	for _, machine := range machineKeys {
		if !machines[machine] {
			return false
		}
	}
	return true
}

func toPurgeNameSet(names []string) map[string]bool {
	set := make(map[string]bool, len(names))
	for _, name := range names {
		set[name] = true
	}
	return set
}
