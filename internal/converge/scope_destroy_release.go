package converge

import (
	"fmt"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/internal/ownership"
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

type InfraComponentServiceRef struct {
	Name             string
	Kind             string
	Host             string
	SelectionDigests []string
	ClaimDigests     []string
}

func PlanInfraComponentDestroyBlocks(selfContext string, services []InfraComponentServiceRef, records []ownership.ResourceRecord, artifactServerOnly bool) (InfraComponentDestroyDecision, error) {
	stores, err := siblingContextStores(selfContext)
	if err != nil {
		return InfraComponentDestroyDecision{}, err
	}
	var decision InfraComponentDestroyDecision
	ownIDs := map[ownership.SharedComponentID]bool{}
	referenceIDs := map[ownership.SharedComponentID]bool{}
	componentKinds := map[ownership.SharedComponentID]string{}
	var ids []ownership.SharedComponentID
	for _, record := range records {
		if strings.TrimSpace(record.Kind) != ownershipInfraComponentKind {
			continue
		}
		if record.IsReference() {
			id := sharedComponentRecordID(record)
			if id.Name == "" || id.Host == "" {
				decision.Warnings = append(decision.Warnings, fmt.Errorf("infra-component reference record %q has no exact name/host identity", record.Name))
				continue
			}
			referenceIDs[id] = true
			continue
		}
		componentKind := strings.TrimSpace(record.Labels["bootwright.kind"])
		if artifactServerOnly && componentKind != artifactServerRecordKindLabel {
			continue
		}
		id := sharedComponentRecordID(record)
		if id.Name == "" || id.Host == "" {
			decision.Warnings = append(decision.Warnings, fmt.Errorf("infra-component ownership record %q has no exact name/host identity", record.Name))
			continue
		}
		ownIDs[id] = true
		componentKinds[id] = componentKind
		ids = append(ids, id)
	}
	for _, service := range services {
		id := sharedComponentServiceID(service)
		if id.Name == "" || id.Host == "" || ownIDs[id] || referenceIDs[id] {
			continue
		}
		componentKinds[id] = strings.TrimSpace(service.Kind)
		ids = append(ids, id)
	}
	relations, skipped := ownership.OtherContextsWithRolesForComponents(stores, selfContext, ids, ownership.RoleOwner, ownership.RoleReference)
	decision.Warnings = append(decision.Warnings, skipped...)
	seen := map[ownership.SharedComponentID]bool{}
	for _, id := range ids {
		if seen[id] || len(relations[id]) == 0 {
			continue
		}
		seen[id] = true
		decision.Blocks = append(decision.Blocks, InfraComponentDestroyBlock{
			Name:          id.Name,
			Host:          id.Host,
			ComponentKind: componentKinds[id],
			Contexts:      relations[id],
			Unrecorded:    !ownIDs[id],
		})
	}
	decision.Warnings = dedupeErrors(decision.Warnings)
	sort.SliceStable(decision.Blocks, func(i, j int) bool {
		if decision.Blocks[i].Name != decision.Blocks[j].Name {
			return decision.Blocks[i].Name < decision.Blocks[j].Name
		}
		return decision.Blocks[i].Host < decision.Blocks[j].Host
	})
	return decision, nil
}

func sharedComponentRecordID(record ownership.ResourceRecord) ownership.SharedComponentID {
	return ownership.SharedComponentID{
		Kind: strings.TrimSpace(record.Kind),
		Name: strings.TrimSpace(record.Name),
		Host: strings.TrimSpace(record.Host),
	}
}

func sharedComponentServiceID(service InfraComponentServiceRef) ownership.SharedComponentID {
	return ownership.SharedComponentID{
		Kind: ownershipInfraComponentKind,
		Name: strings.TrimSpace(service.Name),
		Host: strings.TrimSpace(service.Host),
	}
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
		identity := block.Name
		if block.Host != "" {
			identity += " on " + block.Host
		}
		parts = append(parts, fmt.Sprintf("%s (%s context(s): %s)", identity, detail, strings.Join(block.Contexts, ", ")))
	}
	return fmt.Errorf("refusing to tear down shared infra-component(s) that other contexts own or reference: %s; destroy or detach them from the owning context first, or re-run with --authorize shared-infra to tear them down regardless", strings.Join(parts, "; "))
}

func InfraComponentDestroyScanWarningError(warnings []error) error {
	if len(warnings) == 0 {
		return nil
	}
	parts := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		parts = append(parts, warning.Error())
	}
	return fmt.Errorf("cannot prove that cross-context infra-component services are unreferenced because %d ownership record(s) could not be evaluated: %s", len(parts), strings.Join(parts, "; "))
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
