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

	"github.com/crmarques/bootwright/api/v1alpha1"
	safefs "github.com/crmarques/bootwright/internal/host/safefs"
)

const SubstrateReleaseAPIVersion = "bootwright.io/substrate-release/v1"

type SubstrateReleaseRecord struct {
	APIVersion string    `json:"apiVersion"`
	Cluster    string    `json:"cluster"`
	Machines   []string  `json:"machines,omitempty"`
	ReleasedAt time.Time `json:"releasedAt"`
}

func substrateReleaseDir(runsDir string) string {
	return filepath.Join(runsDir, "substrate-release")
}

func substrateReleasePath(runsDir, cluster string) string {
	return filepath.Join(substrateReleaseDir(runsDir), convergeSafetyRecordFileName(cluster))
}

func writeSubstrateRelease(runsDir string, record SubstrateReleaseRecord) error {
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("encode substrate release record: %w", err)
	}
	data = append(data, '\n')
	if err := safefs.WriteFileEnsuringDir(substrateReleasePath(runsDir, record.Cluster), data, 0o600); err != nil {
		return fmt.Errorf("write substrate release record: %w", err)
	}
	return nil
}

func loadSubstrateRelease(runsDir, cluster string) (SubstrateReleaseRecord, bool, error) {
	data, err := os.ReadFile(substrateReleasePath(runsDir, cluster))
	if errors.Is(err, os.ErrNotExist) {
		return SubstrateReleaseRecord{}, false, nil
	}
	if err != nil {
		return SubstrateReleaseRecord{}, false, fmt.Errorf("read substrate release record for %s: %w", cluster, err)
	}
	var record SubstrateReleaseRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return SubstrateReleaseRecord{}, false, fmt.Errorf("decode substrate release record for %s: %w", cluster, err)
	}
	return record, true, nil
}

func MarkSubstrateReleased(runsDir, cluster string, now time.Time) error {
	if strings.TrimSpace(runsDir) == "" || strings.TrimSpace(cluster) == "" {
		return nil
	}
	return writeSubstrateRelease(runsDir, SubstrateReleaseRecord{
		APIVersion: SubstrateReleaseAPIVersion,
		Cluster:    cluster,
		ReleasedAt: now.UTC(),
	})
}

func MarkSubstrateMachinesReleased(runsDir, cluster string, machines []string, now time.Time) error {
	if strings.TrimSpace(runsDir) == "" || strings.TrimSpace(cluster) == "" || len(machines) == 0 {
		return nil
	}
	existing, found, err := loadSubstrateRelease(runsDir, cluster)
	if err != nil {
		return err
	}
	if found && len(existing.Machines) == 0 {
		return nil
	}
	return writeSubstrateRelease(runsDir, SubstrateReleaseRecord{
		APIVersion: SubstrateReleaseAPIVersion,
		Cluster:    cluster,
		Machines:   UnionClusterNames(existing.Machines, machines),
		ReleasedAt: now.UTC(),
	})
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

func ConsumeSubstrateRelease(runsDir, cluster string, coveredMachines, clusterMachines []string) error {
	if strings.TrimSpace(runsDir) == "" || strings.TrimSpace(cluster) == "" {
		return nil
	}
	if coveredMachines == nil {
		return ClearSubstrateRelease(runsDir, cluster)
	}
	record, found, err := loadSubstrateRelease(runsDir, cluster)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	released := record.Machines
	if len(released) == 0 {
		released = clusterMachines
	}
	covered := make(map[string]bool, len(coveredMachines))
	for _, name := range coveredMachines {
		covered[name] = true
	}
	var remaining []string
	for _, name := range released {
		if !covered[name] {
			remaining = append(remaining, name)
		}
	}
	if len(remaining) == 0 {
		return ClearSubstrateRelease(runsDir, cluster)
	}
	sort.Strings(remaining)
	record.Machines = remaining
	return writeSubstrateRelease(runsDir, record)
}

func loadAllSubstrateReleases(runsDir string) ([]SubstrateReleaseRecord, error) {
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
	var out []SubstrateReleaseRecord
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
		out = append(out, record)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Cluster < out[j].Cluster })
	return out, nil
}

func ReleasedSubstrateClusters(runsDir string) ([]string, error) {
	records, err := loadAllSubstrateReleases(runsDir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, record := range records {
		out = append(out, record.Cluster)
	}
	return out, nil
}

func MachineSubstrateClusters(tasks []ApplyTask) []string {
	seen := map[string]bool{}
	var out []string
	for _, task := range tasks {
		if !isMachineSubstrateKind(task.Entry.Kind) || task.Entry.Cluster == "" || seen[task.Entry.Cluster] {
			continue
		}
		seen[task.Entry.Cluster] = true
		out = append(out, task.Entry.Cluster)
	}
	sort.Strings(out)
	return out
}

func ConsumableSubstrateReleases(runsDir string, tasks []ApplyTask) ([]SubstrateReleaseRecord, error) {
	released, err := loadAllSubstrateReleases(runsDir)
	if err != nil || len(released) == 0 {
		return nil, err
	}
	planned := map[string]bool{}
	for _, name := range MachineSubstrateClusters(tasks) {
		planned[name] = true
	}
	var out []SubstrateReleaseRecord
	for _, record := range released {
		if planned[record.Cluster] {
			out = append(out, record)
		}
	}
	return out, nil
}

func SubstrateReleaseClusterNames(records []SubstrateReleaseRecord) []string {
	var out []string
	for _, record := range records {
		out = append(out, record.Cluster)
	}
	return out
}

func SubstrateReleaseMachinePairs(records []SubstrateReleaseRecord) []string {
	var out []string
	for _, record := range records {
		for _, machine := range record.Machines {
			out = append(out, record.Cluster+"/"+machine)
		}
	}
	sort.Strings(out)
	return out
}

func ClusterSubstrateMachineNames(state v1alpha1.State, cluster string) []string {
	seen := map[string]bool{}
	var out []string
	for _, sc := range state.StorageClusters {
		if sc.Metadata.Name != cluster || sc.Spec.Ceph == nil {
			continue
		}
		for _, node := range sc.Spec.Ceph.Topology.Nodes {
			if node.MachineRef.Name != "" && !seen[node.MachineRef.Name] {
				seen[node.MachineRef.Name] = true
				out = append(out, node.MachineRef.Name)
			}
		}
	}
	for _, cc := range state.ContainerClusters {
		if cc.Metadata.Name != cluster {
			continue
		}
		for _, node := range cc.Spec.Nodes {
			if node.MachineRef.Name != "" && !seen[node.MachineRef.Name] {
				seen[node.MachineRef.Name] = true
				out = append(out, node.MachineRef.Name)
			}
		}
	}
	sort.Strings(out)
	return out
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
