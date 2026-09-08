package cli

import (
	"context"
	"errors"
	"slices"

	"github.com/crmarques/bootwright/internal/environment"
)

func (s Services) invokeEnvironment(ctx context.Context, path string, values *requestValues, args []string) error {
	if s.Environment == nil {
		return errMissingService
	}
	switch path {
	case "preflight infra":
		return invokeRequest(ctx, values, environment.InfrastructurePreflightRequest{
			ContextName:     values.text("context"),
			Clusters:        values.names("clusters"),
			DryRun:          values.boolean("dry-run"),
			TrustOnFirstUse: values.boolean("trust-on-first-use"),
			Verbose:         values.boolean("verbose"),
			SSH:             values.ssh(),
		}, s.Environment.PreflightInfrastructure)
	case "preflight clusters":
		return invokeRequest(ctx, values, environment.ClustersPreflightRequest{
			ContextName:     values.text("context"),
			Clusters:        values.names("clusters"),
			DryRun:          values.boolean("dry-run"),
			TrustOnFirstUse: values.boolean("trust-on-first-use"),
			Verbose:         values.boolean("verbose"),
			SSH:             values.ssh(),
		}, s.Environment.PreflightClusters)
	case "preflight all":
		return invokeRequest(ctx, values, environment.AllPreflightRequest{
			ContextName:     values.text("context"),
			DryRun:          values.boolean("dry-run"),
			TrustOnFirstUse: values.boolean("trust-on-first-use"),
			Verbose:         values.boolean("verbose"),
			SSH:             values.ssh(),
		}, s.Environment.PreflightAll)
	case "cluster list":
		return invokeRequest(ctx, values, environment.ListClustersRequest{
			ContextName: values.text("context"),
		}, s.Environment.ListClusters)
	case "cluster info":
		return invokeRequest(ctx, values, environment.ClusterInfoRequest{
			ContextName: values.text("context"),
			Name:        values.text("name"),
			Secrets:     values.boolean("secrets"),
		}, s.Environment.ClusterInfo)
	case "cluster rsh":
		return invokeRequest(ctx, values, environment.ClusterRshRequest{
			ContextName: values.text("context"),
			Name:        values.text("name"),
			Node:        values.text("node"),
			SSH:         values.ssh(),
		}, s.Environment.ClusterRsh)
	case "cluster exec":
		return invokeRequest(ctx, values, environment.ClusterExecRequest{
			ContextName: values.text("context"),
			Name:        values.text("name"),
			Node:        values.text("node"),
			SSH:         values.ssh(),
			Command:     slices.Clone(args),
		}, s.Environment.ClusterExec)
	default:
		return errors.New("command has no application dispatch")
	}
}
