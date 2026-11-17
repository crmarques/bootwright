package cephcluster

import (
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

type placements struct {
	cluster  api.Object
	resolved []resolvedPlacement
	members  map[string]map[string]bool
}

type resolvedPlacement struct {
	placement api.Value
	names     []string
	issues    []api.Issue
}

func newPlacements(cluster api.Object) *placements {
	return &placements{cluster: cluster, members: map[string]map[string]bool{}}
}

func (p *placements) resolve(service api.Value) ([]string, []api.Issue) {
	placement := service.Get("placement")
	for _, known := range p.resolved {
		if known.placement.Equal(placement) {
			return slices.Clone(known.names), slices.Clone(known.issues)
		}
	}
	names, issues := ResolveServicePlacement(p.cluster, service)
	p.resolved = append(p.resolved, resolvedPlacement{placement: placement, names: slices.Clone(names), issues: slices.Clone(issues)})
	return names, issues
}

func (p *placements) member(serviceType, nodeName string) bool {
	members, known := p.members[serviceType]
	if !known {
		members = map[string]bool{}
		for _, service := range p.cluster.Spec().Get("services").Items() {
			if textOf(service, "service_type") != serviceType {
				continue
			}
			if names, issues := p.resolve(service); len(issues) == 0 {
				for _, name := range names {
					members[name] = true
				}
			}
		}
		p.members[serviceType] = members
	}
	return members[nodeName]
}

func (a *admission) placements() *placements {
	if a.placed == nil {
		a.placed = newPlacements(a.object)
	}
	return a.placed
}

func (a *admission) placementsOf(cluster api.Object) *placements {
	if sameCluster(a.object, cluster) {
		return a.placements()
	}
	if a.foreign == nil || !sameCluster(a.foreign.cluster, cluster) {
		a.foreign = newPlacements(cluster)
	}
	return a.foreign
}

func sameCluster(held, cluster api.Object) bool {
	return held.Kind() == cluster.Kind() && held.Identity() == cluster.Identity() && held.Spec().Equal(cluster.Spec())
}
