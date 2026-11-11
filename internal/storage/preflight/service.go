package preflight

import (
	"context"
	"maps"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/storage/cephcluster"
	"github.com/crmarques/bootwright/internal/substrate/capacity"
)

type Service struct {
	Graph     Graph
	Ownership Ownership
	Project   Projector
	Writes    capacity.WriteProjector
	Capacity  CapacityChecker
	Guests    GuestObserver
	HostPorts func(api.Catalog) ([]HostPortRequirement, error)
	Selection func(context.Context) (string, error)
}

type PreflightRequest struct {
	ContextName     string
	Clusters        []string
	DryRun          bool
	ObserveHostKeys bool
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
	owned := map[string]machine.OwnershipState{}
	if s.Ownership != nil {
		if owned, err = s.Ownership.Ownership(ctx, contextName); err != nil {
			return nil, err
		}
		for i := range allocations {
			state, found := owned["Machine/"+allocations[i].Machine]
			allocations[i].Allocated = found && !(state.Verb == machine.VerbDestroy && state.State == machine.BlockDone)
		}
	}
	reserves := ReservesFromCatalog(catalog, allocations)
	for i := range allocations {
		allocations[i].Reserve = reserves(Target{Host: allocations[i].Host, Pool: allocations[i].Pool, Context: allocations[i].Context, Local: allocations[i].Local})
	}
	if s.Capacity == nil {
		return nil, availability.ErrNotImplemented
	}
	var writes []Write
	if !dryRun {
		if writes, err = capacity.PendingWrites(ctx, s.Writes, s.Ownership, catalog, contextName, allocations); err != nil {
			return nil, err
		}
	}
	return s.Capacity.Check(ctx, allocations, writes, dryRun)
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
	clusters := catalog.OfKind(api.CephCluster)
	names := slices.Clone(request.Clusters)
	slices.Sort(names)
	names = slices.Compact(names)
	for _, name := range names {
		if !slices.ContainsFunc(clusters, func(object api.Object) bool { return object.Name() == name }) {
			return nil, diagnostics.NewFailureWithRemediation("access.target", "the context selects no CephCluster named "+name, "", "run bootwright cluster list --context "+selected)
		}
	}
	hosts := map[string]bool{}
	fleet := map[string]*FleetAccess{}
	peers := map[string]*PeerNames{}
	result := &Report{Context: selected, Target: "storage-cluster", Checks: []Check{}}
	for _, cluster := range clusters {
		if len(names) > 0 && !slices.Contains(names, cluster.Name()) {
			continue
		}
		if cluster.Spec().Get("management").Text() == "external" {
			result.Checks = append(result.Checks, Check{ID: "cluster.prerequisites", Target: cluster.Name(), Status: capacity.Unknown, Reason: "external cluster prerequisite observation is unavailable", Next: "verify the external cluster through its owner"})
			continue
		}
		for _, node := range cluster.Spec().Get("topology", "nodes").Items() {
			ref := node.Get("machineRef").Text()
			hosts[ref] = true
			if user, provided := cephcluster.ProvidedTiebreakerUser(catalog, cluster, ref); provided {
				access := &FleetAccess{User: user}
				if key, declared := machine.FleetKey(catalog); declared {
					access.KeyRef = key
				}
				fleet[ref] = access
			}
		}
		addPeerNames(peers, cluster)
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
	published := []HostPortRequirement{}
	if s.HostPorts != nil && len(hostNames) > 0 {
		if published, err = s.HostPorts(catalog); err != nil {
			return nil, err
		}
	}
	for _, name := range hostNames {
		ports := slices.DeleteFunc(slices.Clone(published), func(requirement HostPortRequirement) bool { return requirement.Machine != name })
		checks, err := s.guestChecks(ctx, selected, catalog, name, owned, HostRequirements{Ports: ports, Fleet: fleet[name], Peers: peers[name]}, request)
		if err != nil {
			return nil, err
		}
		result.Checks = append(result.Checks, checks...)
	}

	slices.SortFunc(result.Checks, func(a, b Check) int { return strings.Compare(a.Target+"/"+a.ID, b.Target+"/"+b.ID) })
	summarize(result)
	if result.Summary.Failed+result.Summary.Unknown+result.Summary.Pending > 0 {
		code := "preflight.unknown"
		if result.Summary.Failed > 0 {
			code = "preflight.failed"
		}
		return result, diagnostics.NewFailureWithRemediation(code, "storage readiness is not proved; inspect the failed, unknown and pending checks", "", "resolve the reported prerequisites and repeat bootwright preflight --stage clusters")
	}
	return result, nil
}

var nameAddressingServices = []string{"alertmanager", "alloy", "grafana", "loki", "mgmt-gateway", "prometheus", "promtail"}

func addPeerNames(peers map[string]*PeerNames, cluster api.Object) {
	machines, names := map[string]string{}, []string{}
	for _, node := range cluster.Spec().Get("topology", "nodes").Items() {
		machines[node.Get("name").Text()] = node.Get("machineRef").Text()
		names = append(names, cephcluster.NativeHostname(node))
	}
	placed := map[string][]string{}
	addressing := false
	place := func(service api.Value, label string) {
		nodes, issues := cephcluster.ResolveServicePlacement(cluster, service)
		if len(issues) != 0 || len(nodes) == 0 {
			nodes = slices.Collect(maps.Keys(machines))
		}
		for _, node := range nodes {
			if machine, found := machines[node]; found {
				placed[machine] = append(placed[machine], label)
			}
		}
	}
	for _, service := range cluster.Spec().Get("services").Items() {
		if typeName := service.Get("service_type").Text(); slices.Contains(nameAddressingServices, typeName) {
			addressing = true
			place(service, typeName)
		}
	}
	if !addressing {
		return
	}
	managers := false
	for _, service := range cluster.Spec().Get("services").Items() {
		if service.Get("service_type").Text() == "mgr" {
			managers = true
			place(service, "mgr")
		}
	}
	if !managers {
		place(api.Value{}, "mgr")
	}
	for machine, services := range placed {
		entry := peers[machine]
		if entry == nil {
			entry = &PeerNames{}
			peers[machine] = entry
		}
		entry.Services = append(entry.Services, services...)
		entry.Names = append(entry.Names, names...)
		slices.Sort(entry.Services)
		entry.Services = slices.Compact(entry.Services)
		slices.Sort(entry.Names)
		entry.Names = slices.Compact(entry.Names)
	}
}

func (s Service) guestChecks(ctx context.Context, selected string, catalog api.Catalog, name string, owned map[string]machine.OwnershipState, requirements HostRequirements, request PreflightRequest) ([]Check, error) {
	host, found := catalog.Find(api.Machine, name)
	check := Check{ID: "host.prerequisites", Target: name, Status: capacity.Unknown, Next: "prove this Machine's installed OS and prerequisites before Ceph enrollment"}
	state, recorded := owned["Machine/"+name]
	provided := host.Spec().Get("os", "provided").Bool()
	switch {
	case !found:
		check.Reason = "the declared Machine is missing"
	case !provided && s.Ownership != nil && (!recorded || state.Verb == machine.VerbApply && state.State == machine.BlockPending):
		check.Status = capacity.Pending
		check.Reason = "the Machine has not been installed"
		check.Next = "run bootwright apply --context " + selected + " --stage machines"
	case !provided && (!recorded || !state.Realized()):
		check.Reason = "the Machine's installation has not been proved complete"
	case request.DryRun:
		check.Reason = "live guest prerequisites were not observed in dry-run"
	case s.Guests == nil:
		check.Reason = "live guest prerequisite observation is unavailable"
	default:
		probe, cancel := context.WithTimeout(ctx, capacity.ProbeTimeout)
		checks, err := s.Guests.CheckGuest(probe, selected, host, requirements, request)
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
