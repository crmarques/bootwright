package converge

import (
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/roles"
	"github.com/crmarques/bootwright/internal/storage/cephstate"
	"github.com/crmarques/bootwright/internal/workspace"
)

func RunCephStateDiscovery(cmdCtx context.Context, stdout, stderr io.Writer, ctx workspace.Context, clustersDir, executable, bundleDir string, state v1alpha1.State, clusterNames []string, verbose bool, streamAnsible bool, reporter workflow.Reporter) (map[string]cephstate.Discovery, error) {
	limit, err := cephStateDiscoveryLimit(state, clusterNames)
	if err != nil {
		return nil, err
	}
	discoveryDir, err := os.MkdirTemp(ctx.RunsDir, "storage-discovery-")
	if err != nil {
		return nil, fmt.Errorf("create storage discovery result dir: %w", err)
	}
	defer os.RemoveAll(discoveryDir)
	runner := preflightRunner(stdout, stderr, streamAnsible)
	opts := runOptionsForContext(ctx, clustersDir, executable, state)
	opts.BundleDir = bundleDir
	opts.Playbook = roles.PlaybookDiscoverStorageState
	opts.Limit = limit
	opts.ArtifactsBaseName = "storage-discovery"
	opts.OutputLogPath = workflow.PreflightLogPath(ctx.RunsDir, "storage-discovery")
	opts.Label = "storage state discovery"
	opts.ExtraVarPairs = append([]string{"bootwright_storage_discovery_dir=" + discoveryDir}, VerboseNoLogExtraVarPairs(verbose)...)
	if _, err := workflow.Run(cmdCtx, opts, runner, reporter); err != nil {
		discovered, _ := cephstate.Load(discoveryDir)
		return discovered, err
	}
	return cephstate.Load(discoveryDir)
}

func cephStateDiscoveryLimit(state v1alpha1.State, clusterNames []string) (string, error) {
	if len(clusterNames) == 0 {
		return "", fmt.Errorf("ceph state discovery requires at least one selected managed StorageCluster")
	}
	byName := make(map[string]v1alpha1.StorageCluster, len(state.StorageClusters))
	for _, cluster := range state.StorageClusters {
		byName[cluster.Metadata.Name] = cluster
	}
	hosts := make([]string, 0, len(clusterNames))
	seen := map[string]bool{}
	for _, candidate := range clusterNames {
		name := strings.TrimSpace(candidate)
		if name == "" {
			return "", fmt.Errorf("ceph state discovery selection contains an empty StorageCluster name")
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		cluster, found := byName[name]
		if !found {
			return "", fmt.Errorf("ceph state discovery selected unknown StorageCluster %q", name)
		}
		if !v1alpha1.StorageClusterManaged(cluster) || cluster.Spec.Ceph == nil {
			return "", fmt.Errorf("ceph state discovery selected StorageCluster %q, which is not a managed Ceph cluster", name)
		}
		if strings.TrimSpace(cluster.Spec.Ceph.Cephadm.Bootstrap.Node) == "" {
			return "", fmt.Errorf("ceph state discovery selected StorageCluster %q, which has no bootstrap node", name)
		}
		hosts = append(hosts, render.StorageSeedHostName(cluster))
	}
	slices.Sort(hosts)
	return ansibleLimit(hosts...), nil
}
