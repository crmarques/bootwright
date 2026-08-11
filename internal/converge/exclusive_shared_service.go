package converge

import (
	"fmt"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/internal/converge/remedy"
	"github.com/crmarques/bootwright/internal/ownership"
)

type ExclusiveSharedServiceDecision struct {
	Blocks   []ExclusiveSharedServiceBlock
	Warnings []error
}

type ExclusiveSharedServiceBlock struct {
	Kind     string
	Name     string
	Host     string
	Contexts []string
}

type ExclusiveSharedServiceSafetyError struct {
	Decision ExclusiveSharedServiceDecision
	ScanErr  error
}

func PlanExclusiveSharedServiceBlocks(selfContext string, services []InfraComponentServiceRef) (ExclusiveSharedServiceDecision, error) {
	if len(services) == 0 {
		return ExclusiveSharedServiceDecision{}, nil
	}
	stores, err := siblingContextStores(selfContext)
	if err != nil {
		return ExclusiveSharedServiceDecision{}, err
	}
	decision := ExclusiveSharedServiceDecision{}
	ids := make([]ownership.SharedComponentID, 0, len(services))
	seen := map[ownership.SharedComponentID]bool{}
	for _, service := range services {
		id := ownership.SharedComponentID{
			Kind: strings.TrimSpace(service.Kind),
			Name: strings.TrimSpace(service.Name),
			Host: strings.TrimSpace(service.Host),
		}
		if id.Kind == "" || id.Name == "" || id.Host == "" {
			decision.Warnings = append(decision.Warnings, fmt.Errorf("exclusive shared-service consequence has no exact kind/name/host identity: kind=%q name=%q host=%q", id.Kind, id.Name, id.Host))
			continue
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	relations, skipped := ownership.OtherContextsWithRolesForComponents(stores, selfContext, ids, ownership.RoleOwner, ownership.RoleReference)
	decision.Warnings = append(decision.Warnings, skipped...)
	for _, id := range ids {
		contexts := relations[id]
		if len(contexts) == 0 {
			continue
		}
		decision.Blocks = append(decision.Blocks, ExclusiveSharedServiceBlock{
			Kind: id.Kind, Name: id.Name, Host: id.Host, Contexts: contexts,
		})
	}
	decision.Warnings = dedupeErrors(decision.Warnings)
	sort.SliceStable(decision.Blocks, func(i, j int) bool {
		if decision.Blocks[i].Kind != decision.Blocks[j].Kind {
			return decision.Blocks[i].Kind < decision.Blocks[j].Kind
		}
		if decision.Blocks[i].Name != decision.Blocks[j].Name {
			return decision.Blocks[i].Name < decision.Blocks[j].Name
		}
		return decision.Blocks[i].Host < decision.Blocks[j].Host
	})
	return decision, nil
}

func ExclusiveSharedServiceRefusal(decision ExclusiveSharedServiceDecision, scanErr error) error {
	if len(decision.Blocks) == 0 && len(decision.Warnings) == 0 && scanErr == nil {
		return nil
	}
	return &ExclusiveSharedServiceSafetyError{Decision: decision, ScanErr: scanErr}
}

func (e *ExclusiveSharedServiceSafetyError) Error() string {
	findings := make([]string, 0, len(e.Decision.Blocks)+len(e.Decision.Warnings)+1)
	for _, block := range e.Decision.Blocks {
		findings = append(findings, fmt.Sprintf("%s/%s on %s is owned or referenced by context(s) %s", block.Kind, block.Name, block.Host, strings.Join(block.Contexts, ", ")))
	}
	for _, warning := range e.Decision.Warnings {
		findings = append(findings, warning.Error())
	}
	if e.ScanErr != nil {
		findings = append(findings, e.ScanErr.Error())
	}
	return "refusing to mutate an exclusive provider-global shared service while another context may own it or its ownership evidence is inconclusive: " + strings.Join(findings, "; ") + "; reconcile or destroy it from the owning context and repair any reported evidence, then retry the exact command; no authorization token adopts, redefines, or deletes this service"
}

func (e *ExclusiveSharedServiceSafetyError) Remedy() remedy.Request {
	return remedy.Request{Action: remedy.ActionRetrySameInvocation}
}
