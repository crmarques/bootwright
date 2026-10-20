package preflight

import (
	"context"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
)

type Service struct {
	Graph     Graph
	Ownership Ownership
	Project   Projector
	Capacity  CapacityChecker
	Guests    GuestObserver
	Selection func(context.Context) (string, error)
}

type PreflightRequest struct {
	ContextName     string
	Clusters        []string
	DryRun          bool
	TrustOnFirstUse bool
	SSH             machine.SSHOptions
}

func (s Service) CheckCapacity(ctx context.Context, catalog api.Catalog, contextName string, dryRun bool) ([]Check, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.Project == nil {
		return nil, availability.ErrNotImplemented
	}
	allocations, err := s.Project(catalog, contextName)
	if err != nil {
		return nil, err
	}
	if s.Ownership != nil {
		owned, err := s.Ownership.Ownership(ctx, contextName)
		if err != nil {
			return nil, err
		}
		for i := range allocations {
			state, found := owned["Machine/"+allocations[i].Machine]
			allocations[i].Allocated = found && !(state.Verb == machine.VerbDestroy && state.State == machine.BlockDone)
		}
	}
	reserves := ReservesFromCatalog(catalog, allocations)
	for i := range allocations {
		allocations[i].Reserve = reserves(Target{Host: allocations[i].Host, Pool: allocations[i].Pool, Local: allocations[i].Local})
	}
	if s.Capacity == nil {
		return nil, availability.ErrNotImplemented
	}
	return s.Capacity.Check(ctx, allocations, dryRun)
}

func (s Service) Check(ctx context.Context, request PreflightRequest) (*Report, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.Graph == nil || s.Project == nil {
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
	clusters := catalog.OfKind(api.StorageCluster)
	names := slices.Clone(request.Clusters)
	slices.Sort(names)
	names = slices.Compact(names)
	for _, name := range names {
		if !slices.ContainsFunc(clusters, func(object api.Object) bool { return object.Name() == name }) {
			return nil, diagnostics.NewFailureWithRemediation("access.target", "the context selects no StorageCluster named "+name, "", "run bootwright cluster list --context "+selected)
		}
	}
	hosts := map[string]bool{}
	result := &Report{Context: selected, Target: "storage-cluster", Checks: []Check{}}
	for _, cluster := range clusters {
		if len(names) > 0 && !slices.Contains(names, cluster.Name()) {
			continue
		}
		if cluster.Spec().Get("management").Text() == "external" {
			result.Checks = append(result.Checks, Check{ID: "cluster.prerequisites", Target: cluster.Name(), Status: Unknown, Reason: "external cluster prerequisite observation is unavailable", Next: "verify the external cluster through its owner"})
			continue
		}
		for _, node := range cluster.Spec().Get("ceph", "topology", "nodes").Items() {
			hosts[node.Get("machineRef").Text()] = true
		}
	}
	if len(hosts) > 0 {
		capacity, err := s.CheckCapacity(ctx, catalog, selected, request.DryRun)
		if err != nil {
			return nil, err
		}
		result.Checks = append(result.Checks, capacity...)
	}
	var owned map[string]machine.OwnershipState
	if s.Ownership != nil {
		owned, err = s.Ownership.Ownership(ctx, selected)
		if err != nil {
			return nil, err
		}
	}
	hostNames := make([]string, 0, len(hosts))
	for host := range hosts {
		hostNames = append(hostNames, host)
	}
	slices.Sort(hostNames)
	for _, name := range hostNames {
		checks, err := s.guestChecks(ctx, selected, catalog, name, owned, request)
		if err != nil {
			return nil, err
		}
		result.Checks = append(result.Checks, checks...)
	}

	slices.SortFunc(result.Checks, func(a, b Check) int { return strings.Compare(a.Target+"/"+a.ID, b.Target+"/"+b.ID) })
	result.summarize()
	if result.Summary.Failed+result.Summary.Unknown+result.Summary.Pending > 0 {
		code := "preflight.unknown"
		if result.Summary.Failed > 0 {
			code = "preflight.failed"
		}
		return result, diagnostics.NewFailureWithRemediation(code, "storage readiness is not proved; inspect the failed, unknown and pending checks", "", "resolve the reported prerequisites and repeat preflight storage-cluster")
	}
	return result, nil
}

func (s Service) guestChecks(ctx context.Context, selected string, catalog api.Catalog, name string, owned map[string]machine.OwnershipState, request PreflightRequest) ([]Check, error) {
	host, found := catalog.Find(api.Machine, name)
	check := Check{ID: "host.prerequisites", Target: name, Status: Unknown, Next: "prove this Machine's installed OS and prerequisites before Ceph enrollment"}
	state, recorded := owned["Machine/"+name]
	provided := host.Spec().Get("os", "provided").Bool()
	switch {
	case !found:
		check.Reason = "the declared Machine is missing"
	case !provided && s.Ownership != nil && (!recorded || state.Verb == machine.VerbApply && state.State == machine.BlockPending):
		check.Status = Pending
		check.Reason = "the Machine has not been installed"
		check.Next = "run bootwright apply --context " + selected + " --stage machines"
	case !provided && (!recorded || !state.Realized()):
		check.Reason = "the Machine's installation has not been proved complete"
	case request.DryRun:
		check.Reason = "live guest prerequisites were not observed in dry-run"
	case s.Guests == nil:
		check.Reason = "live guest prerequisite observation is unavailable"
	default:
		probe, cancel := context.WithTimeout(ctx, ProbeTimeout)
		checks, err := s.Guests.CheckGuest(probe, selected, host, request)
		expired := probe.Err()
		cancel()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err != nil || expired != nil || len(checks) == 0 {
			check.Reason = "the bounded guest prerequisite observation did not complete"
		} else {
			return checks, nil
		}
	}
	return []Check{check}, nil
}
