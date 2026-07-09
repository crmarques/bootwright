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
	var (
		storageScope string
		output       string
	)
	cmd := &cobra.Command{
		Use:   "storage",
		Short: "Render storage tool inputs",
		Args:  cobra.NoArgs,
		Example: `  # Render storage files for every StorageCluster
  bootwright render storage

  # Render only one StorageCluster
  bootwright render storage --clusters ceph-stretch

  # Machine-readable output for CI
  bootwright render storage --output json`,
	}
	cf := addCommonFlags()
	cmd.Flags().StringVar(&storageScope, "clusters", "", "comma-separated StorageCluster names to render (default: all)")
	addOutputFlag(cmd, &output)
	cmd.RunE = func(_ *cobra.Command, _ []string) error {
		if err := validateOutputFormat(output); err != nil {
			return failErr(2, err)
		}
		state, err := loadDesiredState(cf)
		if err != nil {
			return failErr(1, err)
		}
		ctx := cf.ctx
		state, err = clusteraccess.StorageRenderState(state, storageScope)
		if err != nil {
			return failErr(1, err)
		}
		result, err := workflow.RenderOnly(ctx.RenderedDir, workspace.ControllerClustersDir(ctx.Name), ctx.SecretsDir, state)
		if err != nil {
			return failErr(1, err)
		}
		if output == outputJSON {
			return cliout.JSON(stdout, renderStorageReport{Clusters: renderStorageClusters(result.StorageAssets)})
		}
		outputpkg(stdout).Command("storage render")
		printStorageFiles(stdout, result)
		return nil
	}
	return cmd
}

type renderStorageReport struct {
	Clusters []renderStorageCluster `json:"clusters"`
}

type renderStorageCluster struct {
	Name                 string   `json:"name"`
	ApplyScriptPath      string   `json:"applyScriptPath,omitempty"`
	ApplyLibPath         string   `json:"applyLibPath,omitempty"`
	BootstrapSpecPath    string   `json:"bootstrapSpecPath,omitempty"`
	CoreServicesSpecPath string   `json:"coreServicesSpecPath,omitempty"`
	OperationsPath       string   `json:"operationsPath,omitempty"`
	LateServicesSpecPath string   `json:"lateServicesSpecPath,omitempty"`
	Attachments          []string `json:"attachments,omitempty"`
}

// renderStorageClusters projects rendered StorageAssets into the JSON report
// shape shared by `render storage --output json` and the top-level tool-input
// render, so the two never spell the same paths differently.
func renderStorageClusters(assets []render.StorageAsset) []renderStorageCluster {
	clusters := make([]renderStorageCluster, 0, len(assets))
	for _, asset := range assets {
		entry := renderStorageCluster{
			Name:                 asset.StorageClusterName,
			ApplyScriptPath:      asset.ApplyScriptPath,
			ApplyLibPath:         asset.ApplyLibPath,
			BootstrapSpecPath:    asset.BootstrapSpecPath,
			CoreServicesSpecPath: asset.CoreServicesSpecPath,
			OperationsPath:       asset.OperationsPath,
			LateServicesSpecPath: asset.LateServicesSpecPath,
		}
		clusters = append(clusters, entry)
	}
	return clusters
}

func printStorageFiles(stdout io.Writer, result render.Result) {
	var paths []string
	for _, asset := range result.StorageAssets {
		paths = appendNonEmpty(paths, asset.BootstrapSpecPath, asset.CoreServicesSpecPath, asset.OperationsPath, asset.LateServicesSpecPath)
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
