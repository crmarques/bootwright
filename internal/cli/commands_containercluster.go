package cli

import (
	"context"
	"slices"

	containeraccess "github.com/crmarques/bootwright/internal/containercluster/access"
	"github.com/crmarques/bootwright/internal/containercluster/installation"
)

func installerRenderCommand() commandSpec {
	return commandSpec{path: "render installer", invoke: Services.invokeRenderInstaller, short: "Render container-cluster installer artifacts", flags: []flagSpec{clustersFlag(), sensitiveFlag()}}
}

func containerAccessCommands() []commandSpec {
	return []commandSpec{
		available(accessCommand(commandSpec{path: "cluster oc", example: "--context lab --name cluster -- get nodes", invoke: Services.invokeClusterOc, short: "Print an oc access descriptor; do not launch a client or connect", flags: []flagSpec{nameFlag()}, payload: true}, " Applicable to OpenShiftClusters only.")),
		available(accessCommand(commandSpec{path: "cluster kubectl", example: "--context lab --name cluster -- get nodes", invoke: Services.invokeClusterKubectl, short: "Print a kubectl access descriptor; do not launch a client or connect", flags: []flagSpec{nameFlag()}, payload: true}, " Applicable to OpenShiftClusters only.")),
		available(commandSpec{path: "cluster kubeconfig", example: "--context lab --name cluster", invoke: Services.invokeClusterKubeconfig, short: "Export raw sensitive kubeconfig bytes to standard output", flags: []flagSpec{nameFlag()}, long: "Export raw sensitive kubeconfig bytes to standard output. Applicable to OpenShiftClusters only."}),
	}
}

type InstallerService interface {
	Render(context.Context, installation.RenderInstallerRequest) error
}

type ClusterAccessService interface {
	OC(context.Context, containeraccess.OCRequest) (*containeraccess.Handoff, error)
	Kubectl(context.Context, containeraccess.KubectlRequest) (*containeraccess.Handoff, error)
	Kubeconfig(context.Context, containeraccess.KubeconfigRequest) (*containeraccess.KubeconfigResult, error)
}

func (s Services) invokeRenderInstaller(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.Installer == nil {
		return commandResult{}, errMissingService
	}
	return commandResult{}, invokeRequest(ctx, values, installation.RenderInstallerRequest{
		ContextName: values.text("context"),
		Clusters:    values.names("clusters"),
		Sensitive:   values.boolean("sensitive"),
	}, s.Installer.Render)
}

func (s Services) invokeClusterOc(ctx context.Context, values *requestValues, args []string) (commandResult, error) {
	if s.ClusterAccess == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, containeraccess.OCRequest{
		ContextName: values.text("context"),
		Name:        values.text("name"),
		Command:     slices.Clone(args),
	}, s.ClusterAccess.OC)
	return commandResult{handoff: result}, err
}

func (s Services) invokeClusterKubectl(ctx context.Context, values *requestValues, args []string) (commandResult, error) {
	if s.ClusterAccess == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, containeraccess.KubectlRequest{
		ContextName: values.text("context"),
		Name:        values.text("name"),
		Command:     slices.Clone(args),
	}, s.ClusterAccess.Kubectl)
	return commandResult{handoff: result}, err
}

func (s Services) invokeClusterKubeconfig(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.ClusterAccess == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, containeraccess.KubeconfigRequest{
		ContextName: values.text("context"),
		Name:        values.text("name"),
	}, s.ClusterAccess.Kubeconfig)
	return commandResult{kubeconfig: result}, err
}
