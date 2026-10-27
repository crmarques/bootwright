package clusterpreflight

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/availability"
	containerpreflight "github.com/crmarques/bootwright/internal/containercluster/preflight"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	storagepreflight "github.com/crmarques/bootwright/internal/storage/preflight"
	"github.com/crmarques/bootwright/internal/substrate/capacity"
)

type Service struct {
	Graph     storagepreflight.Graph
	Selection func(context.Context) (string, error)
	Container ContainerChecker
	Storage   StorageChecker
}

var errSiblingStopped = errors.New("the other cluster checks stopped")

type half struct {
	report *Report
	err    error
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
	if request.SSH.Borrowed() && !storageSelected {
		return nil, diagnostics.NewFailureWithRemediation("cli.usage", "preflight clusters selects no storage cluster, so SSH overrides reach nothing; the container-cluster checks observe the controller and accept none", "", "repeat without the SSH overrides, or select a storage cluster")
	}
	runStorage := storageSelected || all
	var container, storage half
	var group sync.WaitGroup
	checks, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	stop := func(part *half) {
		if part.err != nil && part.report == nil {
			cancel(errSiblingStopped)
		}
	}
	if runContainer {
		group.Go(func() {
			container.report, container.err = s.Container.Check(checks, containerpreflight.PreflightRequest{ContextName: selected, Clusters: containerNames, DryRun: request.DryRun})
			stop(&container)
		})
	}
	if runStorage {
		group.Go(func() {
			storage.report, storage.err = s.Storage.Check(checks, storagepreflight.PreflightRequest{ContextName: selected, Clusters: storageNames, DryRun: request.DryRun, TrustOnFirstUse: request.TrustOnFirstUse, SSH: request.SSH})
			stop(&storage)
		})
	}
	group.Wait()
	for _, part := range []half{container, storage} {
		if part.err != nil && part.report == nil && !(errors.Is(part.err, context.Canceled) && context.Cause(checks) == errSiblingStopped && ctx.Err() == nil) {
			return nil, part.err
		}
	}
	result := &Report{Context: selected, Target: "clusters", Checks: []capacity.Check{}}
	for _, part := range []half{container, storage} {
		if part.report != nil {
			result.Checks = mergeChecks(result.Checks, part.report.Checks)
		}
	}
	slices.SortFunc(result.Checks, func(a, b capacity.Check) int { return strings.Compare(a.Target+"/"+a.ID, b.Target+"/"+b.ID) })
	for _, check := range result.Checks {
		switch check.Status {
		case capacity.Passed:
			result.Summary.Passed++
		case capacity.Failed:
			result.Summary.Failed++
		case capacity.Pending:
			result.Summary.Pending++
		default:
			result.Summary.Unknown++
		}
	}
	if result.Summary.Failed+result.Summary.Unknown+result.Summary.Pending > 0 {
		code := "preflight.unknown"
		if result.Summary.Failed > 0 {
			code = "preflight.failed"
		}
		return result, diagnostics.NewFailureWithRemediation(code, "cluster readiness is not proved; inspect the failed, unknown and pending checks", "", "resolve the reported prerequisites and repeat preflight clusters")
	}
	return result, nil
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

func mergeChecks(merged, added []capacity.Check) []capacity.Check {
	for _, check := range added {
		index := slices.IndexFunc(merged, func(held capacity.Check) bool { return held.Target == check.Target && held.ID == check.ID })
		switch {
		case index < 0:
			merged = append(merged, check)
		case severity(check.Status) > severity(merged[index].Status):
			merged[index] = check
		}
	}
	return merged
}

func severity(status string) int {
	switch status {
	case capacity.Passed:
		return 0
	case capacity.Pending:
		return 1
	case capacity.Failed:
		return 3
	default:
		return 2
	}
}
