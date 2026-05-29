package cli

import (
	"io"

	"github.com/spf13/cobra"

	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/render"
)

type clusterAccessReport struct {
	Context  string                 `json:"context"`
	Clusters []clusterAccessSummary `json:"clusters"`
}

type clusterListReport struct {
	Context  string             `json:"context"`
	Clusters []clusterListEntry `json:"clusters"`
}

type clusterListEntry struct {
	Name              string                `json:"name"`
	InstallMode       string                `json:"installMode"`
	InstallMethod     string                `json:"installMethod"`
	APIURL            string                `json:"apiURL"`
	ConsoleURL        string                `json:"consoleURL"`
	Kubeconfig        clusterAccessArtifact `json:"kubeconfig"`
	KubeadminPassword clusterAccessArtifact `json:"kubeadminPassword"`
	Ready             bool                  `json:"ready"`
}

func newClusterCmd(stdout io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cluster <command>",
		Short: "Inspect cluster access inventory",
	}
	cmd.AddCommand(
		newClusterListCmd(stdout),
		newClusterAccessCmd(stdout),
	)
	requireSubcommand(cmd)
	showSubcommandFlagsInHelp(cmd)
	return cmd
}

func newClusterListCmd(stdout io.Writer) *cobra.Command {
	outputFormat := outputText
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List clusters and local access artifact status",
		Args:  cobra.NoArgs,
	}
	cmd.Flags().StringVar(&outputFormat, "output", outputFormat, "output format: text|json")
	cf := addCommonFlags()
	cmd.RunE = func(_ *cobra.Command, _ []string) error {
		if err := validateOutputFormat(outputFormat); err != nil {
			return failErr(2, err)
		}
		state, err := loadDesiredState(cf)
		if err != nil {
			return failErr(1, err)
		}
		summaries := clusterAccessSummariesFromAssets(state, render.InstallerAssets(cf.ctx.RenderedDir, cf.ctx.RuntimeDir, state), cf.ctx.SecretsDir)
		if outputFormat == outputJSON {
			return cliout.JSON(stdout, clusterListReport{Context: cf.ctx.Name, Clusters: clusterListEntries(summaries)})
		}
		printClusterList(stdout, summaries)
		return nil
	}
	return cmd
}

func newClusterAccessCmd(stdout io.Writer) *cobra.Command {
	outputFormat := outputText
	clusterName := ""
	cmd := &cobra.Command{
		Use:   "access",
		Short: "Print local access details for installed clusters",
		Args:  cobra.NoArgs,
		Example: `  # Print access details for every cluster in the current context
  bootwright cluster access

  # Print access details for one cluster
  bootwright cluster access --cluster managed-01`,
	}
	cmd.Flags().StringVar(&clusterName, "cluster", "", "ContainerCluster name to inspect")
	cmd.Flags().StringVar(&outputFormat, "output", outputFormat, "output format: text|json")
	cf := addCommonFlags()
	cmd.RunE = func(_ *cobra.Command, _ []string) error {
		if err := validateOutputFormat(outputFormat); err != nil {
			return failErr(2, err)
		}
		state, err := loadDesiredState(cf)
		if err != nil {
			return failErr(1, err)
		}
		if clusterName != "" {
			if err := validateClusterNames(state, []string{clusterName}); err != nil {
				return failErr(2, err)
			}
		}
		summaries := clusterAccessSummariesFromAssets(state, render.InstallerAssets(cf.ctx.RenderedDir, cf.ctx.RuntimeDir, state), cf.ctx.SecretsDir)
		summaries = filterClusterAccessSummaries(summaries, clusterName)
		if outputFormat == outputJSON {
			return cliout.JSON(stdout, clusterAccessReport{Context: cf.ctx.Name, Clusters: summaries})
		}
		printClusterAccessSummaries(stdout, "cluster access", summaries)
		return nil
	}
	return cmd
}

