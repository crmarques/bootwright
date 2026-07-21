package cli

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/spf13/cobra"

	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/clusteraccess"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/state/desired"
	"github.com/crmarques/bootwright/internal/workspace"
)

func newRenderClusterInstallFilesCmd(stdout io.Writer, _ io.Writer) *cobra.Command {
	var (
		clusterScope string
		sensitive    bool
		output       string
	)
	cmd := &cobra.Command{
		Use:   "installer",
		Short: "Render OpenShift installer inputs",
		Args:  cobra.NoArgs,
		Example: `  # Render installer files (placeholders for secrets) for every cluster
  bootwright render installer

  # Render only specific clusters
  bootwright render installer --clusters managed-01

  # Also write the effective installer files with secrets inlined (mode 0600)
  bootwright render installer --sensitive

  # Machine-readable output for CI
  bootwright render installer --output json`,
	}
	cf := addCommonFlags()
	cmd.Flags().StringVar(&clusterScope, "clusters", "", "comma-separated ContainerCluster names to render, default: all (the openshift-install agent inputs are container-cluster only)")
	registerClusterScopeCompletion(cmd, clusterKindContainer)
	cmd.Flags().BoolVar(&sensitive, "sensitive", false, "also write effective installer inputs under the context runtime/installer dir with secret material inlined for direct openshift-install consumption (mode 0600)")
	addOutputFlag(cmd, &output)
	cmd.RunE = func(c *cobra.Command, _ []string) error {
		if err := validateOutputFormat(output); err != nil {
			return failErr(2, err)
		}
		state, err := loadDesiredState(cf)
		if err != nil {
			return failErr(1, err)
		}
		ctx := cf.ctx
		clustersDir := workspace.ControllerClustersDir(ctx.Name)
		warnSecretsDirPerms(ctx.SecretsDir, c.ErrOrStderr())
		sel, err := clusteraccess.Resolve(state, "container-cluster", clusterScope)
		if err != nil {
			return failErr(1, err)
		}
		state = sel.RenderState
		result, err := workflow.RenderOnly(ctx.RenderedDir, clustersDir, ctx.SecretsDir, state)
		if err != nil {
			return failErr(1, err)
		}
		var resolved render.Result
		hasResolved := false
		if sensitive {
			resolved, err = workflow.ResolveInstallerForContext(ctx.Name, clustersDir, ctx.SecretsDir, state)
			if err != nil {
				return failErr(1, err)
			}
			hasResolved = true
		}
		if output == outputJSON {
			return writeRenderInstallerJSON(stdout, result, resolved, hasResolved)
		}
		outputpkg(stdout).Command("installer render")
		printInstallerFiles(stdout, result)
		if hasResolved {
			printEffectiveInstallerFiles(stdout, resolved)
		}
		return nil
	}
	return cmd
}

func runRenderToolInputs(c *cobra.Command, stdout io.Writer, cf *commonFlags, outputDir, clusterScope, output string) error {
	state, err := loadDesiredState(cf)
	if err != nil {
		return failErr(1, err)
	}
	ctx := cf.ctx
	warnSecretsDirPerms(ctx.SecretsDir, c.ErrOrStderr())
	sel, err := clusteraccess.Resolve(state, "all", clusterScope)
	if err != nil {
		return failErr(1, err)
	}
	state = sel.RenderState
	outputDir, err = filepath.Abs(outputDir)
	if err != nil {
		return failErr(1, fmt.Errorf("resolve --output-dir %s: %w", outputDir, err))
	}
	result, err := workflow.RenderToolInputsForContext(ctx.Name, outputDir, ctx.SecretsDir, state)
	if err != nil {
		return failErr(1, err)
	}
	if output == outputJSON {
		return writeRenderToolInputsJSON(stdout, "", outputDir, result)
	}
	p := outputpkg(stdout)
	p.Command("tool input render")
	p.Section("Output")
	p.Fields([]cliout.Field{{Key: "output-dir", Value: outputDir}})
	printToolInputFiles(stdout, result)
	printToolInputCommands(stdout, result)
	return nil
}

