package workflow

func BootProvenContainerClusters(clustersDir string, tasks []ApplyTask) []string {
	var out []string
	for _, name := range installTaskClusterNames(tasks) {
		record, found, err := LoadClusterInstallRecord(clustersDir, name)
		if err != nil || !found || record.Status == ClusterInstallStatusDestroyed || validateClusterInstallRecordState(clustersDir, name, record) != nil {
			continue
		}
		if record.Status == ClusterInstallStatusInstalled || clusterInstallPhaseMayHaveBooted(record.Phase) {
			out = append(out, name)
		}
	}
	return out
}
