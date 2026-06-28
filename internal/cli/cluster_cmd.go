package cli

import (
	"fmt"
	"io"
	"sort"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/api/v1alpha1"
	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/clusteraccess"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/workspace"
)

type clusterAccessReport struct {
	Context  string                         `json:"context"`
	Clusters []clusteraccess.ClusterSummary `json:"clusters"`
	Storage  []clusteraccess.StorageSummary `json:"storage,omitempty"`
}

type clusterListReport struct {
	Context  string             `json:"context"`
	Clusters []clusterListEntry `json:"clusters"`
}

type clusterListEntry struct {
	Kind              string                 `json:"kind"`
	Name              string                 `json:"name"`
	Type              string                 `json:"type,omitempty"`
	Management        string                 `json:"management,omitempty"`
	InstallMode       string                 `json:"installMode,omitempty"`
	InstallMethod     string                 `json:"installMethod,omitempty"`
	Substrate         string                 `json:"substrate,omitempty"`
	HostCluster       string                 `json:"hostCluster,omitempty"`
	APIURL            string                 `json:"apiURL,omitempty"`
	ConsoleURL        string                 `json:"consoleURL,omitempty"`
	Kubeconfig        clusteraccess.Artifact `json:"kubeconfig,omitempty"`
	KubeadminPassword clusteraccess.Artifact `json:"kubeadminPassword,omitempty"`
	DashboardPassword clusteraccess.Artifact `json:"dashboardPassword,omitempty"`
	Ready             bool                   `json:"ready,omitempty"`
}

func newClusterCmd(stdout io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cluster <command>",
		Short: "Inspect container and storage cluster inventory",
	}
	cmd.AddCommand(
		newClusterListCmd(stdout),
		newClusterAccessCommand(stdout),
		newClusterKubeconfigCmd(stdout),
	)
	requireSubcommand(cmd)
	return cmd
}

func newClusterKubeconfigCmd(stdout io.Writer) *cobra.Command {
	clusterName := ""
	cmd := &cobra.Command{
		Use:   "kubeconfig --name <cluster>",
		Short: "Print the admin kubeconfig for an installed cluster",
		Long: `Print the generated admin kubeconfig for an installed container cluster to
stdout, so you can save it to a file you own instead of copying the root-owned
source by hand:

    bootwright cluster kubeconfig --name managed-01 > ~/.kube/managed-01
    oc --kubeconfig ~/.kube/managed-01 get nodes

The kubeconfig is admin credential material; redirect it to a private path and
do not commit it.`,
		Args: cobra.NoArgs,
		Example: `  # Save one cluster's admin kubeconfig to a private file
  bootwright cluster kubeconfig --name managed-01 > ~/.kube/managed-01`,
	}
	cmd.Flags().StringVar(&clusterName, "name", "", "ContainerCluster name (required)")
	_ = cmd.MarkFlagRequired("name")
	cf := addCommonFlags()
	cmd.RunE = func(_ *cobra.Command, _ []string) error {
		state, err := loadDesiredState(cf)
		if err != nil {
			return failErr(1, err)
		}
		if err := clusteraccess.ValidateClusterNames(state, []string{clusterName}); err != nil {
			return failErr(2, err)
		}
		clustersDir := workspace.ControllerClustersDir(cf.ctx.Name)
		data, err := clusteraccess.Kubeconfig(state, clustersDir, clusterName)
		if err != nil {
			return failErr(1, err)
		}
		if _, err := stdout.Write(data); err != nil {
			return failErr(1, fmt.Errorf("write kubeconfig for %s: %w", clusterName, err))
		}
		return nil
	}
	return cmd
}

func newClusterListCmd(stdout io.Writer) *cobra.Command {
	var outputFormat string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List clusters and local access artifact status",
		Args:  cobra.NoArgs,
	}
	addOutputFlag(cmd, &outputFormat)
	cf := addCommonFlags()
	cmd.RunE = func(_ *cobra.Command, _ []string) error {
		if err := validateOutputFormat(outputFormat); err != nil {
			return failErr(2, err)
		}
		state, err := loadDesiredState(cf)
		if err != nil {
			return failErr(1, err)
		}
		clustersDir := workspace.ControllerClustersDir(cf.ctx.Name)
		summaries := clusteraccess.ClusterSummariesFromAssets(state, render.InstallerAssets(clustersDir, state))
		storage := storageClusterListEntries(state, clustersDir)
		if outputFormat == outputJSON {
			return cliout.JSON(stdout, clusterListReport{Context: cf.ctx.Name, Clusters: append(clusterListEntries(summaries), storage...)})
		}
		printClusterList(stdout, summaries, storage)
		return nil
	}
	return cmd
}

func newClusterAccessCommand(stdout io.Writer) *cobra.Command {
	return newClusterAccessCmd(stdout, clusterAccessCommandSpec{
		use:            "access",
		label:          "cluster access",
		includeStorage: true,
		example: `  # Print access details for every managed cluster in the current context
  bootwright cluster access

  # Print access details for one container or storage cluster
  bootwright cluster access --name managed-01
  bootwright cluster access --name ceph-libvirt`,
	})
}

type clusterAccessCommandSpec struct {
	use            string
	label          string
	example        string
	includeStorage bool
}

