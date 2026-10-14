package access

import (
	"context"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
)

type Service struct {
	graph     Graph
	sessions  Sessions
	selection CurrentSelection
}

func New(graph Graph, sessions Sessions, selection CurrentSelection) Service {
	return Service{graph: graph, sessions: sessions, selection: selection}
}

func (s Service) ClusterRsh(ctx context.Context, request ClusterRshRequest) (*machine.SessionResult, error) {
	selected, node, err := s.resolve(ctx, "cluster rsh", request.ContextName, request.Name, request.Node)
	if err != nil {
		return nil, err
	}
	return s.sessions.Rsh(ctx, selected, node, request.SSH)
}

func (s Service) ClusterExec(ctx context.Context, request ClusterExecRequest) (*machine.SessionResult, error) {
	selected, node, err := s.resolve(ctx, "cluster exec", request.ContextName, request.Name, request.Node)
	if err != nil {
		return nil, err
	}
	return s.sessions.Exec(ctx, selected, node, request.SSH, request.Command)
}

func (s Service) resolve(ctx context.Context, command, contextName, name, selector string) (string, string, error) {
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	if s.graph == nil || s.sessions == nil {
		return "", "", availability.ErrNotImplemented
	}
	selected, err := machine.SelectedContext(ctx, s.selection, contextName)
	if err != nil {
		return "", "", err
	}
	catalog, err := s.graph.Effective(ctx, selected)
	if err != nil {
		return "", "", err
	}
	cluster, err := sessionTarget(catalog, command, selected, name)
	if err != nil {
		return "", "", err
	}
	node, err := selectNode(cluster, selected, selector)
	if err != nil {
		return "", "", err
	}
	return selected, node.Get("machineRef").Text(), nil
}

func sessionTarget(catalog api.Catalog, command, contextName, name string) (api.Object, error) {
	discovery := "bootwright cluster info --context " + contextName
	if name == "" {
		return api.Object{}, failure("access.target", "no cluster name was supplied", "name one with --name <cluster>; "+discovery+" lists them")
	}
	if storage, ok := catalog.Find(api.StorageCluster, name); ok {
		if management := storage.Spec().Get("management").Text(); management != "" && management != "managed" {
			return api.Object{}, failure("cluster.not-applicable",
				"bootwright "+command+" does not apply to "+name+", an "+management+" Ceph StorageCluster; it applies to managed Ceph StorageClusters, whose declared nodes it opens a session on",
				discovery+" lists the clusters this context selects and their kinds")
		}
		return storage, nil
	}
	if _, ok := catalog.Find(api.ContainerCluster, name); ok {
		return api.Object{}, diagnostics.NewFailure("access.unavailable",
			"bootwright "+command+" is not available for "+name+", an OpenShift or OKD ContainerCluster: sessions are not yet implemented for that kind, and the cluster is reached through bootwright cluster kubeconfig", "")
	}
	return api.Object{}, failure("access.target", "the context "+contextName+" selects no ContainerCluster or StorageCluster named "+name,
		"name a cluster the context selects; "+discovery+" lists them")
}

func failure(code, message, remediation string) error {
	return diagnostics.NewFailureWithRemediation(code, message, "", remediation)
}
