package converge

import (
	"fmt"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/ownership"
	stategraph "github.com/crmarques/bootwright/internal/state/graph"
	"github.com/crmarques/bootwright/internal/workspace"
)

const ownershipInfraComponentKind = "infra-component"

const artifactServerRecordKindLabel = "artifacts"

type InfraComponentDestroyBlock struct {
	Name          string
	Host          string
	ComponentKind string
	Contexts      []string
	Unrecorded    bool
}

type InfraComponentDestroyDecision struct {
	Blocks   []InfraComponentDestroyBlock
	Warnings []error
}

func PlanInfraComponentDestroyBlocks(selfContext string, state v1alpha1.State, records []ownership.ResourceRecord, artifactServerOnly bool) (InfraComponentDestroyDecision, error) {
	stores, err := siblingContextStores(selfContext)
	if err != nil {
		return InfraComponentDestroyDecision{}, err
	}
	var decision InfraComponentDestroyDecision
	ownNames := map[string]bool{}
	for _, record := range records {
		if strings.TrimSpace(record.Kind) != ownershipInfraComponentKind {
			continue
		}
		componentKind := strings.TrimSpace(record.Labels["bootwright.kind"])
		if artifactServerOnly && componentKind != artifactServerRecordKindLabel {
			continue
		}
		ownNames[strings.TrimSpace(record.Name)] = true
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
			decision.Blocks = append(decision.Blocks, InfraComponentDestroyBlock{
				Name:          record.Name,
				Host:          record.Host,
				ComponentKind: componentKind,
				Contexts:      blockers,
			})
		}
	}
	if len(stores) > 0 {
		for _, service := range stategraph.ResolveMachineServices(state).Services {
			if !service.IsInfraComponentService() {
				continue
			}
			if artifactServerOnly && service.Identity.Kind != v1alpha1.ComponentSlotArtifactServer {
				continue
			}
			name := strings.TrimSpace(service.Identity.Name)
			if name == "" || ownNames[name] {
				continue
			}
			owners, skipped := ownership.OwnerContextsForComponent(stores, selfContext, ownershipInfraComponentKind, name)
			decision.Warnings = append(decision.Warnings, skipped...)
			if len(owners) > 0 {
				decision.Blocks = append(decision.Blocks, InfraComponentDestroyBlock{
					Name:          name,
					ComponentKind: service.Identity.Kind,
					Contexts:      owners,
					Unrecorded:    true,
				})
			}
		}
	}
	decision.Warnings = dedupeErrors(decision.Warnings)
	sort.SliceStable(decision.Blocks, func(i, j int) bool { return decision.Blocks[i].Name < decision.Blocks[j].Name })
	return decision, nil
}

func InfraComponentDestroyBlockError(blocks []InfraComponentDestroyBlock) error {
	if len(blocks) == 0 {
		return nil
	}
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		detail := "co-owned or referenced by"
		if block.Unrecorded {
			detail = "owned (with no ownership record in this context) by"
		}
		parts = append(parts, fmt.Sprintf("%s (%s context(s): %s)", block.Name, detail, strings.Join(block.Contexts, ", ")))
	}
	return fmt.Errorf("refusing to tear down shared infra-component(s) that other contexts own or reference: %s; destroy or detach them from the owning context first, or re-run with --override to tear them down regardless", strings.Join(parts, "; "))
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

func dedupeErrors(in []error) []error {
	seen := map[string]bool{}
	out := make([]error, 0, len(in))
	for _, err := range in {
		if err == nil || seen[err.Error()] {
			continue
		}
		seen[err.Error()] = true
		out = append(out, err)
	}
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
