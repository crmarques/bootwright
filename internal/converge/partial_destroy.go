package converge

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/ownership"
)

const (
	StorageDestroyStatusAttr       = "destroyStatus"
	StorageDestroyStatusPartial    = "partial"
	StorageDestroySkippedNodesAttr = "destroySkippedNodes"
)

type storageDestroyResult struct {
	PartialClusters []string `json:"partialClusters"`
	SkippedNodes    []string `json:"skippedNodes"`
	SkippedHosts    []string `json:"skippedHosts"`
}

type PartialStorageDestroy struct {
	Recorded   []string
	Unrecorded []string
	Skipped    string
	Found      bool
}

func StorageDestroyResultPath(runLogPath string) string {
	return filepath.Join(filepath.Dir(runLogPath), "tasks", workflow.DestroyStorageClustersTaskID, "artifacts", "storage-destroy-result.json")
}

func RecordPartialStorageDestroy(ownershipDir, contextName, runLogPath string) (PartialStorageDestroy, error) {
	var out PartialStorageDestroy
	result, ok, err := readStorageDestroyResult(StorageDestroyResultPath(runLogPath))
	if err != nil {
		return out, err
	}
	out.Found = ok
	if !ok || len(result.PartialClusters) == 0 {
		return out, nil
	}
	out.Skipped = strings.Join(result.SkippedNodes, ",")
	records, err := ownership.LoadContext(ownershipDir, contextName)
	if err != nil {
		return out, fmt.Errorf("load ownership records to persist partial-destroy markers: %w", err)
	}
	byName := map[string]ownership.ResourceRecord{}
	for _, rec := range records {
		if rec.Kind == string(ownership.KindStorageCluster) {
			byName[rec.Name] = rec
		}
	}
	var firstErr error
	for _, name := range uniqueSorted(result.PartialClusters) {
		rec, found := byName[name]
		if !found {
			out.Unrecorded = append(out.Unrecorded, name)
			continue
		}
		if rec.Attributes == nil {
			rec.Attributes = map[string]string{}
		}
		rec.Attributes[StorageDestroyStatusAttr] = StorageDestroyStatusPartial
		if out.Skipped != "" {
			rec.Attributes[StorageDestroySkippedNodesAttr] = out.Skipped
		}
		rec.UpdatedAt = time.Time{}
		if err := ownership.SaveResource(ownershipDir, rec); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("persist partial-destroy marker for storage cluster %s: %w", name, err)
			}
			continue
		}
		out.Recorded = append(out.Recorded, name)
	}
	return out, firstErr
}

func PartiallyDestroyedStorageClusters(ownershipDir, contextName string) (map[string]string, error) {
	records, err := ownership.LoadContext(ownershipDir, contextName)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, rec := range records {
		if rec.Kind != string(ownership.KindStorageCluster) {
			continue
		}
		if rec.Attributes[StorageDestroyStatusAttr] == StorageDestroyStatusPartial {
			out[rec.Name] = rec.Attributes[StorageDestroySkippedNodesAttr]
		}
	}
	return out, nil
}

func readStorageDestroyResult(path string) (storageDestroyResult, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return storageDestroyResult{}, false, nil
	}
	if err != nil {
		return storageDestroyResult{}, false, fmt.Errorf("read storage destroy result: %w", err)
	}
	var result storageDestroyResult
	if err := json.Unmarshal(data, &result); err != nil {
		return storageDestroyResult{}, false, fmt.Errorf("decode storage destroy result: %w", err)
	}
	return result, true, nil
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
