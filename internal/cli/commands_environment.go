package cli

import (
	"context"
	"errors"
	"slices"

	environmentaccess "github.com/crmarques/bootwright/internal/environment/access"
	"github.com/crmarques/bootwright/internal/environment/inspection"
	environmentpreflight "github.com/crmarques/bootwright/internal/environment/preflight"
	"github.com/crmarques/bootwright/internal/machine"
)

func preflightCommand() commandSpec {
	stage := stringFlag("stage", "Select comma-separated stages to check (default: every stage but add-ons)")
	stage.enum, stage.enumList = stageFlag().enum, true
	return available(commandSpec{path: "preflight", short: "Check the selected context's readiness for apply, stage by stage", flags: []flagSpec{stage, clustersFlag(), dryRunFlag(), trustFlag()},
		long: "Check, without changing anything, whether the selected context is ready for the stages apply runs, in one report." +
			" controller checks the controller host's prerequisites, the target tools this context's controller stage selects and its host binding;" +
			" infrastructure-services checks the firewall admission of the controller's managed service sockets;" +
			" substrates checks the capacity of the hosts that hold the context's Machines;" +
			" machines has no checks; clusters checks the OpenShiftClusters and CephClusters that --clusters selects, or all of them." +
			" add-ons has no checks yet and is refused." +
			" It reports only what it observes: external services, a Registry or LoadBalancer and managed services on other Machines are not checked, and apply checks them itself." +
			" The SSH and trust-on-first-use options apply to the CephCluster checks. --dry-run runs no process and reports every check pending." +
			" For the controller host alone, before any context exists, run controller check."})
}

func environmentClusterCommands() []commandSpec {
	return []commandSpec{
		available(commandSpec{path: "cluster list", short: "List container and storage clusters", long: "List container and storage clusters in canonical name order. Applicable to all selected OpenShiftClusters and CephClusters, including external storage clusters."}),
		available(commandSpec{path: "cluster info", short: "Show cluster details and static access applicability", long: "Show cluster details and static access applicability, readiness and invocation templates from local desired state and evidence, with no host contact." +
			" Applicable to all selected OpenShiftClusters and CephClusters, including external storage clusters." +
			" A managed CephCluster also reports its release, FSID, frozen image, monitors and administrator command." +
			" --name takes a comma-separated list." +
			" --secrets and --object-user each require --name naming exactly one cluster and apply to managed CephClusters only:" +
			" --secrets reveals the dashboard administrator password, and --object-user <name> reveals that CephObjectUser's S3 keys alone.", flags: []flagSpec{stringFlag("name", "Select comma-separated cluster names (default: all)"), boolFlag("secrets", "Reveal the dashboard administrator password"), stringFlag("object-user", "Reveal the S3 keys of this one CephObjectUser")}}),
		available(clusterSessionCommand(commandSpec{path: "cluster rsh", short: "Open an SSH session on a cluster node's Machine"})),
		available(clusterExecCommand()),
	}
}

func clusterSessionCommand(spec commandSpec) commandSpec {
	spec.flags = []flagSpec{nameFlag(), stringFlag("node", "Select a declared node by name, then FQDN (default: the first node in name order)")}
	spec = sessionCommand(spec)
	spec.long += " The session opens on the Machine behind one node of a managed CephCluster: --node selects" +
		" the node by name, then by FQDN, and defaults to the first node in name order." +
		clusterSessionLimits
	return spec
}

const clusterSessionLimits = " External CephClusters are inapplicable, and sessions on OpenShiftClusters are not yet available."

func clusterExecCommand() commandSpec {
	spec := sessionCommand(commandSpec{path: "cluster exec", short: "Run a command over SSH on cluster nodes' Machines", payload: true,
		flags: []flagSpec{requiredFlag("name", "Select comma-separated cluster names"),
			stringFlag("node", "Select declared nodes by comma-separated name, then FQDN (default: each cluster's first node in name order)")}})
	spec.long += " --name takes a comma-separated list, and each cluster runs the command on the Machine behind its first node in name order." +
		" --node takes a comma-separated list and selects those nodes, by name then by FQDN, in every listed cluster that declares them." +
		" The command runs on every resolved Machine concurrently, at most eight at a time, each output line is prefixed by its Machine, and the exit status is the highest any client returned." +
		clusterSessionLimits
	return spec
}