func runRenderPortable(stdout io.Writer, inputDir, outputDir, clusterScope, output string) error {
	state, err := desiredstate.LoadNormalizeValidate([]string{inputDir})
	if err != nil {
		return failErr(1, err)
	}
	sel, err := clusteraccess.Resolve(state, "all", clusterScope)
	if err != nil {
		return failErr(1, err)
	}
	state = sel.RenderState
	outputDir, err = filepath.Abs(outputDir)
	if err != nil {
		return failErr(1, fmt.Errorf("resolve --output-dir %s: %w", outputDir, err))
	}
	result, err := workflow.RenderToolInputsPortable(outputDir, state)
	if err != nil {
		return failErr(1, err)
	}
	if output == outputJSON {
		return writeRenderToolInputsJSON(stdout, inputDir, outputDir, result)
	}
	p := outputpkg(stdout)
	p.Command("portable render")
	p.Section("Output")
	p.Fields([]cliout.Field{
		{Key: "input-dir", Value: inputDir},
		{Key: "output-dir", Value: outputDir},
	})
	printToolInputArtifacts(stdout, result, "openshift-install ({{ secret }} placeholders)", "storage ({{ secret }} placeholders)")
	cont := cliout.NewContinuation(stdout)
	cont.Section("Secrets")
	cont.Status(cliout.StatusWarn, "placeholders", "substitute every {{ secret <name> }} token before use")
	return nil
}

type renderInstallerReport struct {
	Clusters []renderInstallerCluster `json:"clusters"`
}

type renderInstallerCluster struct {
	Name                         string `json:"name"`
	InstallConfigPath            string `json:"installConfigPath"`
	AgentConfigPath              string `json:"agentConfigPath"`
	InstallManifestsDir          string `json:"installManifestsDir,omitempty"`
	EffectiveInstallConfigPath   string `json:"effectiveInstallConfigPath,omitempty"`
	EffectiveAgentConfigPath     string `json:"effectiveAgentConfigPath,omitempty"`
	EffectiveInstallManifestsDir string `json:"effectiveInstallManifestsDir,omitempty"`
}

func writeRenderInstallerJSON(stdout io.Writer, result render.Result, resolved render.Result, hasResolved bool) error {
	report := renderInstallerReport{Clusters: make([]renderInstallerCluster, 0, len(result.InstallerAssets))}
	effectiveByName := map[string]render.InstallerAsset{}
	if hasResolved {
		for _, asset := range resolved.InstallerAssets {
			effectiveByName[asset.ClusterName] = asset
		}
	}
	for _, asset := range result.InstallerAssets {
		entry := renderInstallerCluster{
			Name:                asset.ClusterName,
			InstallConfigPath:   asset.InstallConfigPath,
			AgentConfigPath:     asset.AgentConfigPath,
			InstallManifestsDir: asset.InstallManifestsDir,
		}
		if effective, ok := effectiveByName[asset.ClusterName]; ok {
			entry.EffectiveInstallConfigPath = effective.EffectiveInstallConfigPath
			entry.EffectiveAgentConfigPath = effective.EffectiveAgentConfigPath
			entry.EffectiveInstallManifestsDir = effective.EffectiveInstallManifestsDir
		}
		report.Clusters = append(report.Clusters, entry)
	}
	return cliout.JSON(stdout, report)
}

type renderToolInputsReport struct {
	InputDir           string                   `json:"inputDir,omitempty"`
	OutputDir          string                   `json:"outputDir"`
	EffectiveStatePath string                   `json:"effectiveStatePath"`
	LockPath           string                   `json:"lockPath,omitempty"`
	InventoryPath      string                   `json:"inventoryPath"`
	VarsPath           string                   `json:"varsPath"`
	Installer          []renderInstallerCluster `json:"installer,omitempty"`
	Storage            []renderStorageCluster   `json:"storage,omitempty"`
}

