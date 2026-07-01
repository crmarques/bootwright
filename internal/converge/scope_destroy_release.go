package converge

import (
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/internal/ownership"
	"github.com/crmarques/bootwright/internal/workspace"
)

// ownershipInfraComponentKind is the ownership-record Kind the infra_component_* roles
// stamp for container-backed bastion services (artifact server, registry, proxy, …).
const ownershipInfraComponentKind = "infra-component"

// InfraComponentReleaseExtraVar carries the comma-joined ownership-record names of the
// shared infra-components this destroy must RELEASE — remove only this context's
// contribution (fragment) and its own reference record — instead of tearing down,
// because this context only REFERENCES them (role: reference); the owner context runs
// the base. The infra-component destroy roles and the ownership-record teardown skip
// their destructive steps for a released component but still remove this context's own
// reference record. A reference is released regardless of component kind: additive
// contributions (artifact media, DNS drop-ins, proxy/registry/NTP entries) are always
// caller-scoped, so removing one never degrades the owner's base.
const InfraComponentReleaseExtraVar = "bootwright_infra_component_release_records"

// InfraComponentRelease is one shared infra-component this context's destroy RELEASES
// (keeps the owner's base, removes only this context's contribution) because this
// context only references it.
type InfraComponentRelease struct {
	Name          string // ownership record name (<provider>-<component>)
	Host          string // host the shared component runs on
	ComponentKind string // recorded bootwright.kind (artifacts/registry/proxy/nameResolution/…)
}

// InfraComponentReferencedOwner is a base infra-component this context OWNS that one or
// more sibling contexts still reference, so tearing it down would break them. The
// destroy is refused unless the operator passes --override.
type InfraComponentReferencedOwner struct {
	Name          string   // ownership record name (<provider>-<component>)
	Host          string   // host the shared component runs on
	ComponentKind string   // recorded bootwright.kind
	Referrers     []string // sibling context names still referencing it
}

// ReleaseDecision is the outcome of the cross-context shared-service reference scan.
type ReleaseDecision struct {
	Releases []InfraComponentRelease
	// Blocks are owner-held components that sibling contexts still reference; a
	// destroy that would tear one down must fail closed unless --override.
	Blocks []InfraComponentReferencedOwner
	// Warnings carries per-sibling-store load failures. The scan continues past them
	// (best effort), so a single unreadable sibling never strands a destroy.
	Warnings []error
}

// Names returns the ownership-record names to release, for the executor extra-var.
func (d ReleaseDecision) Names() []string {
	names := make([]string, 0, len(d.Releases))
	for _, release := range d.Releases {
		names = append(names, release.Name)
	}
	return names
}

// PlanInfraComponentReleases decides, for this context's infra-component teardown, which
// records to RELEASE and which owner-held bases are still referenced elsewhere (and so
// must block the destroy). records is this context's own ownership set (already
// context-filtered). The decision keys on each record's ownership ROLE:
//
//   - role reference  -> this context only contributes to / consumes a service another
//     context owns; its teardown removes only this context's fragment + reference
//     record, never the owner's base, for ALL component kinds. Released without a
//     sibling scan (a reference is always caller-scoped).
//   - role owner       -> this context runs the base. If any sibling context holds a
//     reference record for the same (kind,name,host), tearing the base down would break
//     those referrers, so it is reported in Blocks for the caller to refuse (unless
//     --override).
//
// A genuine failure to enumerate sibling contexts is returned as an error so the caller
// can fail closed; an individual unreadable sibling store is a warning and the scan
// continues (over-counting referrers fails safe toward blocking the owner teardown).
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
		referrers, skipped := ownership.ReferenceContexts(stores, selfContext, ownership.SharedComponentID{
			Kind: record.Kind,
			Name: record.Name,
			Host: record.Host,
		})
		decision.Warnings = append(decision.Warnings, skipped...)
		if len(referrers) > 0 {
			decision.Blocks = append(decision.Blocks, InfraComponentReferencedOwner{
				Name:          record.Name,
				Host:          record.Host,
				ComponentKind: componentKind,
				Referrers:     referrers,
			})
		}
	}
	return decision, nil
}

// ReferencedOwnerError renders the owner-refuse-while-referenced blocks into a single
// actionable error for the destroy command to fail closed with (unless --override).
func ReferencedOwnerError(blocks []InfraComponentReferencedOwner) error {
	if len(blocks) == 0 {
		return nil
	}
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		parts = append(parts, fmt.Sprintf("%s (referrers: %s)", block.Name, strings.Join(block.Referrers, ", ")))
	}
	return fmt.Errorf("refusing to tear down shared infra-component(s) still referenced by other contexts: %s; detach the referrers first, or re-run with --override to tear them down regardless", strings.Join(parts, "; "))
}

// ApplyInfraComponentReleaseExtraVar stamps the release set onto a destroy plan. A
// no-op when nothing is released, so a destroy with no shared cross-context services
// carries no extra var (and the teardown roles tear everything down as before).
func ApplyInfraComponentReleaseExtraVar(plan *WorkflowPlan, names []string) {
	if len(names) == 0 {
		return
	}
	plan.ExtraVarPairs = append(plan.ExtraVarPairs, InfraComponentReleaseExtraVar+"="+strings.Join(names, ","))
}

// siblingContextStores lists every usable Bootwright context's ownership store except
// selfContext, for the cross-context shared-service reference scan.
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