type EnvironmentPreflightService interface {
	Preflight(context.Context, environmentpreflight.PreflightRequest) (*environmentpreflight.Report, error)
}

type EnvironmentInspectionService interface {
	ListClusters(context.Context, inspection.ListClustersRequest) (*inspection.ListResult, error)
	ClusterInfo(context.Context, inspection.ClusterInfoRequest) (*inspection.InfoResult, error)
	ClientKeyring(context.Context, inspection.KeyringRequest) (*inspection.KeyringResult, error)
}

type EnvironmentAccessService interface {
	ClusterRsh(context.Context, environmentaccess.ClusterRshRequest) (*machine.SessionResult, error)
	ClusterExec(context.Context, environmentaccess.ClusterExecRequest) (*machine.SessionResult, error)
}

func (s Services) invokeEnvironmentPreflight(ctx context.Context, values *requestValues) (commandResult, error) {
	if s.EnvironmentPreflight == nil {
		return commandResult{}, errMissingService
	}
	report, err := invokeResult(ctx, values, environmentpreflight.PreflightRequest{
		ContextName:     values.text("context"),
		Stages:          values.names("stage"),
		Clusters:        values.names("clusters"),
		DryRun:          values.boolean("dry-run"),
		TrustOnFirstUse: values.boolean("trust-on-first-use"),
		SSH:             values.ssh(),
	}, s.EnvironmentPreflight.Preflight)
	return commandResult{storagePreflight: report}, err
}

func (s Services) invokeEnvironmentInspection(ctx context.Context, path string, values *requestValues, args []string) (commandResult, error) {
	if s.EnvironmentInspection == nil {
		return commandResult{}, errMissingService
	}
	switch path {
	case "cluster list":
		result, err := invokeResult(ctx, values, inspection.ListClustersRequest{
			ContextName: values.text("context"),
		}, s.EnvironmentInspection.ListClusters)
		return commandResult{clusterList: result}, err
	case "cluster info":
		result, err := invokeResult(ctx, values, inspection.ClusterInfoRequest{
			ContextName: values.text("context"),
			Names:       values.names("name"),
			Secrets:     values.boolean("secrets"),
			ObjectUser:  values.text("object-user"),
		}, s.EnvironmentInspection.ClusterInfo)
		return commandResult{clusterInfo: result}, err
	case "cluster keyring":
		result, err := invokeResult(ctx, values, inspection.KeyringRequest{
			ContextName: values.text("context"),
			Name:        values.text("name"),
			Client:      values.text("client"),
		}, s.EnvironmentInspection.ClientKeyring)
		return commandResult{keyring: result}, err
	default:
		return commandResult{}, errors.New("command has no application dispatch")
	}
}

func (s Services) invokeEnvironmentAccess(ctx context.Context, path string, values *requestValues, args []string) (commandResult, error) {
	if s.EnvironmentAccess == nil {
		return commandResult{}, errMissingService
	}
	switch path {
	case "cluster rsh":
		result, err := invokeResult(ctx, values, environmentaccess.ClusterRshRequest{
			ContextName: values.text("context"),
			Name:        values.text("name"),
			Node:        values.text("node"),
			SSH:         values.sshAccount(),
		}, s.EnvironmentAccess.ClusterRsh)
		return commandResult{session: result}, err
	case "cluster exec":
		result, err := invokeResult(ctx, values, environmentaccess.ClusterExecRequest{
			ContextName: values.text("context"),
			Names:       values.names("name"),
			Nodes:       values.names("node"),
			SSH:         values.sshAccount(),
			Command:     slices.Clone(args),
		}, s.EnvironmentAccess.ClusterExec)
		return commandResult{session: result}, err
	default:
		return commandResult{}, errors.New("command has no application dispatch")
	}
}
