package preflight

import (
	"context"
	"net/netip"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/containercluster/openshift"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/environment"
	"github.com/crmarques/bootwright/internal/machine"
	storage "github.com/crmarques/bootwright/internal/storage/preflight"
	"github.com/crmarques/bootwright/internal/substrate/capacity"
)

type Service struct {
	Graph     storage.Graph
	Selection func(context.Context) (string, error)
	Ownership storage.Ownership
	Project   storage.Projector
	Capacity  capacity.CapacityChecker
	Blocks    func(context.Context, string) (map[string]string, error)
	DNSBlock  func(server string) string
	Resolver  Resolver
}

func (s Service) Check(ctx context.Context, request PreflightRequest) (*Report, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.Graph == nil || s.Project == nil || s.Capacity == nil {
		return nil, availability.ErrNotImplemented
	}
	if request.TrustOnFirstUse || request.SSH.Borrowed() {
		return nil, diagnostics.NewFailureWithRemediation("cli.usage", "container-cluster preflight checks the controller and accepts no SSH or trust-on-first-use options", "", "repeat without --trust-on-first-use or SSH overrides")
	}
	selected, err := machine.SelectedContext(ctx, s.Selection, request.ContextName)
	if err != nil {
		return nil, err
	}
	catalog, err := s.Graph.Effective(ctx, selected)
	if err != nil {
		return nil, err
	}
	clusters := catalog.OfKind(api.OpenShiftCluster)
	names := slices.Clone(request.Clusters)
	slices.Sort(names)
	names = slices.Compact(names)
	for _, name := range names {
		if !slices.ContainsFunc(clusters, func(o api.Object) bool { return o.Name() == name }) {
			return nil, diagnostics.NewFailureWithRemediation("access.target", "the context selects no OpenShiftCluster named "+name, "", "run bootwright cluster list --context "+selected)
		}
	}
	blocks := map[string]string{}
	if !request.DryRun && s.Blocks != nil {
		blocks, err = s.Blocks(ctx, selected)
		if err != nil {
			return nil, err
		}
	}
	result := &Report{Context: selected, Target: "container-cluster", Checks: []capacity.Check{}}
	nodes := map[string]bool{}
	for _, cluster := range clusters {
		if len(names) > 0 && !slices.Contains(names, cluster.Name()) {
			continue
		}
		for _, node := range cluster.Spec().Get("nodes").Items() {
			nodes[node.Get("machineRef").Text()] = true
		}
		dns, pending := clusterDNS(catalog, cluster, blocks, s.DNSBlock)
		for _, endpoint := range openshift.ResolutionNames(catalog, cluster) {
			result.Checks = append(result.Checks, s.resolve(ctx, request, selected, endpoint.Name, endpoint.Address, endpoint.Slot, dns, pending))
		}
	}
	checks, err := s.capacityChecks(ctx, catalog, selected, nodes, request.DryRun)
	if err != nil {
		return nil, err
	}
	result.Checks = append(result.Checks, checks...)
	if err := ctx.Err(); err != nil {
		return nil, err
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
		return result, diagnostics.NewFailureWithRemediation(code, "container-cluster readiness is not proved; inspect the failed, unknown and pending checks", "", "resolve the reported prerequisites and repeat preflight container-cluster")
	}
	return result, nil
}

