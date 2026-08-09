package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func installedContainerClusterMachineReleaseRefusal(clustersDir string, records []workflow.SubstrateReleaseRecord, invocation *resolvedInvocation) error {
	var clusters, details []string
	for _, release := range records {
		if len(release.Machines) == 0 {
			continue
		}
		record, found, err := workflow.LoadClusterInstallRecord(clustersDir, release.Cluster)
		if err != nil {
			return installedContainerClusterReleaseError(fmt.Sprintf("apply refuses to rebuild machine-scoped released substrate for %s because its ContainerCluster install record could not be read: %v; Bootwright cannot prove that an individual-node recovery is safe", release.Cluster, err), []string{release.Cluster}, invocation)
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
	return installedContainerClusterReleaseError(fmt.Sprintf("apply refuses to rebuild machine-scoped released substrate for installed %s: its cluster-install work would be skipped as already complete, and Bootwright's initial-install workflow cannot recover individual cluster nodes; the release remains recorded", strings.Join(details, "; ")), clusters, invocation)
}

func installedContainerClusterReleaseError(evidence string, clusters []string, invocation *resolvedInvocation) error {
	if invocation == nil {
		return fmt.Errorf("%s — rebuild the selected cluster(s) as a whole with a context-preserving destroy followed by an apply explicitly authorizing data loss, or recover the node with the platform's supported external node-recovery procedure", evidence)
	}
	destroyCommand, err := invocation.destroyClustersRetry(clusters)
	if err != nil {
		return err
	}
	applyCommand, err := invocation.applyClustersRetry(clusters, authorizeDataLoss)
	if err != nil {
		return err
	}
	return fmt.Errorf("%s — rebuild the selected cluster(s) as a whole with `%s` then `%s`, or recover the node with the platform's supported external node-recovery procedure", evidence, destroyCommand.String(), applyCommand.String())
}