func printClusterList(stdout io.Writer, summaries []clusterAccessSummary) {
	p := cliout.New(stdout)
	p.Command("cluster list")
	if len(summaries) == 0 {
		p.Summary(cliout.StatusSkip, "clusters", "none declared")
		return
	}
	p.Section("Clusters")
	for _, summary := range summaries {
		p.Status(clusterAccessStatus(summary), summary.Name, summary.InstallMode+" "+summary.InstallMethod)
		p.Fields([]cliout.Field{
			{Key: "API", Value: emptyAccessValue(summary.APIURL)},
			{Key: "Console", Value: emptyAccessValue(summary.ConsoleURL)},
			{Key: "Kubeconfig", Value: accessArtifactDetail(summary.Kubeconfig)},
			{Key: "Kubeadmin password", Value: accessArtifactDetail(summary.KubeadminPassword)},
		})
	}
}

func printClusterAccessSummaries(stdout io.Writer, command string, summaries []clusterAccessSummary) {
	p := cliout.New(stdout)
	p.Command(command)
	if len(summaries) == 0 {
		p.Summary(cliout.StatusSkip, "clusters", "none declared")
		return
	}
	for _, summary := range summaries {
		p.Section("Cluster " + summary.Name)
		p.Fields([]cliout.Field{
			{Key: "API", Value: emptyAccessValue(summary.APIURL)},
			{Key: "Console", Value: emptyAccessValue(summary.ConsoleURL)},
			{Key: "Kubeconfig", Value: summary.KubeconfigPath},
			{Key: "Kube context", Value: summary.KubeContextCommand},
			{Key: "Kubeadmin user", Value: summary.KubeadminUsername},
			{Key: "Password secret", Value: summary.KubeadminPasswordSecret},
			{Key: "Password file", Value: summary.KubeadminPasswordPath},
			{Key: "Show password", Value: summary.KubeadminPasswordCommand},
		})
		p.Status(accessArtifactStatus(summary.Kubeconfig), "kubeconfig", accessArtifactDetail(summary.Kubeconfig))
		p.Status(accessArtifactStatus(summary.KubeadminPassword), "kubeadmin password", accessArtifactDetail(summary.KubeadminPassword))
	}
}

func clusterListEntries(summaries []clusterAccessSummary) []clusterListEntry {
	entries := make([]clusterListEntry, 0, len(summaries))
	for _, summary := range summaries {
		entries = append(entries, clusterListEntry{
			Name:              summary.Name,
			InstallMode:       summary.InstallMode,
			InstallMethod:     summary.InstallMethod,
			APIURL:            summary.APIURL,
			ConsoleURL:        summary.ConsoleURL,
			Kubeconfig:        summary.Kubeconfig,
			KubeadminPassword: summary.KubeadminPassword,
			Ready:             summary.Ready,
		})
	}
	return entries
}

func filterClusterAccessSummaries(summaries []clusterAccessSummary, name string) []clusterAccessSummary {
	if name == "" {
		return summaries
	}
	for _, summary := range summaries {
		if summary.Name == name {
			return []clusterAccessSummary{summary}
		}
	}
	return nil
}

func clusterAccessStatus(summary clusterAccessSummary) cliout.Status {
	switch {
	case summary.Kubeconfig.Present && summary.KubeadminPassword.Present:
		return cliout.StatusOK
	case summary.Kubeconfig.Present || summary.KubeadminPassword.Present:
		return cliout.StatusWarn
	default:
		return cliout.StatusMissing
	}
}

func accessArtifactStatus(artifact clusterAccessArtifact) cliout.Status {
	if artifact.Present {
		return cliout.StatusOK
	}
	return cliout.StatusMissing
}

func accessArtifactDetail(artifact clusterAccessArtifact) string {
	if artifact.Present {
		return "OK " + artifact.Path
	}
	if artifact.Detail != "" {
		return "MISSING " + artifact.Detail
	}
	return "MISSING " + artifact.Path
}

func emptyAccessValue(value string) string {
	if value == "" {
		return "(unknown)"
	}
	return value
}