func (s Service) resolve(ctx context.Context, request PreflightRequest, contextName, name, address, slot, dns string, pending bool) capacity.Check {
	next := "publish " + name + " as " + address + " in the resolver the controller uses"
	if dns != "" {
		next = "configure the controller's resolver route to DNSServer " + dns + "; verify record " + name + " = " + address
	}
	check := capacity.Check{ID: "resolution." + slot, Target: name, Status: capacity.Unknown, Next: next}
	switch {
	case request.DryRun:
		check.Status = capacity.Pending
		check.Reason = "dry-run would resolve " + name + " to " + address
	case pending:
		check.Status = capacity.Pending
		check.Reason = "the managed DNSServer serving " + name + " is not done"
		check.Next = "run bootwright apply --context " + contextName + " --stage infrastructure-services, then " + next
	case s.Resolver == nil:
		check.Reason = "the controller resolver is unavailable"
	default:
		probe, cancel := context.WithTimeout(ctx, capacity.ProbeTimeout)
		data, err := s.Resolver.Read(probe, []string{"getent", "ahosts", name})
		expired := probe.Err()
		cancel()
		answers := []string{}
		valid := true
		for line := range strings.SplitSeq(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 0 {
				continue
			}
			ip, parseErr := netip.ParseAddr(fields[0])
			if parseErr != nil {
				valid = false
				continue
			}
			answer := ip.Unmap().String()
			if !slices.Contains(answers, answer) {
				answers = append(answers, answer)
			}
		}
		slices.Sort(answers)
		expected, parseErr := netip.ParseAddr(address)
		if err == nil && expired == nil && valid && parseErr == nil && len(answers) == 1 && answers[0] == expected.Unmap().String() {
			check.Status = capacity.Passed
			check.Reason = name + " resolves to " + address
			check.Next = ""
		} else {
			check.Status = capacity.Failed
			check.Reason = name + " must resolve only to " + address + "; answers: " + strings.Join(answers, ", ")
			if len(answers) == 0 {
				check.Reason += "none"
			}
		}
	}
	return check
}

func clusterDNS(catalog api.Catalog, cluster api.Object, blocks map[string]string, dnsBlock func(string) string) (string, bool) {
	for _, node := range cluster.Spec().Get("nodes").Items() {
		host, found := catalog.Find(api.Machine, node.Get("machineRef").Text())
		if !found {
			continue
		}
		configuration, _ := machine.NetworkConfig(host, catalog)
		for _, selection := range configuration.Get("dns").Items() {
			server, found := catalog.Find(api.DNSServer, selection.Get("serverRef").Text())
			if !found || server.Spec().Get("management").Text() != "managed" {
				continue
			}
			address := ""
			for _, endpoint := range server.Spec().Get("endpoints").Items() {
				if endpoint.Get("name").Text() == selection.Get("endpointRef").Text() {
					address = endpoint.Get("address").Text()
					if address == "" {
						placement, _ := catalog.Find(api.Machine, server.Spec().Get("machineRef").Text())
						address, _ = machine.Address(placement, endpoint.Get("addressRef").Text())
					}
				}
			}
			if address == "" {
				address = server.Spec().Get("bindAddress").Text()
			}
			return server.Name() + " at " + address + " for " + cluster.Name() + "." + environment.ContainerClusterDomain(catalog), dnsBlock == nil || blocks[dnsBlock(server.Name())] != "done"
		}
	}
	return "", false
}

func (s Service) capacityChecks(ctx context.Context, catalog api.Catalog, selected string, nodes map[string]bool, dryRun bool) ([]capacity.Check, error) {
	allocations, err := s.Project(catalog, selected)
	if err != nil {
		return nil, err
	}
	hosts := map[string]bool{}
	for _, a := range allocations {
		if nodes[a.Machine] {
			hosts[a.Host] = true
		}
	}
	allocations = slices.DeleteFunc(allocations, func(a capacity.Allocation) bool { return !hosts[a.Host] })
	if len(allocations) > 0 {
		if s.Ownership != nil {
			owned, err := s.Ownership.Ownership(ctx, selected)
			if err != nil {
				return nil, err
			}
			for i := range allocations {
				state, found := owned["Machine/"+allocations[i].Machine]
				allocations[i].Allocated = found && !(state.Verb == machine.VerbDestroy && state.State == machine.BlockDone)
			}
		}
		reserves := capacity.ReservesFromCatalog(catalog, allocations)
		for i := range allocations {
			a := &allocations[i]
			a.Reserve = reserves(capacity.Target{Host: a.Host, Pool: a.Pool, Local: a.Local})
		}
		checks, err := s.Capacity.Check(ctx, allocations, dryRun)
		if err != nil {
			return nil, err
		}
		if dryRun {
			for i := range checks {
				checks[i].Status = capacity.Pending
				checks[i].Reason = "dry-run would check " + checks[i].ID + " on " + checks[i].Target
			}
		}
		return checks, nil
	}
	return nil, nil
}
