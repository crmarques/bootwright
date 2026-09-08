package cli

import (
	"context"
	"errors"
	"slices"

	environmentaccess "github.com/crmarques/bootwright/internal/environment/access"
	"github.com/crmarques/bootwright/internal/environment/inspection"
	environmentpreflight "github.com/crmarques/bootwright/internal/environment/preflight"
)

func infrastructurePreflightCommand() commandSpec {
	return commandSpec{path: "preflight infra", short: "Check infrastructure readiness", flags: preflightFlags()}
}

func clustersPreflightCommand() commandSpec {
	return commandSpec{path: "preflight clusters", short: "Check selected cluster readiness", flags: preflightFlags()}
}

func allPreflightCommand() commandSpec {
	return commandSpec{path: "preflight all", short: "Check all prerequisites", flags: []flagSpec{dryRunFlag(), outputFlag(), trustFlag(), verboseFlag()}}
}

func environmentClusterCommands() []commandSpec {
	return []commandSpec{
		{path: "cluster list", short: "List container and storage clusters", flags: []flagSpec{outputFlag()}},
		{path: "cluster info", short: "Show cluster details and static access applicability", flags: []flagSpec{stringFlag("name", "Select one cluster (default: all)"), boolFlag("secrets", "Include explicitly selected sensitive values"), outputFlag()}},
		accessCommand(commandSpec{path: "cluster rsh", short: "Print an SSH access descriptor; do not launch a client or connect", flags: []flagSpec{nameFlag(), stringFlag("node", "Select a declared node, FQDN, or role-ordinal")}}, " Applicable to OpenShift/OKD ContainerClusters and managed Ceph StorageClusters with declared nodes. External StorageClusters are inapplicable."),
		accessCommand(commandSpec{path: "cluster exec", short: "Print a command access descriptor; do not launch a client or connect", flags: []flagSpec{nameFlag(), stringFlag("node", "Select a declared node, FQDN, or role-ordinal")}, payload: true}, " Applicable to OpenShift/OKD ContainerClusters and managed Ceph StorageClusters with declared nodes. External StorageClusters are inapplicable."),
	}
}

type EnvironmentPreflightService interface {
	PreflightInfrastructure(context.Context, environmentpreflight.InfrastructurePreflightRequest) error
	PreflightClusters(context.Context, environmentpreflight.ClustersPreflightRequest) error
	PreflightAll(context.Context, environmentpreflight.AllPreflightRequest) error
}

type EnvironmentInspectionService interface {
	ListClusters(context.Context, inspection.ListClustersRequest) error
	ClusterInfo(context.Context, inspection.ClusterInfoRequest) error
}

type EnvironmentAccessService interface {
	ClusterRsh(context.Context, environmentaccess.ClusterRshRequest) error
	ClusterExec(context.Context, environmentaccess.ClusterExecRequest) error
}

func (s Services) invokeEnvironmentPreflight(ctx context.Context, path string, values *requestValues, args []string) error {
	if s.EnvironmentPreflight == nil {
		return errMissingService
	}
	switch path {
	case "preflight infra":
		return invokeRequest(ctx, values, environmentpreflight.InfrastructurePreflightRequest{
			ContextName:     values.text("context"),
			Clusters:        values.names("clusters"),
			DryRun:          values.boolean("dry-run"),
			TrustOnFirstUse: values.boolean("trust-on-first-use"),
			Verbose:         values.boolean("verbose"),
			SSH:             values.ssh(),
		}, s.EnvironmentPreflight.PreflightInfrastructure)
	case "preflight clusters":
		return invokeRequest(ctx, values, environmentpreflight.ClustersPreflightRequest{
			ContextName:     values.text("context"),
			Clusters:        values.names("clusters"),
			DryRun:          values.boolean("dry-run"),
			TrustOnFirstUse: values.boolean("trust-on-first-use"),
			Verbose:         values.boolean("verbose"),
			SSH:             values.ssh(),
		}, s.EnvironmentPreflight.PreflightClusters)
	case "preflight all":
		return invokeRequest(ctx, values, environmentpreflight.AllPreflightRequest{
			ContextName:     values.text("context"),
			DryRun:          values.boolean("dry-run"),
			TrustOnFirstUse: values.boolean("trust-on-first-use"),
			Verbose:         values.boolean("verbose"),
			SSH:             values.ssh(),
		}, s.EnvironmentPreflight.PreflightAll)
	default:
		return errors.New("command has no application dispatch")
	}
}

func (s Services) invokeEnvironmentInspection(ctx context.Context, path string, values *requestValues, args []string) error {
	if s.EnvironmentInspection == nil {
		return errMissingService
	}
	switch path {
	case "cluster list":
		return invokeRequest(ctx, values, inspection.ListClustersRequest{
			ContextName: values.text("context"),
		}, s.EnvironmentInspection.ListClusters)
	case "cluster info":
		return invokeRequest(ctx, values, inspection.ClusterInfoRequest{
			ContextName: values.text("context"),
			Name:        values.text("name"),
			Secrets:     values.boolean("secrets"),
		}, s.EnvironmentInspection.ClusterInfo)
	default:
		return errors.New("command has no application dispatch")
	}
}

func (s Services) invokeEnvironmentAccess(ctx context.Context, path string, values *requestValues, args []string) error {
	if s.EnvironmentAccess == nil {
		return errMissingService
	}
	switch path {
	case "cluster rsh":
		return invokeRequest(ctx, values, environmentaccess.ClusterRshRequest{
			ContextName: values.text("context"),
			Name:        values.text("name"),
			Node:        values.text("node"),
			SSH:         values.ssh(),
		}, s.EnvironmentAccess.ClusterRsh)
	case "cluster exec":
		return invokeRequest(ctx, values, environmentaccess.ClusterExecRequest{
			ContextName: values.text("context"),
			Name:        values.text("name"),
			Node:        values.text("node"),
			SSH:         values.ssh(),
			Command:     slices.Clone(args),
		}, s.EnvironmentAccess.ClusterExec)
	default:
		return errors.New("command has no application dispatch")
	}
}
