package clusterpreflight

import (
	"context"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/availability"
	containerpreflight "github.com/crmarques/bootwright/internal/containercluster/preflight"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	storagepreflight "github.com/crmarques/bootwright/internal/storage/preflight"
)

type Service struct {
	Graph     storagepreflight.Graph
	Selection func(context.Context) (string, error)
	Container ContainerChecker
	Storage   StorageChecker
}

func (s Service) Check(ctx context.Context, request PreflightRequest) (*Report, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.Graph == nil || s.Container == nil || s.Storage == nil {
		return nil, availability.ErrNotImplemented
	}
	selected, err := machine.SelectedContext(ctx, s.Selection, request.ContextName)
	if err != nil {
		return nil, err
	}
	catalog, err := s.Graph.Effective(ctx, selected)
	if err != nil {
		return nil, err
	}
	containerNames, storageNames, err := splitSelection(catalog, selected, request.Clusters)
	if err != nil {
		return nil, err
	}
	all := len(request.Clusters) == 0
	runContainer := all || len(containerNames) > 0
	storageSelected := len(storageNames) > 0 || all && len(catalog.OfCategory(api.StorageClusterCategory)) > 0
	if err := RefuseUnreachedSSH(request.SSH, storageSelected); err != nil {
		return nil, err
	}
	runStorage := storageSelected || all
	var containerPart, storagePart Part
	if runContainer {
		containerPart = func(checks context.Context) (*Report, error) {
			return s.Container.Check(checks, containerpreflight.PreflightRequest{ContextName: selected, Clusters: containerNames, DryRun: request.DryRun})
		}
	}
	if runStorage {
		storagePart = func(checks context.Context) (*Report, error) {
			return s.Storage.Check(checks, storagepreflight.PreflightRequest{ContextName: selected, Clusters: storageNames, DryRun: request.DryRun, TrustOnFirstUse: request.TrustOnFirstUse, SSH: request.SSH})
		}
	}
	reports, err := RunTogether(ctx, containerPart, storagePart)
	if err != nil {
		return nil, err
	}
	return Assemble(selected, "clusters", "cluster", "clusters", reports...)
}

func RefuseUnreachedSSH(ssh machine.SSHOptions, storageSelected bool) error {
	if ssh.Borrowed() && !storageSelected {
		return diagnostics.NewFailureWithRemediation("cli.usage", "no storage cluster is selected, so SSH overrides reach nothing; the container-cluster checks observe the controller and accept none", "", "repeat without the SSH overrides, or select a storage cluster")
	}
	return nil
}

func splitSelection(catalog api.Catalog, selected string, requested []string) ([]string, []string, error) {
	names := slices.Clone(requested)
	slices.Sort(names)
	names = slices.Compact(names)
	var container, storage []string
	for _, name := range names {
		switch {
		case hasCluster(catalog, api.ContainerClusterCategory, name):
			container = append(container, name)
		case hasCluster(catalog, api.StorageClusterCategory, name):
			storage = append(storage, name)
		default:
			return nil, nil, diagnostics.NewFailureWithRemediation("access.target", "the context selects no container or storage cluster named "+name, "", "run bootwright cluster list --context "+selected)
		}
	}
	return container, storage, nil
}

func hasCluster(catalog api.Catalog, category api.Category, name string) bool {
	_, found := catalog.FindInCategory(category, name)
	return found
}
