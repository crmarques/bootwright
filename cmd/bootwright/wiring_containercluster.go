package main

import (
	"context"
	"errors"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/cli"
	containeraccess "github.com/crmarques/bootwright/internal/containercluster/access"
	"github.com/crmarques/bootwright/internal/containercluster/agentinstall"
	"github.com/crmarques/bootwright/internal/controller/evidence"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets/custody"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// wireClusterAccess binds cluster access to the compiled graph a name resolves
// in and to the custody the install block's proved completion fills. Client
// access comes from the proved install and retained controller client closure.
func wireClusterAccess(state containeraccess.EffectiveState, binder *custody.Service, selection contexts.SelectionStore, reconciler lifecycle.Service) cli.ClusterAccessService {
	return containeraccess.New(state, producedReader{binder: binder}, installAccess, currentSelection(selection)).WithMetadata(clusterClientMetadata{reconciler: reconciler, custody: binder})
}

func installAccess(cluster string) (string, string) {
	return agentinstall.InstallBlockID(cluster), agentinstall.KubeconfigOutput
}

type producedReader struct{ binder *custody.Service }

func (r producedReader) ReadProduced(ctx context.Context, contextName, block, name string) (containeraccess.Custodied, bool, error) {
	read, found, err := r.binder.ReadProduced(ctx, custody.ReadProducedRequest{ContextName: contextName, Block: block, Name: name})
	return containeraccess.Custodied{Material: read.Material, Unproved: read.Unproved}, found, err
}

type clusterClientReader interface {
	evidenceReader
	AccessTool(context.Context, string, evidence.InstalledTool) (string, error)
}

type producedAvailability interface {
	ProducedAvailable(context.Context, custody.ReadProducedRequest) (bool, error)
}

type clusterClientMetadata struct {
	reconciler clusterClientReader
	custody    producedAvailability
}

func (m clusterClientMetadata) Client(ctx context.Context, contextName, cluster, executable string) (containeraccess.Client, error) {
	records, err := m.reconciler.Evidence(ctx, contextName, string(api.OpenShiftCluster), cluster)
	if err != nil {
		return containeraccess.Client{}, err
	}
	tool, found, err := agentinstall.InstalledClient(records, contextName, cluster, executable)
	if err != nil {
		return containeraccess.Client{}, err
	}
	if !found || m.custody == nil {
		return containeraccess.Client{}, containeraccess.UnavailableCluster(contextName, cluster)
	}
	present, err := m.custody.ProducedAvailable(ctx, custody.ReadProducedRequest{ContextName: contextName, Block: agentinstall.InstallBlockID(cluster), Name: agentinstall.KubeconfigOutput})
	if err != nil {
		return containeraccess.Client{}, err
	}
	if !present {
		return containeraccess.Client{}, containeraccess.UnavailableCluster(contextName, cluster)
	}
	path, err := m.reconciler.AccessTool(ctx, contextName, tool)
	if err != nil {
		return containeraccess.Client{}, err
	}
	return containeraccess.Client{Executable: path, Version: tool.Version, Distribution: tool.Compatibility}, nil
}

func (m clusterClientMetadata) ClientReason(ctx context.Context, contextName, cluster, executable string) (string, error) {
	_, err := m.Client(ctx, contextName, cluster, executable)
	if err == nil {
		return "", nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "", err
	}
	details := diagnostics.Of(err)
	if len(details) != 1 {
		return "", err
	}
	return strings.TrimSpace(details[0].Message + "; " + details[0].Remediation), nil
}
