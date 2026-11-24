package cli

import (
	"context"

	environmentaccess "github.com/crmarques/bootwright/internal/environment/access"
	"github.com/crmarques/bootwright/internal/environment/inspection"
	environmentpreflight "github.com/crmarques/bootwright/internal/environment/preflight"
	"github.com/crmarques/bootwright/internal/machine"
)

func environmentClusterCommands() []commandSpec {
	return []commandSpec{
		available(commandSpec{path: "cluster list", results: []jsonResult{clusterListPresentation{}}, render: renderClusterList(), example: "--context lab", invoke: Services.invokeClusterList, short: "List container and storage clusters", long: "List container and storage clusters in canonical name order. Applicable to all selected OpenShiftClusters and CephClusters, including external storage clusters."}),
		available(commandSpec{path: "cluster info", classify: classifyRules{secretOutput: clusterInfoReveals}, results: []jsonResult{clusterInfoPresentation{}}, render: renderClusterInfo(), validate: validateClusterInfo, example: "--context lab --name storage", invoke: Services.invokeClusterInfo, short: "Show cluster details and static access applicability", long: "Show cluster details and static access applicability, readiness and invocation templates from local desired state and evidence, with no host contact." +
			" Applicable to all selected OpenShiftClusters and CephClusters, including external storage clusters." +
			" A managed CephCluster also reports its release, FSID, frozen image, monitors and administrator command." +
			" --name takes a comma-separated list." +
			" --secrets and --object-user each require --name naming exactly one cluster." +
			" --secrets reveals the console administrator user and password for OpenShift or OKD, or the dashboard administrator password for managed Ceph." +
			" --object-user <name> applies to managed Ceph only and reveals that CephObjectUser's S3 keys alone.", flags: []flagSpec{stringFlag("name", "Select comma-separated cluster names (default: all)"), boolFlag("secrets", "Reveal the console or dashboard administrator credentials"), stringFlag("object-user", "Reveal the S3 keys of this one CephObjectUser")}}),
		available(commandSpec{path: "cluster update-admin-creds", results: []jsonResult{adminCredentialsPresentation{}}, render: renderAdminCredentials(), validate: validateClusterAdminName, example: "--context lab --name cluster --login prometheus,alertmanager", invoke: Services.invokeAdminCredentials, short: "Generate and update a cluster's administrator logins", long: "Generate a fresh password for each administrator login of one cluster and update it in the running environment: the console administrator of OpenShift or OKD; the dashboard administrator, the Prometheus and Alertmanager logins behind the management gateway, and the Grafana admin password of a managed Ceph cluster. --login takes a comma-separated list of console, dashboard, prometheus, alertmanager and grafana (default: every login the cluster has); a listed login the cluster does not have refuses before anything changes. Requires a completed apply and the original administrator access. Runs the logins in that order, each retaining its candidate before updating the cluster; a failure stops the run, and its remedy names the logins left to update. Repeat an interrupted command to finish the same update. A Grafana update writes the new password as a new version of its bound Secret and holds that version in the apply's binding. Refuses removed accounts and external Ceph. Retrieve the updated credentials with cluster info --secrets --name <cluster>. --yes skips ordinary confirmation.", flags: []flagSpec{nameFlag(), loginFlag(), confirmationFlag()}}),
		available(clusterSessionCommand(commandSpec{path: "cluster rsh", validate: validateClusterRsh, example: "--context lab --name storage --node node-1", sshFlags: accountSSHFlags, invoke: Services.invokeClusterRsh, short: "Open an SSH session on a cluster node's Machine"})),
		available(clusterExecCommand()),
	}
}

func clusterSessionCommand(spec commandSpec) commandSpec {
	spec.flags = []flagSpec{nameFlag(), stringFlag("node", "Select a declared node by name, then FQDN (default: the cluster's administrator node)")}
	spec = sessionCommand(spec)
	spec.long += " The session opens on the Machine behind one node of an OpenShiftCluster or managed CephCluster: --node selects" +
		" the node by name, then by FQDN, and defaults to the administrator node that cluster info names." +
		clusterSessionLimits
	return spec
}

const clusterSessionLimits = " For an OpenShiftCluster, the default is its first node in name order, and --node also accepts <role>-<ordinal> with a zero-based ordinal in that order (for example, controlPlane-0 or worker-0). External CephClusters are inapplicable."

func clusterExecCommand() commandSpec {
	spec := sessionCommand(commandSpec{path: "cluster exec", validate: validateClusterExec, example: "--context lab --name storage -- uname -a", sshFlags: accountSSHFlags, invoke: Services.invokeClusterExec, short: "Run a command over SSH on cluster nodes' Machines", payload: true,
		flags: []flagSpec{requiredFlag("name", "Select comma-separated cluster names"),
			stringFlag("node", "Select declared nodes by comma-separated name, then FQDN (default: each cluster's administrator node)")}})
	spec.long += " --name takes a comma-separated list, and each cluster runs the command on the Machine behind its administrator node." +
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

func (s Services) invokeClusterList(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.EnvironmentInspection == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, inspection.ListClustersRequest{
		ContextName: values.text("context"),
	}, s.EnvironmentInspection.ListClusters)
	return commandResult{clusterList: result}, err
}

func (s Services) invokeClusterInfo(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.EnvironmentInspection == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, inspection.ClusterInfoRequest{
		ContextName: values.text("context"),
		Names:       values.names("name"),
		Secrets:     values.boolean("secrets"),
		ObjectUser:  values.text("object-user"),
	}, s.EnvironmentInspection.ClusterInfo)
	return commandResult{clusterInfo: result}, err
}

func (s Services) invokeClusterKeyring(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.EnvironmentInspection == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, inspection.KeyringRequest{
		ContextName: values.text("context"),
		Name:        values.text("name"),
		Client:      values.text("client"),
	}, s.EnvironmentInspection.ClientKeyring)
	return commandResult{keyring: result}, err
}

func (s Services) invokeClusterRsh(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.EnvironmentAccess == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, environmentaccess.ClusterRshRequest{
		ContextName: values.text("context"),
		Name:        values.text("name"),
		Node:        values.text("node"),
		SSH:         values.sshSession(),
	}, s.EnvironmentAccess.ClusterRsh)
	return commandResult{session: result}, err
}

func (s Services) invokeClusterExec(ctx context.Context, values *requestValues, args []string) (commandResult, error) {
	if s.EnvironmentAccess == nil {
		return commandResult{}, errMissingService
	}
	command, err := remoteCommand(values.flags, args)
	if err != nil {
		return commandResult{}, err
	}
	result, err := invokeResult(ctx, values, environmentaccess.ClusterExecRequest{
		ContextName: values.text("context"),
		Names:       values.names("name"),
		Nodes:       values.names("node"),
		SSH:         values.sshSession(),
		Command:     command,
	}, s.EnvironmentAccess.ClusterExec)
	return commandResult{session: result}, err
}
