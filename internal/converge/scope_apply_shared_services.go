package converge

import (
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/internal/converge/remedy"
)

type InfraComponentApplyDecision struct {
	Blocks   []InfraComponentDestroyBlock
	Warnings []error
}

type InfraComponentApplySafetyError struct {
	Decision InfraComponentApplyDecision
	ScanErr  error
}

func PlanInfraComponentApplyBlocks(selfContext string, services []InfraComponentServiceRef) (InfraComponentApplyDecision, error) {
	decision, err := PlanInfraComponentDestroyBlocks(selfContext, services, nil, false)
	return InfraComponentApplyDecision(decision), err
}

func InfraComponentApplyRefusal(decision InfraComponentApplyDecision, scanErr error) error {
	if len(decision.Blocks) == 0 && len(decision.Warnings) == 0 && scanErr == nil {
		return nil
	}
	return &InfraComponentApplySafetyError{Decision: decision, ScanErr: scanErr}
}

func (e *InfraComponentApplySafetyError) Error() string {
	var findings []string
	for _, block := range e.Decision.Blocks {
		findings = append(findings, fmt.Sprintf("%s on %s is owned or referenced by context(s) %s", block.Name, block.Host, strings.Join(block.Contexts, ", ")))
	}
	for _, warning := range e.Decision.Warnings {
		findings = append(findings, warning.Error())
	}
	if e.ScanErr != nil {
		findings = append(findings, e.ScanErr.Error())
	}
	return "refusing to apply degrading shared infra-component configuration while another context may still consume the same live service: " + strings.Join(findings, "; ") + "; this run would render only the current context's consumers and could remove sibling cluster endpoints. Consolidate every consumer into one owning context or detach the sibling context from this exact service, repair any reported ownership record, then retry; no --authorize token bypasses this apply gate"
}

func (e *InfraComponentApplySafetyError) Remedy() remedy.Request {
	return remedy.Request{Action: remedy.ActionRetrySameInvocation}
}
