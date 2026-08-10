package converge

import (
	"fmt"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/internal/converge/remedy"
	"github.com/crmarques/bootwright/internal/ownership"
)

type ControllerResolverClaim struct {
	Context string
	Name    string
}

type ControllerResolverClaimSafetyError struct {
	findings []string
}

func (e *ControllerResolverClaimSafetyError) Error() string {
	return "refusing to alter controller-global resolver state because another context has a controller resolver claim or its ownership store is inconclusive: " + strings.Join(e.findings, "; ") + "; inspect that context and complete or undo its interrupted resolver operation from the owning desired declaration before retrying this context; no authorization token adopts or removes a sibling context's controller route"
}

func (e *ControllerResolverClaimSafetyError) Remedy() remedy.Request {
	return remedy.Request{Action: remedy.ActionRetrySameInvocation}
}

func OtherContextControllerResolverClaims(selfContext string) ([]ControllerResolverClaim, []error, error) {
	stores, err := siblingContextStores(selfContext)
	if err != nil {
		return nil, nil, err
	}
	var claims []ControllerResolverClaim
	var warnings []error
	for _, store := range stores {
		records, skipped, loadErr := ownership.LoadResourcesWithWarnings(store.Dir)
		for _, warning := range skipped {
			warnings = append(warnings, fmt.Errorf("scan context %q ownership store %s: %w", store.Context, store.Dir, warning))
		}
		if loadErr != nil {
			warnings = append(warnings, fmt.Errorf("scan context %q ownership store %s: %w", store.Context, store.Dir, loadErr))
			continue
		}
		for _, record := range records {
			if strings.TrimSpace(record.Kind) != string(ownership.KindControllerNameResolver) {
				continue
			}
			claims = append(claims, ControllerResolverClaim{Context: strings.TrimSpace(store.Context), Name: strings.TrimSpace(record.Name)})
		}
	}
	sort.SliceStable(claims, func(i, j int) bool {
		if claims[i].Context != claims[j].Context {
			return claims[i].Context < claims[j].Context
		}
		return claims[i].Name < claims[j].Name
	})
	return claims, dedupeErrors(warnings), nil
}

func ControllerResolverClaimRefusal(claims []ControllerResolverClaim, warnings []error, scanErr error) error {
	if len(claims) == 0 && len(warnings) == 0 && scanErr == nil {
		return nil
	}
	var findings []string
	for _, claim := range claims {
		findings = append(findings, fmt.Sprintf("context %q record %q", claim.Context, claim.Name))
	}
	for _, warning := range warnings {
		findings = append(findings, warning.Error())
	}
	if scanErr != nil {
		findings = append(findings, scanErr.Error())
	}
	return &ControllerResolverClaimSafetyError{findings: findings}
}
