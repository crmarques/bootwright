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

	safefs "github.com/crmarques/bootwright/internal/host/safefs"
)

const SubstrateReleaseAPIVersion = "bootwright.io/substrate-release/v1"

type SubstrateReleaseRecord struct {
	APIVersion string    `json:"apiVersion"`
	Cluster    string    `json:"cluster"`
	ReleasedAt time.Time `json:"releasedAt"`
}

func substrateReleaseDir(runsDir string) string {
	return filepath.Join(runsDir, "substrate-release")
}

func substrateReleasePath(runsDir, cluster string) string {
	return filepath.Join(substrateReleaseDir(runsDir), convergeSafetyRecordFileName(cluster))
}

func MarkSubstrateReleased(runsDir, cluster string, now time.Time) error {
	if strings.TrimSpace(runsDir) == "" || strings.TrimSpace(cluster) == "" {
		return nil
	}
	record := SubstrateReleaseRecord{
		APIVersion: SubstrateReleaseAPIVersion,
		Cluster:    cluster,
		ReleasedAt: now.UTC(),
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("encode substrate release record: %w", err)
	}
	data = append(data, '\n')
	if err := safefs.WriteFileEnsuringDir(substrateReleasePath(runsDir, cluster), data, 0o600); err != nil {
		return fmt.Errorf("write substrate release record: %w", err)
	}
	return nil
}

func ClearSubstrateRelease(runsDir, cluster string) error {
	if strings.TrimSpace(runsDir) == "" || strings.TrimSpace(cluster) == "" {
		return nil
	}
	if err := os.Remove(substrateReleasePath(runsDir, cluster)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove substrate release record: %w", err)
	}
	return nil
}

func ReleasedSubstrateClusters(runsDir string) ([]string, error) {
	if strings.TrimSpace(runsDir) == "" {
		return nil, nil
	}
	dir := substrateReleaseDir(runsDir)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list substrate release records: %w", err)
	}
	seen := map[string]bool{}
	var out []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read substrate release record %s: %w", path, err)
		}
		var record SubstrateReleaseRecord
		if err := json.Unmarshal(data, &record); err != nil {
			return nil, fmt.Errorf("decode substrate release record %s: %w", path, err)
		}
		name := strings.TrimSpace(record.Cluster)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

func MachineSubstrateClusters(tasks []ApplyTask) []string {
	seen := map[string]bool{}
	var out []string
	for _, task := range tasks {
		if !machineSubstrateKinds[task.Entry.Kind] || task.Entry.Cluster == "" || seen[task.Entry.Cluster] {
			continue
		}
		seen[task.Entry.Cluster] = true
		out = append(out, task.Entry.Cluster)
	}
	sort.Strings(out)
	return out
}

func ConsumableSubstrateReleases(runsDir string, tasks []ApplyTask) ([]string, error) {
	released, err := ReleasedSubstrateClusters(runsDir)
	if err != nil || len(released) == 0 {
		return nil, err
	}
	planned := map[string]bool{}
	for _, name := range MachineSubstrateClusters(tasks) {
		planned[name] = true
	}
	var out []string
	for _, name := range released {
		if planned[name] {
			out = append(out, name)
		}
	}
	return out, nil
}

func SubstrateReleaseClearKind(kind string) bool {
	switch kind {
	case ApplyTaskKindManagedMachineOS, ApplyTaskKindNodeBoot, ApplyTaskKindInstallWait:
		return true
	default:
		return false
	}
}

func UnionClusterNames(lists ...[]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, list := range lists {
		for _, name := range list {
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func ApplyTaskConvergeSafetyRecorded(runsDir string, task ApplyTask) (bool, error) {
	if strings.TrimSpace(runsDir) == "" || strings.TrimSpace(task.Entry.ID) == "" {
		return false, nil
	}
	_, found, err := LoadConvergeSafetyRecord(runsDir, applyTaskSafetyResourceID(task))
	return found, err
}
