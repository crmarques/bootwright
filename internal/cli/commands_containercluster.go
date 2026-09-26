package cli

import (
	"context"
	"errors"
	"slices"

	containeraccess "github.com/crmarques/bootwright/internal/containercluster/access"
	"github.com/crmarques/bootwright/internal/containercluster/installation"
	containerpreflight "github.com/crmarques/bootwright/internal/containercluster/preflight"
)

func containerPreflightCommand() commandSpec {
	return commandSpec{path: "preflight container-cluster", short: "Check ContainerCluster readiness", flags: preflightFlags()}
}

func installerRenderCommand() commandSpec {
	return commandSpec{path: "render installer", short: "Render container-cluster installer artifacts", flags: []flagSpec{clustersFlag(), sensitiveFlag(), outputFlag()}}
}

func containerAccessCommands() []commandSpec {
	return []commandSpec{
		accessCommand(commandSpec{path: "cluster oc", short: "Print an oc access descriptor; do not launch a client or connect", flags: []flagSpec{nameFlag()}, payload: true, stopAtPayload: true}, " Applicable to OpenShift/OKD ContainerClusters only."),
		accessCommand(commandSpec{path: "cluster kubectl", short: "Print a kubectl access descriptor; do not launch a client or connect", flags: []flagSpec{nameFlag()}, payload: true, stopAtPayload: true}, " Applicable to OpenShift/OKD ContainerClusters only."),
		{path: "cluster kubeconfig", short: "Export raw sensitive kubeconfig bytes to standard output", flags: []flagSpec{nameFlag()}, long: "Export raw sensitive kubeconfig bytes to standard output. Applicable to OpenShift/OKD ContainerClusters only."},
	}
}

type ContainerPreflightService interface {
	Check(context.Context, containerpreflight.PreflightRequest) error
}

type InstallerService interface {
	Render(context.Context, installation.RenderInstallerRequest) error
}

type ClusterAccessService interface {
	OC(context.Context, containeraccess.OCRequest) error
	Kubectl(context.Context, containeraccess.KubectlRequest) error
	Kubeconfig(context.Context, containeraccess.KubeconfigRequest) error
}

func (s Services) invokeContainerPreflight(ctx context.Context, path string, values *requestValues, args []string) error {
	if s.ContainerPreflight == nil {
		return errMissingService
	}
	switch path {
	case "preflight container-cluster":
		return invokeRequest(ctx, values, containerpreflight.PreflightRequest{
			ContextName:     values.text("context"),
			Clusters:        values.names("clusters"),
			DryRun:          values.boolean("dry-run"),
			TrustOnFirstUse: values.boolean("trust-on-first-use"),
			SSH:             values.ssh(),
		}, s.ContainerPreflight.Check)
	default:
		return errors.New("command has no application dispatch")
	}
}

func (s Services) invokeInstaller(ctx context.Context, path string, values *requestValues, args []string) error {
	if s.Installer == nil {
		return errMissingService
	}
	switch path {
	case "render installer":
		return invokeRequest(ctx, values, installation.RenderInstallerRequest{
			ContextName: values.text("context"),
			Clusters:    values.names("clusters"),
			Sensitive:   values.boolean("sensitive"),
		}, s.Installer.Render)
	default:
		return errors.New("command has no application dispatch")
	}
}

func (s Services) invokeClusterAccess(ctx context.Context, path string, values *requestValues, args []string) error {
	if s.ClusterAccess == nil {
		return errMissingService
	}
	switch path {
	case "cluster oc":
		return invokeRequest(ctx, values, containeraccess.OCRequest{
			ContextName: values.text("context"),
			Name:        values.text("name"),
			Command:     slices.Clone(args),
		}, s.ClusterAccess.OC)
	case "cluster kubectl":
		return invokeRequest(ctx, values, containeraccess.KubectlRequest{
			ContextName: values.text("context"),
			Name:        values.text("name"),
			Command:     slices.Clone(args),
		}, s.ClusterAccess.Kubectl)
	case "cluster kubeconfig":
		return invokeRequest(ctx, values, containeraccess.KubeconfigRequest{
			ContextName: values.text("context"),
			Name:        values.text("name"),
		}, s.ClusterAccess.Kubeconfig)
	default:
		return errors.New("command has no application dispatch")
	}
}
