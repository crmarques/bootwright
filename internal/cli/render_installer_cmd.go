package cli

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/render"
)

func newRenderClusterInstallFilesCmd(stdout io.Writer, _ io.Writer) *cobra.Command {
	var (
		clusterScope string
		sensitive    bool
		output       string
	)
	output = outputText
	cmd := &cobra.Command{
		Use:   "installer",
		Short: "Render OpenShift installer inputs",
		Args:  cobra.NoArgs,
		Example: `  # Render installer files (placeholders for secrets) for every cluster
  bootwright render installer

  # Render only specific clusters
  bootwright render installer --scope managed-01

  # Also write the effective installer files with secrets inlined (mode 0600)
  bootwright render installer --sensitive

  # Machine-readable output for CI
  bootwright render installer --output json`,
	}
	cf := addCommonFlags()
	cmd.Flags().StringVar(&clusterScope, "scope", "", "comma-separated ContainerCluster names to render")
	cmd.Flags().BoolVar(&sensitive, "sensitive", false, "also write effective installer inputs under /var/lib/bootwright/contexts/<context>/clusters/<cluster>/runtime/installer/ with secret material inlined for direct openshift-install consumption (mode 0600)")
	cmd.Flags().StringVar(&output, "output", output, "output format: text|json")
	cmd.RunE = func(c *cobra.Command, _ []string) error {
		if err := validateOutputFormat(output); err != nil {
			return failErr(2, err)
		}
		state, err := loadDesiredState(cf)
		if err != nil {
			return failErr(1, err)
		}
		ctx := cf.ctx
		clustersDir := controllerClustersDir(ctx.Name)
		warnSecretsDirPerms(ctx.SecretsDir, c.ErrOrStderr())
		names, err := clusterNamesForTarget(state, clusterScope)
		if err != nil {
			return failErr(1, err)
		}
		state = filterStateToClusters(state, names)
		result, err := workflow.RenderOnly(ctx.RenderedDir, clustersDir, ctx.SecretsDir, state)
		if err != nil {
			return failErr(1, err)
		}
		var resolved render.Result
		hasResolved := false
		if sensitive {
			resolved, err = workflow.ResolveInstaller(clustersDir, ctx.SecretsDir, state)
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

func runRenderToolInputs(c *cobra.Command, stdout io.Writer, cf *commonFlags, outputDir, clusterScope string) error {
	state, err := loadDesiredState(cf)
	if err != nil {
		return failErr(1, err)
	}
	ctx := cf.ctx
	warnSecretsDirPerms(ctx.SecretsDir, c.ErrOrStderr())
	if strings.TrimSpace(clusterScope) != "" {
		names, err := clusterNamesForTarget(state, clusterScope)
		if err != nil {
			return failErr(1, err)
		}
		state = filterStateToClusters(state, names)
	}
	outputDir, err = filepath.Abs(outputDir)
	if err != nil {
		return failErr(1, fmt.Errorf("resolve --output-dir %s: %w", outputDir, err))
	}
	result, err := workflow.RenderToolInputs(outputDir, ctx.SecretsDir, state)
	if err != nil {
		return failErr(1, err)
	}
	p := outputpkg(stdout)
	p.Command("tool input render")
	p.Section("Output")
	p.Fields([]cliout.Field{{Key: "output-dir", Value: outputDir}})
	printToolInputFiles(stdout, result)
	printToolInputCommands(stdout, result)
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
	var installerPaths []string
	for _, asset := range result.InstallerAssets {
		installerPaths = append(installerPaths, asset.InstallConfigPath, asset.AgentConfigPath, asset.InstallManifestsDir)
	}
	var storagePaths []string
	for _, asset := range result.StorageAssets {
		storagePaths = appendNonEmpty(storagePaths, asset.BootstrapSpecPath, asset.CoreServicesSpecPath, asset.OperationsPath, asset.LateServicesSpecPath)
		for _, attachment := range asset.Attachments {
			storagePaths = appendNonEmpty(storagePaths, attachment.ExternalClusterDetailsPath, attachment.StorageClusterPath, attachment.StorageSystemPath)
		}
	}
	groups := []cliout.ArtifactGroup{
		{Name: "Bootwright", Paths: []string{result.EffectiveStatePath, result.LockPath}},
		{Name: "Ansible", Paths: []string{result.InventoryPath, result.VarsPath}},
	}
	if len(installerPaths) > 0 {
		groups = append(groups, cliout.ArtifactGroup{Name: "openshift-install (secrets inlined)", Paths: installerPaths})
	}
	if len(storagePaths) > 0 {
		groups = append(groups, cliout.ArtifactGroup{Name: "storage", Paths: storagePaths})
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
	if len(result.StorageAssets) > 0 {
		p.Section("Storage commands")
	}
	for _, asset := range result.StorageAssets {
		if asset.BootstrapSpecPath == "" {
			continue
		}
		p.CommandLine("bootstrap ceph ["+asset.StorageClusterName+"]", []string{"cephadm", "bootstrap", "--apply-spec", asset.BootstrapSpecPath, "--mon-ip", "<derived-bootstrap-mon-ip>"})
		p.CommandLine("apply ceph core services ["+asset.StorageClusterName+"]", []string{"ceph", "orch", "apply", "-i", asset.CoreServicesSpecPath})
		p.CommandLine("apply ceph operations ["+asset.StorageClusterName+"]", []string{"bootwright", "apply", "storage-cluster", "--scope", asset.StorageClusterName, "--yes"})
		p.CommandLine("apply ceph late services ["+asset.StorageClusterName+"]", []string{"ceph", "orch", "apply", "-i", asset.LateServicesSpecPath})
	}
}
