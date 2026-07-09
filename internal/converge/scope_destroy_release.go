package converge

import (
	"fmt"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/internal/ownership"
	"github.com/crmarques/bootwright/internal/workspace"
)

const ownershipInfraComponentKind = "infra-component"

const InfraComponentReleaseExtraVar = "bootwright_infra_component_release_records"

type InfraComponentRelease struct {
	Name          string
	Host          string
	ComponentKind string
}

type InfraComponentReferencedOwner struct {
	Name          string
	Host          string
	ComponentKind string
	Referrers     []string
}

type ReleaseDecision struct {
	Releases []InfraComponentRelease
	Blocks   []InfraComponentReferencedOwner
	Warnings []error
}

func (d ReleaseDecision) Names() []string {
	names := make([]string, 0, len(d.Releases))
	for _, release := range d.Releases {
		names = append(names, release.Name)
	}
	return names
}

func PlanInfraComponentReleases(selfContext string, records []ownership.ResourceRecord) (ReleaseDecision, error) {
	stores, err := siblingContextStores(selfContext)
	if err != nil {
		return ReleaseDecision{}, err
	}
	var decision ReleaseDecision
	for _, record := range records {
		if strings.TrimSpace(record.Kind) != ownershipInfraComponentKind {
			continue
		}
		componentKind := strings.TrimSpace(record.Labels["bootwright.kind"])
		if record.IsReference() {
			decision.Releases = append(decision.Releases, InfraComponentRelease{
				Name:          record.Name,
				Host:          record.Host,
				ComponentKind: componentKind,
			})
			continue
		}
		if len(stores) == 0 {
			continue
		}
		id := ownership.SharedComponentID{
			Kind: record.Kind,
			Name: record.Name,
			Host: record.Host,
		}
		referrers, skipped := ownership.ReferenceContexts(stores, selfContext, id)
		decision.Warnings = append(decision.Warnings, skipped...)
		coOwners, coSkipped := ownership.OtherContextsWithRole(stores, selfContext, id, ownership.RoleOwner)
		decision.Warnings = append(decision.Warnings, coSkipped...)
		blockers := mergeUniqueSorted(referrers, coOwners)
		if len(blockers) > 0 {
			decision.Blocks = append(decision.Blocks, InfraComponentReferencedOwner{
				Name:          record.Name,
				Host:          record.Host,
				ComponentKind: componentKind,
				Referrers:     blockers,
			})
		}
	}
	return decision, nil
}

func ReferencedOwnerError(blocks []InfraComponentReferencedOwner) error {
	if len(blocks) == 0 {
		return nil
	}
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		parts = append(parts, fmt.Sprintf("%s (referrers: %s)", block.Name, strings.Join(block.Referrers, ", ")))
	}
	return fmt.Errorf("refusing to tear down shared infra-component(s) still referenced or co-owned by other contexts: %s; detach the other contexts first, or re-run with --override to tear them down regardless", strings.Join(parts, "; "))
}

func ApplyInfraComponentReleaseExtraVar(plan *WorkflowPlan, names []string) {
	if len(names) == 0 {
		return
	}
	plan.ExtraVarPairs = append(plan.ExtraVarPairs, InfraComponentReleaseExtraVar+"="+strings.Join(names, ","))
}

func mergeUniqueSorted(a, b []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(a)+len(b))
	for _, list := range [][]string{a, b} {
		for _, name := range list {
			name = strings.TrimSpace(name)
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func siblingContextStores(selfContext string) ([]ownership.ContextStore, error) {
	contexts, err := workspace.ListContexts()
	if err != nil {
		return nil, err
	}
	self := strings.TrimSpace(selfContext)
	stores := make([]ownership.ContextStore, 0, len(contexts))
	for _, ctx := range contexts {
		if strings.TrimSpace(ctx.Name) == self {
			continue
		}
		stores = append(stores, ownership.ContextStore{Context: ctx.Name, Dir: ctx.OwnershipDir})
	}
	return stores, nil
}
