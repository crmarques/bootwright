package cli

import (
	"github.com/spf13/cobra"

	stateview "github.com/crmarques/bootwright/internal/state/view"
)

// registerMachineNameCompletion offers declared Machine names as completions for
// a command's --name flag (machine rsh/exec). It loads state without escalating;
// on any error it returns no completions rather than failing.
func registerMachineNameCompletion(cmd *cobra.Command) {
	_ = cmd.RegisterFlagCompletionFunc("name", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		state, err := loadDesiredStateLocalOnly(addCommonFlags())
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		names := make([]string, 0, len(state.Machines))
		for _, m := range state.Machines {
			names = append(names, m.Metadata.Name)
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	})
}

// registerAccessClusterNameCompletion offers container and storage cluster names
// as completions for a command's --name flag (cluster info/rsh/exec).
func registerAccessClusterNameCompletion(cmd *cobra.Command) {
	_ = cmd.RegisterFlagCompletionFunc("name", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		state, err := loadDesiredStateLocalOnly(addCommonFlags())
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		names := make([]string, 0, len(state.ContainerClusters)+len(state.StorageClusters))
		for _, c := range state.ContainerClusters {
			names = append(names, c.Metadata.Name)
		}
		for _, c := range state.StorageClusters {
			names = append(names, c.Metadata.Name)
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	})
}

// registerClusterNodeCompletion offers the nodes of the cluster named by the
// already-typed --name flag as completions for --node, one entry per node's
// backing Machine name (the always-valid selector).
func registerClusterNodeCompletion(cmd *cobra.Command) {
	_ = cmd.RegisterFlagCompletionFunc("node", func(c *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		clusterName := c.Flags().Lookup("name").Value.String()
		if clusterName == "" {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		state, err := loadDesiredStateLocalOnly(addCommonFlags())
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		nodes, ok := stateview.ClusterNodes(state, clusterName)
		if !ok {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		out := make([]string, 0, len(nodes))
		for _, n := range nodes {
			out = append(out, n.MachineName)
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	})
}
