package converge

import (
	"strings"

	"github.com/crmarques/bootwright/internal/ownership"
	"github.com/crmarques/bootwright/internal/workspace"
)

// ownershipInfraComponentKind is the ownership-record Kind the infra_component_* roles
// stamp for container-backed bastion services (artifact server, registry, proxy, …).
const ownershipInfraComponentKind = "infra-component"

// selfContainedSharedComponentKinds are the recorded componentKind values — the
// ownership record's bootwright.kind label, stamped by the infra_component_* roles —
// of the self-contained shared bastion services: those that render their whole config
// from the InfraComponent spec and so can be safely co-owned by several contexts on one
// bastion and released (kept) when another context still references them. Keyed by the
// role-stamped kind (e.g. the artifact server stamps "artifacts") because that is what
// an ownership record carries on disk; the slot-vocabulary sibling is
// stategraph.selfContainedSharedServiceSlots (it gates scoped apply, a separate
// concern, and includes the BMC provider service which is not a container record here).
// Degrading services (load-balancer, nameResolution, ntp) render per-cluster config and
// are deliberately absent: they must never be released across contexts.
var selfContainedSharedComponentKinds = map[string]bool{
	"artifacts": true,
	"registry":  true,
	"proxy":     true,
}

// recordedComponentKindIsSelfContainedShared reports whether an ownership record's
// componentKind (its bootwright.kind label) names a self-contained shared bastion
// service eligible for cross-context release.
func recordedComponentKindIsSelfContainedShared(componentKind string) bool {
	return selfContainedSharedComponentKinds[strings.TrimSpace(componentKind)]
}

// InfraComponentReleaseExtraVar carries the comma-joined ownership-record names of the
// shared, self-contained infra-components this destroy must RELEASE — drop only this
// context's reference to — instead of tearing down, because another Bootwright context
// on the same bastion still references them. The infra-component destroy roles and the
// ownership-record teardown skip their destructive steps for a released component but
// still remove this context's own ownership record.
const InfraComponentReleaseExtraVar = "bootwright_infra_component_release_records"

// InfraComponentRelease is one shared, self-contained infra-component kept alive on
// destroy because one or more other contexts still reference it.
type InfraComponentRelease struct {
	Name          string   // ownership record name (<provider>-<component>)
	Host          string   // host the shared component runs on
	ComponentKind string   // recorded bootwright.kind (artifacts/registry/proxy)
	Referrers     []string // other context names still referencing it
}

// ReleaseDecision is the outcome of the cross-context shared-service reference scan.
type ReleaseDecision struct {
	Releases []InfraComponentRelease
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

// PlanInfraComponentReleases scans every OTHER Bootwright context's ownership store on
// this bastion for self-contained shared infra-components this context's destroy would
// otherwise tear down, and returns those still referenced elsewhere so the teardown
// releases (keeps) them. records is this context's own ownership set (already
// context-filtered). A genuine failure to enumerate sibling contexts is returned as an
// error so the caller can fail closed; an individual unreadable sibling store is a
// warning and the scan continues (over-counting fails safe toward release).
func PlanInfraComponentReleases(selfContext string, records []ownership.ResourceRecord) (ReleaseDecision, error) {
	stores, err := siblingContextStores(selfContext)
	if err != nil {
		return ReleaseDecision{}, err
	}
	if len(stores) == 0 {
		return ReleaseDecision{}, nil
	}
	var decision ReleaseDecision
	for _, record := range records {
		if strings.TrimSpace(record.Kind) != ownershipInfraComponentKind {
			continue
		}
		componentKind := strings.TrimSpace(record.Labels["bootwright.kind"])
		if !recordedComponentKindIsSelfContainedShared(componentKind) {
			continue
		}
		referrers, skipped := ownership.OtherContextReferrers(stores, selfContext, ownership.SharedComponentID{
			Kind: record.Kind,
			Name: record.Name,
			Host: record.Host,
		})
		decision.Warnings = append(decision.Warnings, skipped...)
		if len(referrers) > 0 {
			decision.Releases = append(decision.Releases, InfraComponentRelease{
				Name:          record.Name,
				Host:          record.Host,
				ComponentKind: componentKind,
				Referrers:     referrers,
			})
		}
	}
	return decision, nil
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
