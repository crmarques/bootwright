package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/clusteraccess"
)

func newClusterRshCmd() *cobra.Command {
	var clusterName, node string
	cmd := &cobra.Command{
		Use:   "rsh --name <cluster> --node <node>",
		Short: "Open an interactive SSH shell on a cluster node",
		Long: `Open an interactive remote shell on a node of a container or storage cluster over
SSH, using the identity Bootwright already knows for the node's backing Machine.
Select the node by its Machine name, its hostname, or its <role>-<ordinal>
(e.g. master-0); on a single-node cluster --node may be omitted. Run a single
command instead with 'cluster exec'.

    bootwright cluster rsh --name managed-01 --node master-0`,
		Args: cobra.ArbitraryArgs,
		Example: `  # Interactive shell on a node
  bootwright cluster rsh --name managed-01 --node master-0

  # Single-node (SNO) cluster: --node is optional
  bootwright cluster rsh --name sno-libvirt`,
	}
	cmd.Flags().StringVar(&clusterName, "name", "", "ContainerCluster or StorageCluster name (required)")
	cmd.Flags().StringVar(&node, "node", "", "node to connect to: Machine name, hostname, or <role>-<ordinal> (default: the only node)")
	_ = cmd.MarkFlagRequired("name")
	registerAccessClusterNameCompletion(cmd)
	registerClusterNodeCompletion(cmd)
	cf := addCommonFlags()
	cmd.RunE = func(_ *cobra.Command, args []string) error {
		if len(args) > 0 {
			return failErr(2, fmt.Errorf("cluster rsh opens an interactive shell and takes no command; run one with 'cluster exec --name %s --node %s -- %s'", clusterName, node, strings.Join(args, " ")))
		}
		machine, state, err := resolveClusterNodeMachine(cf, clusterName, node)
		if err != nil {
			return err
		}
		return execSSHToMachine(cf.ctx, state, machine, nil)
	}
	return cmd
}

func newClusterExecCmd() *cobra.Command {
	var clusterName, node string
	cmd := &cobra.Command{
		Use:   "exec --name <cluster> --node <node> -- <command>...",
		Short: "Run a command on a cluster node over SSH",
		Long: `Run a single command on a node of a container or storage cluster over SSH and
return its output. Select the node by its Machine name, its hostname, or its
<role>-<ordinal> (e.g. master-0); on a single-node cluster --node may be omitted.
Drop into an interactive shell instead with 'cluster rsh'.

    bootwright cluster exec --name managed-01 --node master-0 -- systemctl status kubelet`,
		Args: cobra.ArbitraryArgs,
		Example: `  # Run one command on a node
  bootwright cluster exec --name managed-01 --node master-0 -- systemctl status kubelet`,
	}
	cmd.Flags().StringVar(&clusterName, "name", "", "ContainerCluster or StorageCluster name (required)")
	cmd.Flags().StringVar(&node, "node", "", "node to run the command on: Machine name, hostname, or <role>-<ordinal> (default: the only node)")
	_ = cmd.MarkFlagRequired("name")
	registerAccessClusterNameCompletion(cmd)
	registerClusterNodeCompletion(cmd)
	cf := addCommonFlags()
	cmd.RunE = func(_ *cobra.Command, args []string) error {
		if len(args) == 0 {
			return failErr(2, errors.New("cluster exec requires a command after --, e.g. 'cluster exec --name <cluster> --node <node> -- systemctl status kubelet'"))
		}
		machine, state, err := resolveClusterNodeMachine(cf, clusterName, node)
		if err != nil {
			return err
		}
		return execSSHToMachine(cf.ctx, state, machine, args)
	}
	return cmd
}

// resolveClusterNodeMachine loads desired state, validates the cluster name, and
// resolves --node to its backing Machine — the shared front half of both
// cluster rsh and cluster exec before they hand off to execSSHToMachine. It
// returns the loaded state so the caller reuses it for the ssh invocation.
func resolveClusterNodeMachine(cf *commonFlags, clusterName, node string) (string, v1alpha1.State, error) {
	state, err := loadDesiredState(cf)
	if err != nil {
		return "", v1alpha1.State{}, failErr(1, err)
	}
	if err := clusteraccess.ValidateAccessClusterName(state, clusterName, true); err != nil {
		return "", v1alpha1.State{}, failErr(2, err)
	}
	machine, err := clusterNodeMachine(state, clusterName, node)
	if err != nil {
		return "", v1alpha1.State{}, failErr(2, err)
	}
	return machine, state, nil
}
