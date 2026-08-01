package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/clusteraccess"
	"github.com/crmarques/bootwright/internal/workspace"
)

func newContainerClusterCmd(stdout io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "container-cluster <command>",
		Short: "Talk to an installed container cluster with its admin credentials",
	}
	cmd.AddCommand(
		newContainerClusterOcCmd(),
		newContainerClusterKubectlCmd(),
		newContainerClusterKubeconfigCmd(stdout),
	)
	requireSubcommand(cmd)
	return cmd
}

func newContainerClusterKubeconfigCmd(stdout io.Writer) *cobra.Command {
	clusterName := ""
	cmd := &cobra.Command{
		Use:   "kubeconfig --name <cluster>",
		Short: "Print the admin kubeconfig for an installed cluster",
		Long: `Print the generated admin kubeconfig for an installed container cluster to
stdout, so you can save it to a file you own instead of copying the root-owned
source by hand:

    bootwright container-cluster kubeconfig --name managed-01 > ~/.kube/managed-01
    oc --kubeconfig ~/.kube/managed-01 get nodes

The kubeconfig is admin credential material; redirect it to a private path and
do not commit it.`,
		Args: cobra.NoArgs,
		Example: `  # Save one cluster's admin kubeconfig to a private file
  bootwright container-cluster kubeconfig --name managed-01 > ~/.kube/managed-01`,
	}
	cmd.Flags().StringVar(&clusterName, "name", "", "ContainerCluster name (required)")
	_ = cmd.MarkFlagRequired("name")
	registerContainerClusterNameCompletion(cmd)
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
		data, err := clusteraccess.Kubeconfig(state, cf.ctx.Name, clustersDir, clusterName)
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

func containerAccessCommandFields(name string) []cliout.Field {
	return []cliout.Field{
		{Key: "oc", Value: "bootwright container-cluster oc --name " + name + " get nodes"},
		{Key: "kubectl", Value: "bootwright container-cluster kubectl --name " + name + " get nodes"},
		{Key: "Kubeconfig", Value: "bootwright container-cluster kubeconfig --name " + name},
	}
}
