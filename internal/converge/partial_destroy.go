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

// Ownership-record attributes that mark a storage cluster left only partially
// torn down because one or more of its nodes were powered off and skipped
// (--skip-unreachable): their OSD devices were not wiped and local Ceph state
// remains. The storage teardown play keeps such a cluster's ownership record
// (rather than removing it) so it is not treated as fully gone, and these
// attributes let `bootwright status` surface it and the operator complete the
// teardown later.
const (
	StorageDestroyStatusAttr       = "destroyStatus"
	StorageDestroyStatusPartial    = "partial"
	StorageDestroySkippedNodesAttr = "destroySkippedNodes"
)

// storageDestroyResult mirrors storage-destroy-result.json, written by the
// storage teardown play (delegate_to: localhost) when --skip-unreachable left
// nodes un-torn-down.
type storageDestroyResult struct {
	PartialClusters []string `json:"partialClusters"`
	SkippedNodes    []string `json:"skippedNodes"`
	SkippedHosts    []string `json:"skippedHosts"`
}

// StorageDestroyResultPath returns the controller path the storage teardown task
// writes its partial-teardown summary to, derived from the destroy graph run's
// log path (history/<runID>/bootwright.log). The teardown task writes the file to
// its per-task artifacts dir (history/<runID>/tasks/<task>/artifacts), which
// round-trips on the controller via delegate_to: localhost.
func StorageDestroyResultPath(runLogPath string) string {
	return filepath.Join(filepath.Dir(runLogPath), "tasks", workflow.DestroyStorageClustersTaskID, "artifacts", "storage-destroy-result.json")
}

// RecordPartialStorageDestroy reads the storage teardown's partial-teardown
// summary (if any) and stamps each affected storage cluster's ownership record as
// partially destroyed, so the cluster is not treated as fully gone. It returns
// the names of the partially-destroyed clusters so the caller can warn the
// operator. A missing summary means a clean teardown (returns nil, nil);
// best-effort stamping errors are returned alongside the names so the caller can
// still warn.
func RecordPartialStorageDestroy(ownershipDir, contextName, runLogPath string) ([]string, error) {
	result, ok, err := readStorageDestroyResult(StorageDestroyResultPath(runLogPath))
	if err != nil {
		return nil, err
	}
	if !ok || len(result.PartialClusters) == 0 {
		return nil, nil
	}
	records, err := ownership.LoadContext(ownershipDir, contextName)
	if err != nil {
		return uniqueSorted(result.PartialClusters), err
	}
	byName := map[string]ownership.ResourceRecord{}
	for _, rec := range records {
		if rec.Kind == string(ownership.KindStorageCluster) {
			byName[rec.Name] = rec
		}
	}
	skipped := strings.Join(result.SkippedNodes, ",")
	var firstErr error
	for _, name := range result.PartialClusters {
		rec, found := byName[name]
		if !found {
			// The teardown gates the record removal on a partial cluster, so it
			// should still exist; if it does not there is nothing to stamp.
			continue
		}
		if rec.Attributes == nil {
			rec.Attributes = map[string]string{}
		}
		rec.Attributes[StorageDestroyStatusAttr] = StorageDestroyStatusPartial
		if skipped != "" {
			rec.Attributes[StorageDestroySkippedNodesAttr] = skipped
		}
		rec.UpdatedAt = time.Time{} // re-stamp with the current time on save
		if err := ownership.SaveResource(ownershipDir, rec); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return uniqueSorted(result.PartialClusters), firstErr
}

// PartiallyDestroyedStorageClusters returns, by storage cluster name, the skipped
// node list recorded when --skip-unreachable left the cluster partially torn
// down, so status can surface it. A missing/empty ownership store yields an empty
// map.
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
