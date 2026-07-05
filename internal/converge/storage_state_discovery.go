package converge

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/roles"
	"github.com/crmarques/bootwright/internal/storage/cephstate"
	"github.com/crmarques/bootwright/internal/workspace"
)

// RunCephStateDiscovery runs the read-only discover_storage_state playbook against
// the managed Ceph seeds and returns each cluster's live observation keyed by
// cluster name. It writes no convergence records and mutates nothing — it is the
// live-state read behind `bootwright diff` (live mode) and `--adopt`. A cluster
// whose seed does not answer is simply absent from the returned map (its offline
// classification stands). The result directory is scratch: the observations are
// read fully into memory and the directory removed before returning. It mirrors
// RunStorageHealthProbe, differing only in the breadth of reads it collects.
func RunCephStateDiscovery(cmdCtx context.Context, stdout, stderr io.Writer, ctx workspace.Context, clustersDir, executable, bundleDir string, state v1alpha1.State, streamAnsible bool, reporter workflow.Reporter) (map[string]cephstate.Discovery, error) {
	discoveryDir, err := os.MkdirTemp(ctx.RunsDir, "storage-discovery-")
	if err != nil {
		return nil, fmt.Errorf("create storage discovery result dir: %w", err)
	}
	defer os.RemoveAll(discoveryDir)
	runner := preflightRunner(stdout, stderr, streamAnsible)
	opts := runOptionsForContext(ctx, clustersDir, executable, state)
	opts.BundleDir = bundleDir
	opts.Playbook = roles.PlaybookDiscoverStorageState
	opts.Limit = storageHostsGroup
	opts.ArtifactsBaseName = "storage-discovery"
	opts.OutputLogPath = workflow.PreflightLogPath(ctx.RunsDir, "storage-discovery")
	opts.Label = "storage state discovery"
	opts.ExtraVarPairs = []string{"bootwright_storage_discovery_dir=" + discoveryDir}
	if _, err := workflow.Run(cmdCtx, opts, runner, reporter); err != nil {
		return nil, err
	}
	return cephstate.Load(discoveryDir)
}
