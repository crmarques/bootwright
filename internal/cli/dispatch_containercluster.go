package cli

import (
	"context"
	"errors"
	"slices"

	"github.com/crmarques/bootwright/internal/containercluster"
)

func (s Services) invokeContainerPreflight(ctx context.Context, path string, values *requestValues, args []string) error {
	if s.ContainerPreflight == nil {
		return errMissingService
	}
	switch path {
	case "preflight container-cluster":
		return invokeRequest(ctx, values, containercluster.PreflightRequest{
			ContextName:     values.text("context"),
			Clusters:        values.names("clusters"),
			DryRun:          values.boolean("dry-run"),
			TrustOnFirstUse: values.boolean("trust-on-first-use"),
			Verbose:         values.boolean("verbose"),
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
		return invokeRequest(ctx, values, containercluster.RenderInstallerRequest{
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
		return invokeRequest(ctx, values, containercluster.OCRequest{
			ContextName: values.text("context"),
			Name:        values.text("name"),
			Command:     slices.Clone(args),
		}, s.ClusterAccess.OC)
	case "cluster kubectl":
		return invokeRequest(ctx, values, containercluster.KubectlRequest{
			ContextName: values.text("context"),
			Name:        values.text("name"),
			Command:     slices.Clone(args),
		}, s.ClusterAccess.Kubectl)
	case "cluster kubeconfig":
		return invokeRequest(ctx, values, containercluster.KubeconfigRequest{
			ContextName: values.text("context"),
			Name:        values.text("name"),
		}, s.ClusterAccess.Kubeconfig)
	default:
		return errors.New("command has no application dispatch")
	}
}
