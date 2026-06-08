package cli

import (
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/api/v1alpha1"
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
	Kind              string                `json:"kind"`
	Name              string                `json:"name"`
	Type              string                `json:"type,omitempty"`
	Management        string                `json:"management,omitempty"`
	InstallMode       string                `json:"installMode,omitempty"`
	InstallMethod     string                `json:"installMethod,omitempty"`
	APIURL            string                `json:"apiURL,omitempty"`
	ConsoleURL        string                `json:"consoleURL,omitempty"`
	Kubeconfig        clusterAccessArtifact `json:"kubeconfig,omitempty"`
	KubeadminPassword clusterAccessArtifact `json:"kubeadminPassword,omitempty"`
	Ready             bool                  `json:"ready,omitempty"`
}

func newClusterCmd(stdout io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cluster <command>",
		Short: "Inspect container and storage cluster inventory",
	}
	cmd.AddCommand(
		newClusterListCmd(stdout),
		newClusterAccessInfoCmd(stdout),
		newClusterKubeconfigCmd(stdout),
	)
	requireSubcommand(cmd)
	return cmd
}

func newClusterKubeconfigCmd(stdout io.Writer) *cobra.Command {
	clusterName := ""
	cmd := &cobra.Command{
		Use:   "kubeconfig --cluster <name>",
		Short: "Print the admin kubeconfig for an installed cluster",
		Long: `Print the generated admin kubeconfig for an installed container cluster to
stdout, so you can save it to a file you own instead of copying the root-owned
source by hand:

    bootwright cluster kubeconfig --cluster managed-01 > ~/.kube/managed-01
    oc --kubeconfig ~/.kube/managed-01 get nodes

The kubeconfig is admin credential material; redirect it to a private path and
do not commit it.`,
		Args: cobra.NoArgs,
		Example: `  # Save one cluster's admin kubeconfig to a private file
  bootwright cluster kubeconfig --cluster managed-01 > ~/.kube/managed-01`,
	}
	cmd.Flags().StringVar(&clusterName, "cluster", "", "ContainerCluster name")
	_ = cmd.MarkFlagRequired("cluster")
	cf := addCommonFlags()
	cmd.RunE = func(_ *cobra.Command, _ []string) error {
		state, err := loadDesiredState(cf)
		if err != nil {
			return failErr(1, err)
		}
		if err := validateClusterNames(state, []string{clusterName}); err != nil {
			return failErr(2, err)
		}
		clustersDir := controllerClustersDir(cf.ctx.Name)
		summaries := filterClusterAccessSummaries(
			clusterAccessSummariesFromAssets(state, render.InstallerAssets(clustersDir, state)),
			clusterName,
		)
		if len(summaries) == 0 {
			return failf(1, "%q is not an installed container cluster", clusterName)
		}
		summary := summaries[0]
		if !summary.Kubeconfig.Present {
			return failf(1, "kubeconfig for %q not found at %s; install the cluster first", clusterName, summary.KubeconfigPath)
		}
		data, err := os.ReadFile(summary.KubeconfigPath)
		if err != nil {
			return failErr(1, fmt.Errorf("read kubeconfig for %s: %w", clusterName, err))
		}
		if _, err := stdout.Write(data); err != nil {
			return failErr(1, fmt.Errorf("write kubeconfig for %s: %w", clusterName, err))
		}
		return nil
	}
	return cmd
}

func newContainerClusterCmd(stdout io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:    "container-cluster <command>",
		Short:  "Inspect container cluster access inventory",
		Hidden: true,
	}
	cmd.AddCommand(
		newClusterAccessCmd(stdout, clusterAccessCommandSpec{
			use:   "access",
			label: "container-cluster access",
			example: `  # Print access details for every container cluster in the current context
  bootwright container-cluster access

  # Print access details for one container cluster
  bootwright container-cluster access --cluster managed-01`,
		}),
	)
	requireSubcommand(cmd)
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
		clustersDir := controllerClustersDir(cf.ctx.Name)
		summaries := clusterAccessSummariesFromAssets(state, render.InstallerAssets(clustersDir, state))
		storage := storageClusterListEntries(state)
		if outputFormat == outputJSON {
			return cliout.JSON(stdout, clusterListReport{Context: cf.ctx.Name, Clusters: append(clusterListEntries(summaries), storage...)})
		}
		printClusterList(stdout, summaries, storage)
		return nil
	}
	return cmd
}

func newClusterAccessInfoCmd(stdout io.Writer) *cobra.Command {
	return newClusterAccessCmd(stdout, clusterAccessCommandSpec{
		use:   "access-info",
		label: "cluster access-info",
		example: `  # Print access details for every managed cluster in the current context
  bootwright cluster access-info

  # Print access details for one managed cluster
  bootwright cluster access-info --cluster managed-01`,
	})
}

type clusterAccessCommandSpec struct {
	use     string
	label   string
	example string
}

func newClusterAccessCmd(stdout io.Writer, spec clusterAccessCommandSpec) *cobra.Command {
	outputFormat := outputText
	clusterName := ""
	cmd := &cobra.Command{
		Use:     spec.use,
		Short:   "Print local access details for installed clusters",
		Args:    cobra.NoArgs,
		Example: spec.example,
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
		clustersDir := controllerClustersDir(cf.ctx.Name)
		summaries := clusterAccessSummariesFromAssets(state, render.InstallerAssets(clustersDir, state))
		summaries = filterClusterAccessSummaries(summaries, clusterName)
		if outputFormat == outputJSON {
			return cliout.JSON(stdout, clusterAccessReport{Context: cf.ctx.Name, Clusters: summaries})
		}
		printClusterAccessSummaries(stdout, spec.label, summaries)
		return nil
	}
	return cmd
}

func printClusterList(stdout io.Writer, summaries []clusterAccessSummary, storage []clusterListEntry) {
	p := cliout.New(stdout)
	p.Command("cluster list")
	if len(summaries) == 0 && len(storage) == 0 {
		p.Summary(cliout.StatusSkip, "clusters", "none declared")
		return
	}
	if len(summaries) > 0 {
		p.Section("Container clusters")
	}
	for _, summary := range summaries {
		p.Status(clusterAccessStatus(summary), summary.Name, summary.InstallMode+" "+summary.InstallMethod)
		p.Fields([]cliout.Field{
			{Key: "API", Value: emptyAccessValue(summary.APIURL)},
			{Key: "Console", Value: emptyAccessValue(summary.ConsoleURL)},
			{Key: "Kubeconfig", Value: accessArtifactDetail(summary.Kubeconfig)},
			{Key: "Kubeadmin password", Value: accessArtifactDetail(summary.KubeadminPassword)},
		})
	}
	if len(storage) > 0 {
		p.Section("Storage clusters")
	}
	for _, cluster := range storage {
		detail := cluster.Type
		if cluster.Management != "" {
			detail += " " + cluster.Management
		}
		p.Status(cliout.StatusOK, cluster.Name, detail)
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
			Kind:              "container",
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

func storageClusterListEntries(state v1alpha1.State) []clusterListEntry {
	entries := make([]clusterListEntry, 0, len(state.StorageClusters))
	for _, cluster := range state.StorageClusters {
		management := cluster.Spec.Management
		if management == "" {
			management = v1alpha1.StorageClusterManagementManaged
		}
		entries = append(entries, clusterListEntry{
			Kind:       "storage",
			Name:       cluster.Metadata.Name,
			Type:       cluster.Spec.Type,
			Management: management,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
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
