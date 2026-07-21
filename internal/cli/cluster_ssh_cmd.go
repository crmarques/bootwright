package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/clusteraccess"
	"github.com/crmarques/bootwright/internal/render"
	stateview "github.com/crmarques/bootwright/internal/state/view"
	"github.com/crmarques/bootwright/internal/workspace"
)

func newClusterRshCmd() *cobra.Command {
	var clusterName, node string
	cmd := &cobra.Command{
		Use:   "rsh --name <cluster> --node <node>",
		Short: "Open an interactive SSH shell on a cluster node",
		Long: `Open an interactive remote shell on a node of a container or storage cluster over
SSH. Container cluster nodes use the cluster's install.nodeSSH private key and
the core user; storage cluster nodes use the backing Machine's SSH identity.
Select the node by its node name, its FQDN, or its <role>-<ordinal>
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
	cmd.Flags().StringVar(&node, "node", "", "node to connect to: node name, FQDN, or <role>-<ordinal> (default: the only node)")
	_ = cmd.MarkFlagRequired("name")
	registerAccessClusterNameCompletion(cmd)
	registerClusterNodeCompletion(cmd)
	cf := addCommonFlags()
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return failErr(2, fmt.Errorf("cluster rsh opens an interactive shell and takes no command; run one with 'cluster exec --name %s --node %s -- %s'", clusterName, node, strings.Join(args, " ")))
		}
		machine, state, err := resolveClusterNodeMachine(cf, clusterName, node)
		if err != nil {
			return err
		}
		return execSSHToClusterNode(cmd.Context(), cf.ctx, state, clusterName, machine, nil)
	}
	return cmd
}

func newClusterExecCmd() *cobra.Command {
	var clusterName, node string
	cmd := &cobra.Command{
		Use:   "exec --name <cluster> --node <node> -- <command>...",
		Short: "Run a command on a cluster node over SSH",
		Long: `Run a single command on a node of a container or storage cluster over SSH and
return its output. Select the node by its node name, its FQDN, or its
<role>-<ordinal> (e.g. master-0); on a single-node cluster --node may be omitted.
Drop into an interactive shell instead with 'cluster rsh'.

    bootwright cluster exec --name managed-01 --node master-0 -- systemctl status kubelet`,
		Args: cobra.ArbitraryArgs,
		Example: `  # Run one command on a node
  bootwright cluster exec --name managed-01 --node master-0 -- systemctl status kubelet`,
	}
	cmd.Flags().StringVar(&clusterName, "name", "", "ContainerCluster or StorageCluster name (required)")
	cmd.Flags().StringVar(&node, "node", "", "node to run the command on: node name, FQDN, or <role>-<ordinal> (default: the only node)")
	_ = cmd.MarkFlagRequired("name")
	registerAccessClusterNameCompletion(cmd)
	registerClusterNodeCompletion(cmd)
	cf := addCommonFlags()
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return failErr(2, errors.New("cluster exec requires a command after --, e.g. 'cluster exec --name <cluster> --node <node> -- systemctl status kubelet'"))
		}
		machine, state, err := resolveClusterNodeMachine(cf, clusterName, node)
		if err != nil {
			return err
		}
		return execSSHToClusterNode(cmd.Context(), cf.ctx, state, clusterName, machine, args)
	}
	return cmd
}

func execSSHToClusterNode(commandCtx context.Context, ctx workspace.Context, state v1alpha1.State, clusterName, machineName string, cmdArgs []string) error {
	target, err := clusterNodeSSHTarget(state, clusterName, machineName)
	if err != nil {
		return failErr(1, err)
	}
	return execSSHTarget(commandCtx, ctx, state, target, cmdArgs)
}

func clusterNodeSSHTarget(state v1alpha1.State, clusterName, machineName string) (sshTarget, error) {
	cluster, ok := stateview.ContainerCluster(state, clusterName)
	if !ok {
		return machineSSHTarget(state, machineName)
	}
	privateRef := cluster.Spec.Install.NodeSSH.PrivateMaterialRef()
	if privateRef.Name == "" {
		return sshTarget{}, fmt.Errorf("ContainerCluster %q install.nodeSSH has no private key; set keyPairRef or privateKeyRef for cluster SSH access", clusterName)
	}
	if _, ok := stateview.Machine(state, machineName); !ok {
		return sshTarget{}, fmt.Errorf("ContainerCluster %q references unknown Machine %q", clusterName, machineName)
	}
	address := render.ContainerNodePrimaryAddress(state, cluster, machineName)
	if address == "" {
		for _, host := range cluster.Spec.Hosts {
			if host.MachineRef.Name == machineName {
				address = host.Hostname
				break
			}
		}
	}
	if address == "" {
		return sshTarget{}, fmt.Errorf("ContainerCluster %q node Machine %q has no primary install IP or hostname for SSH access", clusterName, machineName)
	}
	return sshTarget{
		Address: address,
		User:    "core",
		KeyRef:  privateRef,
	}, nil
}

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
