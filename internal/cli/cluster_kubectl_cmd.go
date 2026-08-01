package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/internal/clusteraccess"
	"github.com/crmarques/bootwright/internal/host/callerio"
	"github.com/crmarques/bootwright/internal/host/execution"
	"github.com/crmarques/bootwright/internal/workspace"
)

type clusterKubectlInvocation struct {
	Binary         string
	Args           []string
	KubeconfigPath string
}

type clusterKubectlDeps struct {
	run func(ctx context.Context, inv clusterKubectlInvocation) (int, error)
}

var defaultClusterKubectlDeps = clusterKubectlDeps{run: runClusterKubectlInvocation}

func newContainerClusterOcCmd() *cobra.Command {
	return newContainerClusterKubeClientCmd("oc")
}

func newContainerClusterKubectlCmd() *cobra.Command {
	return newContainerClusterKubeClientCmd("kubectl")
}

func newContainerClusterKubeClientCmd(binary string) *cobra.Command {
	clusterName := ""
	cmd := &cobra.Command{
		Use:   binary + " --name <cluster> <command>...",
		Short: "Run " + binary + " against an installed container cluster",
		Long: `Run ` + binary + ` against an installed container cluster using its admin
kubeconfig, which is encrypted at rest and never handed to you as a reusable
file. Bootwright decrypts it to a private, caller-owned temporary file for the
duration of the command, points KUBECONFIG at it, runs ` + binary + ` with the
arguments you pass, and removes the file afterwards.

Put bootwright's own --name (and any global --context) before the ` + binary + `
command; everything after the cluster name is handed to ` + binary + ` verbatim,
so its own flags (-n, -o, --selector, ...) and a shell pipeline work unchanged:

    bootwright container-cluster ` + binary + ` --name managed-01 get nodes
    bootwright container-cluster ` + binary + ` --name managed-01 get pods -A -o json | jq '.items | length'

` + binary + ` runs as you, not as root, with your PATH and HOME, so your plugins
and kube cache behave normally.`,
		Args: cobra.ArbitraryArgs,
		Example: `  # Run a command against a cluster
  bootwright container-cluster ` + binary + ` --name managed-01 get nodes

  # ` + binary + ` flags after the cluster name pass straight through
  bootwright container-cluster ` + binary + ` --name managed-01 get pods -n openshift-ingress

  # Pipe machine-readable output into another tool
  bootwright container-cluster ` + binary + ` --name managed-01 get nodes -o json | jq -r '.items[].metadata.name'`,
	}
	cmd.Flags().StringVar(&clusterName, "name", "", "ContainerCluster name (required)")
	_ = cmd.MarkFlagRequired("name")
	cmd.Flags().SetInterspersed(false)
	registerContainerClusterNameCompletion(cmd)
	cf := addCommonFlags()
	cmd.RunE = func(c *cobra.Command, args []string) error {
		if len(args) == 0 {
			return failErr(2, fmt.Errorf("container-cluster %s requires a command after the cluster name, e.g. 'bootwright container-cluster %s --name %s get nodes'", binary, binary, clusterNameOrPlaceholder(clusterName)))
		}
		return runClusterKubeClient(c.Context(), cf, binary, clusterName, args)
	}
	return cmd
}

func clusterNameOrPlaceholder(name string) string {
	if name == "" {
		return "<cluster>"
	}
	return name
}

func runClusterKubeClient(commandCtx context.Context, cf *commonFlags, binary, clusterName string, args []string) error {
	state, err := loadDesiredState(cf)
	if err != nil {
		return failErr(1, err)
	}
	if err := clusteraccess.ValidateClusterNames(state, []string{clusterName}); err != nil {
		return failErr(2, err)
	}
	clustersDir := workspace.ControllerClustersDir(cf.ctx.Name)
	kubeconfig, err := clusteraccess.Kubeconfig(state, cf.ctx.Name, clustersDir, clusterName)
	if err != nil {
		return failErr(1, err)
	}
	defer clear(kubeconfig)
	kubeconfigPath, cleanup, err := writeCallerReadableKubeconfig(kubeconfig)
	if err != nil {
		return failErr(1, err)
	}
	defer cleanup()
	code, err := defaultClusterKubectlDeps.run(commandCtx, clusterKubectlInvocation{
		Binary:         binary,
		Args:           args,
		KubeconfigPath: kubeconfigPath,
	})
	if err != nil {
		return failErr(1, fmt.Errorf("run %s: %w", binary, err))
	}
	if code != 0 {
		return silentExit(code)
	}
	return nil
}

func writeCallerReadableKubeconfig(data []byte) (string, func(), error) {
	file, err := os.CreateTemp("", ".bootwright-kubeconfig-*")
	if err != nil {
		return "", nil, fmt.Errorf("create temporary kubeconfig: %w", err)
	}
	path := file.Name()
	remove := func() { _ = os.Remove(path) }
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		remove()
		return "", nil, fmt.Errorf("chmod temporary kubeconfig: %w", err)
	}
	if uid, gid, ok := execution.CallerUIDGID(); ok {
		if err := file.Chown(int(uid), int(gid)); err != nil {
			_ = file.Close()
			remove()
			return "", nil, fmt.Errorf("chown temporary kubeconfig to caller: %w", err)
		}
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		remove()
		return "", nil, fmt.Errorf("write temporary kubeconfig: %w", err)
	}
	if err := file.Close(); err != nil {
		remove()
		return "", nil, fmt.Errorf("flush temporary kubeconfig: %w", err)
	}
	return path, remove, nil
}

func runClusterKubectlInvocation(ctx context.Context, inv clusterKubectlInvocation) (int, error) {
	return callerio.RunPassthrough(ctx, inv.Binary, inv.Args,
		[]string{"KUBECONFIG=" + inv.KubeconfigPath},
		os.Stdin, os.Stdout, os.Stderr)
}
