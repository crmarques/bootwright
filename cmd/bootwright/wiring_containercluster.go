package main

import (
	"context"

	"github.com/crmarques/bootwright/internal/cli"
	containeraccess "github.com/crmarques/bootwright/internal/containercluster/access"
	"github.com/crmarques/bootwright/internal/containercluster/agentinstall"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// wireClusterAccess binds cluster access to the compiled graph a name resolves
// in and to the custody the install block's proved completion fills. Only the
// kubeconfig export is available; oc and kubectl stay unavailable.
func wireClusterAccess(state containeraccess.EffectiveState, binder *custody.Service, selection contexts.SelectionStore) cli.ClusterAccessService {
	return containeraccess.New(state, producedReader{binder: binder}, installAccess, currentSelection(selection))
}

func installAccess(cluster string) (string, string) {
	return agentinstall.InstallBlockID(cluster), agentinstall.KubeconfigOutput
}

type producedReader struct{ binder *custody.Service }

func (r producedReader) ReadProduced(ctx context.Context, contextName, block, name string) (secrets.Material, bool, error) {
	return r.binder.ReadProduced(ctx, custody.ReadProducedRequest{ContextName: contextName, Block: block, Name: name})
}