func newClusterAccessCmd(stdout io.Writer, spec clusterAccessCommandSpec) *cobra.Command {
	var outputFormat string
	clusterName := ""
	cmd := &cobra.Command{
		Use:     spec.use,
		Short:   "Print local access details for installed clusters",
		Args:    cobra.NoArgs,
		Example: spec.example,
	}
	clusterFlagUsage := "ContainerCluster name to inspect (default: all)"
	if spec.includeStorage {
		clusterFlagUsage = "ContainerCluster or StorageCluster name to inspect (default: all)"
	}
	cmd.Flags().StringVar(&clusterName, "name", "", clusterFlagUsage)
	addOutputFlag(cmd, &outputFormat)
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
			if err := clusteraccess.ValidateAccessClusterName(state, clusterName, spec.includeStorage); err != nil {
				return failErr(2, err)
			}
		}
		clustersDir := workspace.ControllerClustersDir(cf.ctx.Name)
		summaries := clusteraccess.ClusterSummariesFromAssets(state, render.InstallerAssets(clustersDir, state))
		summaries = clusteraccess.FilterClusterSummaries(summaries, clusterName)
		var storage []clusteraccess.StorageSummary
		if spec.includeStorage {
			storage = clusteraccess.FilterStorageSummaries(clusteraccess.StorageSummaries(state, clustersDir), clusterName)
		}
		if outputFormat == outputJSON {
			return cliout.JSON(stdout, clusterAccessReport{Context: cf.ctx.Name, Clusters: summaries, Storage: storage})
		}
		printClusterAccessSummaries(stdout, spec.label, summaries, storage)
		return nil
	}
	return cmd
}

func printClusterList(stdout io.Writer, summaries []clusteraccess.ClusterSummary, storage []clusterListEntry) {
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
		detail := summary.InstallMode + " " + summary.InstallMethod
		if sd := substrateDescriptor(summary.Substrate, summary.HostCluster); sd != "" {
			detail += " · " + sd
		}
		p.Status(clusterAccessStatus(summary), summary.Name, detail)
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
		if cluster.DashboardPassword.Path != "" {
			p.Fields([]cliout.Field{
				{Key: "Dashboard password", Value: accessArtifactDetail(cluster.DashboardPassword)},
			})
		}
	}
}

func printClusterAccessSummaries(stdout io.Writer, command string, summaries []clusteraccess.ClusterSummary, storage []clusteraccess.StorageSummary) {
	p := cliout.New(stdout)
	p.Command(command)
	if len(summaries) == 0 && len(storage) == 0 {
		p.Summary(cliout.StatusSkip, "clusters", "none declared")
		return
	}
	for _, summary := range summaries {
		p.Section("Cluster " + summary.Name)
		fields := []cliout.Field{
			{Key: "API", Value: emptyAccessValue(summary.APIURL)},
			{Key: "Console", Value: emptyAccessValue(summary.ConsoleURL)},
		}
		if sd := substrateDescriptor(summary.Substrate, summary.HostCluster); sd != "" {
			fields = append(fields, cliout.Field{Key: "Substrate", Value: sd})
		}
		fields = append(fields,
			cliout.Field{Key: "Kubeconfig", Value: summary.KubeconfigPath},
			cliout.Field{Key: "Kube context", Value: summary.KubeContextCommand},
			cliout.Field{Key: "Kubeadmin user", Value: summary.KubeadminUsername},
			cliout.Field{Key: "Password file", Value: summary.KubeadminPasswordPath},
			cliout.Field{Key: "Show password", Value: summary.KubeadminPasswordCommand},
		)
		p.Fields(fields)
		p.Status(accessArtifactStatus(summary.Kubeconfig), "kubeconfig", accessArtifactDetail(summary.Kubeconfig))
		p.Status(accessArtifactStatus(summary.KubeadminPassword), "kubeadmin password", accessArtifactDetail(summary.KubeadminPassword))
	}
	printStorageAccessSections(p, storage)
}

func clusterListEntries(summaries []clusteraccess.ClusterSummary) []clusterListEntry {
	entries := make([]clusterListEntry, 0, len(summaries))
	for _, summary := range summaries {
		entries = append(entries, clusterListEntry{
			Kind:              "container",
			Name:              summary.Name,
			InstallMode:       summary.InstallMode,
			InstallMethod:     summary.InstallMethod,
			Substrate:         summary.Substrate,
			HostCluster:       summary.HostCluster,
			APIURL:            summary.APIURL,
			ConsoleURL:        summary.ConsoleURL,
			Kubeconfig:        summary.Kubeconfig,
			KubeadminPassword: summary.KubeadminPassword,
			Ready:             summary.Ready,
		})
	}
	return entries
}

func storageClusterListEntries(state v1alpha1.State, clustersDir string) []clusterListEntry {
	entries := make([]clusterListEntry, 0, len(state.StorageClusters))
	for _, cluster := range state.StorageClusters {
		management := cluster.Spec.Management
		if management == "" {
			management = v1alpha1.StorageClusterManagementManaged
		}
		entry := clusterListEntry{
			Kind:       "storage",
			Name:       cluster.Metadata.Name,
			Type:       cluster.Spec.Type,
			Management: management,
		}
		if management == v1alpha1.StorageClusterManagementManaged && cluster.Spec.Ceph != nil {
			if path := clusteraccess.StorageDashboardPasswordPath(clustersDir, cluster.Metadata.Name); path != "" {
				entry.DashboardPassword = clusteraccess.FileStatus(path)
			}
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries
}

func clusterAccessStatus(summary clusteraccess.ClusterSummary) cliout.Status {
	switch {
	case summary.Kubeconfig.Present && summary.KubeadminPassword.Present:
		return cliout.StatusOK
	case summary.Kubeconfig.Present || summary.KubeadminPassword.Present:
		return cliout.StatusWarn
	default:
		return cliout.StatusMissing
	}
}

func accessArtifactStatus(artifact clusteraccess.Artifact) cliout.Status {
	if artifact.Present {
		return cliout.StatusOK
	}
	return cliout.StatusMissing
}

func accessArtifactDetail(artifact clusteraccess.Artifact) string {
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
