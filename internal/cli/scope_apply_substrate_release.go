package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func installedContainerClusterMachineReleaseRefusal(clustersDir string, records []workflow.SubstrateReleaseRecord) error {
	var clusters, details []string
	for _, release := range records {
		if len(release.Machines) == 0 {
			continue
		}
		record, found, err := workflow.LoadClusterInstallRecord(clustersDir, release.Cluster)
		if err != nil {
			return fmt.Errorf("apply refuses to rebuild machine-scoped released substrate for %s because its ContainerCluster install record could not be read: %w; Bootwright cannot prove that an individual-node recovery is safe — rebuild the cluster as a whole with `bootwright destroy --clusters %s --authorize data-loss --yes` then `bootwright apply --clusters %s --yes`, or recover the node with the platform's supported external node-recovery procedure", release.Cluster, err, release.Cluster, release.Cluster)
		}
		if !found || record.Status != workflow.ClusterInstallStatusInstalled {
			continue
		}
		machines := append([]string(nil), release.Machines...)
		sort.Strings(machines)
		clusters = append(clusters, release.Cluster)
		details = append(details, fmt.Sprintf("ContainerCluster/%s machine(s) %s", release.Cluster, strings.Join(machines, ", ")))
	}
	if len(clusters) == 0 {
		return nil
	}
	sort.Strings(clusters)
	return fmt.Errorf("apply refuses to rebuild machine-scoped released substrate for installed %s: its cluster-install work would be skipped as already complete, and Bootwright's initial-install workflow cannot recover individual cluster nodes; the release remains recorded — rebuild the selected cluster(s) as a whole with `bootwright destroy --clusters %s --authorize data-loss --yes` then `bootwright apply --clusters %s --yes`, or recover the node with the platform's supported external node-recovery procedure", strings.Join(details, "; "), strings.Join(clusters, ","), strings.Join(clusters, ","))
}
