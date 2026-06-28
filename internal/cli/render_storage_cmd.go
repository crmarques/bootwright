package cli

import (
	"io"

	"github.com/spf13/cobra"

	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/clusteraccess"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/workspace"
)

func newRenderStorageCmd(stdout io.Writer, _ io.Writer) *cobra.Command {
	var storageScope string
	cmd := &cobra.Command{
		Use:   "storage",
		Short: "Render storage tool inputs",
		Args:  cobra.NoArgs,
		Example: `  # Render storage files for every StorageCluster
  bootwright render storage

  # Render only one StorageCluster
  bootwright render storage --clusters ceph-stretch`,
	}
	cf := addCommonFlags()
	cmd.Flags().StringVar(&storageScope, "clusters", "", "comma-separated StorageCluster names to render (default: all)")
	cmd.RunE = func(c *cobra.Command, _ []string) error {
		state, err := loadDesiredState(cf)
		if err != nil {
			return failErr(1, err)
		}
		ctx := cf.ctx
		names, err := clusteraccess.StorageClusterNamesForTarget(state, storageScope)
		if err != nil {
			return failErr(1, err)
		}
		state = clusteraccess.FilterStateToStorageClusters(state, names)
		state.ContainerClusters = nil
		result, err := workflow.RenderOnly(ctx.RenderedDir, workspace.ControllerClustersDir(ctx.Name), ctx.SecretsDir, state)
		if err != nil {
			return failErr(1, err)
		}
		outputpkg(stdout).Command("storage render")
		printStorageFiles(stdout, result)
		return nil
	}
	return cmd
}

func printStorageFiles(stdout io.Writer, result render.Result) {
	var paths []string
	for _, asset := range result.StorageAssets {
		paths = appendNonEmpty(paths, asset.BootstrapSpecPath, asset.CoreServicesSpecPath, asset.OperationsPath, asset.LateServicesSpecPath)
		for _, attachment := range asset.Attachments {
			paths = appendNonEmpty(paths, attachment.ExternalClusterDetailsPath, attachment.StorageClusterPath, attachment.StorageSystemPath)
		}
	}
	p := cliout.NewContinuation(stdout)
	p.Section("Rendered artifacts")
	p.Artifacts([]cliout.ArtifactGroup{{Name: "Storage", Paths: paths}})
}

func appendNonEmpty(paths []string, values ...string) []string {
	for _, value := range values {
		if value != "" {
			paths = append(paths, value)
		}
	}
	return paths
}
