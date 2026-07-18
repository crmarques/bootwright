package workflow

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/ownership"
	stategraph "github.com/crmarques/bootwright/internal/state/graph"
)

func StandingDestroyScopeConflicts(runsDir, clustersDir string, state v1alpha1.State, records []ownership.ResourceRecord, tasks []ApplyTask, conflicts []stategraph.DestroyScopeConflict) []stategraph.DestroyScopeConflict {
	if len(conflicts) == 0 {
		return nil
	}
	standing, ok := standingClusterEvidence(runsDir, clustersDir, state, records, tasks)
	if !ok {
		return conflicts
	}
	var out []stategraph.DestroyScopeConflict
	for _, conflict := range conflicts {
		var kept []string
		for _, name := range conflict.UnscopedClusters {
			if standing[name] {
				kept = append(kept, name)
			}
		}
		if len(kept) == 0 {
			continue
		}
		conflict.UnscopedClusters = kept
		out = append(out, conflict)
	}
	return out
}

func standingClusterEvidence(runsDir, clustersDir string, state v1alpha1.State, records []ownership.ResourceRecord, tasks []ApplyTask) (map[string]bool, bool) {
	standing := map[string]bool{}
	for _, record := range records {
		if record.Cluster != "" {
			standing[record.Cluster] = true
		}
	}
	for _, cluster := range state.ContainerClusters {
		record, found, err := LoadClusterInstallRecord(clustersDir, cluster.Metadata.Name)
		if err != nil {
			return nil, false
		}
		if found && record.Status != ClusterInstallStatusDestroyed {
			standing[cluster.Metadata.Name] = true
		}
	}
	for _, task := range tasks {
		if task.Entry.Cluster == "" || standing[task.Entry.Cluster] {
			continue
		}
		recorded, err := ApplyTaskConvergeSafetyRecorded(runsDir, task)
		if err != nil {
			return nil, false
		}
		if recorded {
			standing[task.Entry.Cluster] = true
		}
	}
	return standing, true
}
