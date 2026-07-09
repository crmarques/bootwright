package workflow

import "sort"

func BareMetalFirstInstallClusters(objects []ObjectClassification, tasks []ApplyTask) []string {
	recorded := map[string]bool{}
	for _, o := range objects {
		if o.Kind == ObjectKindContainerCluster {
			recorded[o.Cluster] = o.Recorded()
		}
	}
	seen := map[string]bool{}
	var out []string
	for _, task := range tasks {
		if task.Entry.Kind != ApplyTaskKindNodeBoot {
			continue
		}
		cluster := task.Entry.Cluster
		if recorded[cluster] || seen[cluster] {
			continue
		}
		seen[cluster] = true
		out = append(out, cluster)
	}
	sort.Strings(out)
	return out
}
