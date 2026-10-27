package access

import (
	"context"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/environment"
	"github.com/crmarques/bootwright/internal/machine"
)

type Service struct {
	graph     Graph
	sessions  Sessions
	selection CurrentSelection
	members   roster
}

func New(graph Graph, sessions Sessions, selection CurrentSelection) Service {
	return Service{graph: graph, sessions: sessions, selection: selection}
}

func (s Service) WithMembers(members func(api.Object) (environment.Membership, bool)) Service {
	s.members = members
	return s
}

func (s Service) ClusterRsh(ctx context.Context, request ClusterRshRequest) (*machine.SessionResult, error) {
	selected, machines, err := s.resolve(ctx, "cluster rsh", request.ContextName, single(request.Name), single(request.Node))
	if err != nil {
		return nil, err
	}
	return s.sessions.Rsh(ctx, selected, machines[0], request.SSH)
}

func single(value string) []string {
	if value == "" {
		return nil
	}
	return []string{value}
}

func (s Service) ClusterExec(ctx context.Context, request ClusterExecRequest) (*machine.SessionResult, error) {
	selected, machines, err := s.resolve(ctx, "cluster exec", request.ContextName, canonicalNames(request.Names), canonicalNames(request.Nodes))
	if err != nil {
		return nil, err
	}
	return s.sessions.Exec(ctx, selected, machines, request.SSH, request.Command)
}

func (s Service) resolve(ctx context.Context, command, contextName string, names, selectors []string) (string, []string, error) {
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	if s.graph == nil || s.sessions == nil {
		return "", nil, availability.ErrNotImplemented
	}
	selected, err := machine.SelectedContext(ctx, s.selection, contextName)
	if err != nil {
		return "", nil, err
	}
	catalog, err := s.graph.Effective(ctx, selected)
	if err != nil {
		return "", nil, err
	}
	clusters, err := sessionTargets(catalog, command, selected, names)
	if err != nil {
		return "", nil, err
	}
	machines, err := selectMachines(clusters, selected, selectors, s.members)
	if err != nil {
		return "", nil, err
	}
	return selected, machines, nil
}

func sessionTargets(catalog api.Catalog, command, contextName string, names []string) ([]api.Object, error) {
	discovery := "bootwright cluster info --context " + contextName
	if len(names) == 0 {
		return nil, failure("access.target", "no cluster name was supplied", "name one with --name <cluster>; "+discovery+" lists them")
	}
	var unknown []string
	var clusters, containers []api.Object
	for _, name := range names {
		if storage, ok := catalog.Find(api.StorageCluster, name); ok {
			clusters = append(clusters, storage)
		} else if container, ok := catalog.Find(api.ContainerCluster, name); ok {
			containers = append(containers, container)
		} else {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		subject, remedy := "no ContainerCluster or StorageCluster named ", "name a cluster the context selects; "
		if len(unknown) > 1 {
			subject, remedy = "no ContainerClusters or StorageClusters named ", "name clusters the context selects; "
		}
		return nil, failure("access.target", "the context "+contextName+" selects "+subject+strings.Join(unknown, ", "), remedy+discovery+" lists them")
	}
	for _, storage := range clusters {
		if management := storage.Spec().Get("management").Text(); management != "" && management != "managed" {
			return nil, failure("cluster.not-applicable",
				"bootwright "+command+" does not apply to "+storage.Name()+", an "+management+" Ceph StorageCluster; it applies to managed Ceph StorageClusters, whose declared nodes it opens a session on",
				discovery+" lists the clusters this context selects and their kinds")
		}
	}
	if len(containers) > 0 {
		return nil, diagnostics.NewFailure("access.unavailable",
			"bootwright "+command+" is not available for "+containers[0].Name()+", an OpenShift or OKD ContainerCluster: sessions are not yet implemented for that kind, and the cluster is reached through bootwright cluster kubeconfig", "")
	}
	return clusters, nil
}

func failure(code, message, remediation string) error {
	return diagnostics.NewFailureWithRemediation(code, message, "", remediation)
}
