package converge

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/ownership"
)

const (
	StorageDestroyStatusAttr       = "destroyStatus"
	StorageDestroyStatusPartial    = "partial"
	StorageDestroySkippedNodesAttr = "destroySkippedNodes"
)

type PartialStorageDestroy struct {
	Recorded   []string
	Unrecorded []string
	Clusters   []string
	Skipped    string
	Reasons    []string
	Found      bool
}

func storageDestroyTaskResultPath(runLogPath, taskID string) string {
	return filepath.Join(filepath.Dir(runLogPath), "tasks", taskID, "artifacts", workflow.StorageDestroyResultFileName)
}

func storageDestroyTaskIDs(runLogPath string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(filepath.Dir(runLogPath), "tasks"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list destroy task artifacts: %w", err)
	}
	var out []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name != workflow.DestroyStorageClustersTaskID && !strings.HasPrefix(name, workflow.DestroyStorageClustersTaskID+".") {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

func readStorageDestroyResults(runLogPath string, expected map[string][]string, allowSkipped bool) (map[string]workflow.StorageDestroyClusterResult, bool, error) {
	taskIDs, err := storageDestroyTaskIDs(runLogPath)
	if err != nil {
		return nil, false, err
	}
	if len(expected) == 0 {
		return map[string]workflow.StorageDestroyClusterResult{}, false, nil
	}
	var reports []workflow.StorageDestroyResult
	complete := len(taskIDs) > 0
	for _, taskID := range taskIDs {
		result, ok, err := workflow.ReadStorageDestroyResult(storageDestroyTaskResultPath(runLogPath, taskID))
		if err != nil {
			return nil, false, err
		}
		if !ok {
			complete = false
			continue
		}
		reports = append(reports, result)
	}
	if !complete {
		return nil, false, errors.New("one or more storage teardown tasks produced no completion attestation")
	}
	results, err := workflow.ValidateStorageDestroyResults(reports, expected, allowSkipped)
	if err != nil {
		return nil, false, err
	}
	return results, true, nil
}

func RecordPartialStorageDestroy(ownershipDir, contextName, runLogPath string, expected map[string][]string, expectedSeedHosts map[string]string, allowSkipped bool) (PartialStorageDestroy, error) {
	var out PartialStorageDestroy
	results, ok, err := readStorageDestroyResults(runLogPath, expected, allowSkipped)
	if err != nil {
		return out, err
	}
	out.Found = ok
	if err := workflow.StampPartialStorageDestroyOwnership(ownershipDir, contextName, results, expectedSeedHosts); err != nil {
		return out, err
	}
	for name, result := range results {
		skippedNodes := result.SkippedNodes()
		if len(skippedNodes) == 0 {
			continue
		}
		out.Clusters = append(out.Clusters, name)
		out.Skipped = strings.Join(uniqueSorted(append(strings.Split(out.Skipped, ","), skippedNodes...)), ",")
		out.Reasons = append(out.Reasons, result.SkippedReasons()...)
	}
	out.Clusters = uniqueSorted(out.Clusters)
	out.Reasons = uniqueSorted(out.Reasons)
	if len(out.Clusters) == 0 {
		return out, nil
	}
	records, err := ownership.LoadContext(ownershipDir, contextName)
	if err != nil {
		return out, fmt.Errorf("load ownership records to report partial-destroy markers: %w", err)
	}
	byName := map[string]ownership.ResourceRecord{}
	for _, rec := range records {
		if rec.Kind == string(ownership.KindStorageCluster) && !rec.IsReference() && rec.Owner == ownership.Owner && rec.EffectiveRole() == ownership.RoleOwner {
			byName[rec.Name] = rec
		}
	}
	for _, name := range out.Clusters {
		record, found := byName[name]
		wantSkipped := strings.Join(results[name].SkippedNodes(), ",")
		if !found || record.Attributes[StorageDestroyStatusAttr] != StorageDestroyStatusPartial || record.Attributes[StorageDestroySkippedNodesAttr] != wantSkipped {
			out.Unrecorded = append(out.Unrecorded, name)
			continue
		}
		out.Recorded = append(out.Recorded, name)
	}
	return out, nil
}

func PartiallyDestroyedStorageClusters(ownershipDir, contextName string) (map[string]string, error) {
	records, err := ownership.LoadContext(ownershipDir, contextName)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, rec := range records {
		if rec.Kind != string(ownership.KindStorageCluster) || rec.IsReference() || rec.Owner != ownership.Owner || rec.EffectiveRole() != ownership.RoleOwner || rec.APIVersion != "bootwright.io/ownership/v1alpha1" || rec.Cluster != rec.Name {
			continue
		}
		if rec.Attributes[StorageDestroyStatusAttr] == StorageDestroyStatusPartial {
			out[rec.Name] = rec.Attributes[StorageDestroySkippedNodesAttr]
		}
	}
	return out, nil
}

func uniqueSorted(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