func writeRenderToolInputsJSON(stdout io.Writer, inputDir, outputDir string, result render.Result) error {
	report := renderToolInputsReport{
		InputDir:           inputDir,
		OutputDir:          outputDir,
		EffectiveStatePath: result.EffectiveStatePath,
		LockPath:           result.LockPath,
		InventoryPath:      result.InventoryPath,
		VarsPath:           result.VarsPath,
		Installer:          make([]renderInstallerCluster, 0, len(result.InstallerAssets)),
		Storage:            renderStorageClusters(result.StorageAssets),
	}
	for _, asset := range result.InstallerAssets {
		report.Installer = append(report.Installer, renderInstallerCluster{
			Name:                asset.ClusterName,
			InstallConfigPath:   asset.InstallConfigPath,
			AgentConfigPath:     asset.AgentConfigPath,
			InstallManifestsDir: asset.InstallManifestsDir,
		})
	}
	return cliout.JSON(stdout, report)
}

func printInstallerFiles(stdout io.Writer, result render.Result) {
	var paths []string
	for _, asset := range result.InstallerAssets {
		paths = append(paths, asset.InstallConfigPath, asset.AgentConfigPath, asset.InstallManifestsDir)
	}
	p := cliout.NewContinuation(stdout)
	p.Section("Rendered artifacts")
	p.Artifacts([]cliout.ArtifactGroup{{Name: "Installer placeholders", Paths: paths}})
}

func printEffectiveInstallerFiles(stdout io.Writer, result render.Result) {
	var paths []string
	for _, asset := range result.InstallerAssets {
		paths = append(paths, asset.EffectiveInstallConfigPath, asset.EffectiveAgentConfigPath, asset.EffectiveInstallManifestsDir)
	}
	p := cliout.NewContinuation(stdout)
	p.Section("Effective installer files")
	p.Artifacts([]cliout.ArtifactGroup{{Name: "Secrets inlined", Paths: paths}})
}

func printToolInputFiles(stdout io.Writer, result render.Result) {
	printToolInputArtifacts(stdout, result, "openshift-install (secrets inlined)", "storage")
}

func printToolInputArtifacts(stdout io.Writer, result render.Result, installerLabel, storageLabel string) {
	var installerPaths []string
	for _, asset := range result.InstallerAssets {
		installerPaths = append(installerPaths, asset.InstallConfigPath, asset.AgentConfigPath, asset.InstallManifestsDir)
	}
	var storagePaths []string
	for _, asset := range result.StorageAssets {
		storagePaths = appendNonEmpty(storagePaths, asset.ApplyScriptPath, asset.ApplyLibPath, asset.BootstrapConfPath, asset.BootstrapSpecPath, asset.CoreServicesSpecPath, asset.OperationsPath, asset.LateServicesSpecPath)
	}
	groups := []cliout.ArtifactGroup{
		{Name: "Bootwright", Paths: []string{result.EffectiveStatePath, result.LockPath}},
		{Name: "Ansible", Paths: []string{result.InventoryPath, result.VarsPath}},
	}
	if len(installerPaths) > 0 {
		groups = append(groups, cliout.ArtifactGroup{Name: installerLabel, Paths: installerPaths})
	}
	if len(storagePaths) > 0 {
		groups = append(groups, cliout.ArtifactGroup{Name: storageLabel, Paths: storagePaths})
	}
	outputpkg(stdout).Artifacts(groups)
}

func printToolInputCommands(stdout io.Writer, result render.Result) {
	if len(result.InstallerAssets) == 0 && len(result.StorageAssets) == 0 {
		return
	}
	p := cliout.NewContinuation(stdout)
	if len(result.InstallerAssets) > 0 {
		p.Section("OpenShift install commands")
	}
	for _, asset := range result.InstallerAssets {
		p.CommandLine("create agent image ["+asset.ClusterName+"]", []string{"openshift-install", "agent", "create", "image", "--dir", asset.Dir})
		p.CommandLine("wait for install complete ["+asset.ClusterName+"]", []string{"openshift-install", "agent", "wait-for", "install-complete", "--dir", asset.Dir, "--log-level", "info"})
	}
	hasStorageScript := false
	for _, asset := range result.StorageAssets {
		if asset.ApplyScriptPath != "" {
			hasStorageScript = true
			break
		}
	}
	if hasStorageScript {
		p.Section("Ceph apply (native CLIs)")
	}
	for _, asset := range result.StorageAssets {
		if asset.ApplyScriptPath == "" {
			continue
		}
		p.CommandLine("apply ceph objects ["+asset.StorageClusterName+"]", []string{asset.ApplyScriptPath})
	}
}
