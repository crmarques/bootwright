package ownership

import (
	"fmt"
	"sort"
	"strings"
)

// SharedComponentID identifies a bastion-shared infra-component by its
// context-independent identity: the recorded ownership Kind and Name (Name already
// encodes <provider>-<component>), plus the Host it runs on. Two contexts that
// reference the same shared service on the same bastion record the same triple, so it
// is the key the destroy-time reference scan matches on across contexts.
type SharedComponentID struct {
	Kind string
	Name string
	Host string
}

// ContextStore pairs a context name with the ownership-store directory its records
// live under (the dir passed to LoadResources / SaveResource). The reference scan
// reads every sibling context's store to decide whether a shared service is still
// in use elsewhere before this context's destroy tears it down.
type ContextStore struct {
	Context string
	Dir     string
}

// OtherContextReferrers returns the names of contexts (other than selfContext) whose
// ownership store still records the given shared component, matched by Kind, Name, and
// Host. Each store is loaded with LoadResources — NOT LoadContext — so a sibling's own
// context-stamped record counts (LoadContext would drop records stamped with the
// sibling's own context). A store that cannot be read is reported in skipped rather
// than failing the scan, so one unreadable sibling never strands a destroy; treating
// an unreadable or stale store as a referrer over-counts, which fails safe toward
// release (keep the shared service) rather than toward destroying it under another
// context.
func OtherContextReferrers(stores []ContextStore, selfContext string, id SharedComponentID) (referrers []string, skipped []error) {
	self := strings.TrimSpace(selfContext)
	seen := map[string]bool{}
	for _, store := range stores {
		name := strings.TrimSpace(store.Context)
		if name == "" || name == self || seen[name] {
			continue
		}
		records, err := LoadResources(store.Dir)
		if err != nil {
			skipped = append(skipped, fmt.Errorf("scan context %q ownership store %s: %w", name, store.Dir, err))
			continue
		}
		if recordsReference(records, id) {
			seen[name] = true
			referrers = append(referrers, name)
		}
	}
	sort.Strings(referrers)
	return referrers, skipped
}

func recordsReference(records []ResourceRecord, id SharedComponentID) bool {
	kind := strings.TrimSpace(id.Kind)
	name := strings.TrimSpace(id.Name)
	host := strings.TrimSpace(id.Host)
	for _, record := range records {
		if strings.TrimSpace(record.Kind) == kind &&
			strings.TrimSpace(record.Name) == name &&
			strings.TrimSpace(record.Host) == host {
			return true
		}
	}
	return false
}

// OtherContextsWithRole is the role-aware generalization of OtherContextReferrers:
// it returns the names of contexts (other than selfContext) whose ownership store
// records the given shared component with the given effective role (RoleOwner or
// RoleReference), matched by Kind, Name, and Host. OwnerContexts drives the
// single-owner apply gate and the reference-requires-owner check; ReferenceContexts
// drives the owner-refuse-while-referenced destroy gate. Stores are loaded with
// LoadResources (not LoadContext), so a sibling's own context-stamped record counts;
// an unreadable store is reported in skipped and the scan continues, matching
// OtherContextReferrers' fail-safe behaviour.
func OtherContextsWithRole(stores []ContextStore, selfContext string, id SharedComponentID, role string) (contexts []string, skipped []error) {
	self := strings.TrimSpace(selfContext)
	want := strings.TrimSpace(role)
	seen := map[string]bool{}
	for _, store := range stores {
		name := strings.TrimSpace(store.Context)
		if name == "" || name == self || seen[name] {
			continue
		}
		records, err := LoadResources(store.Dir)
		if err != nil {
			skipped = append(skipped, fmt.Errorf("scan context %q ownership store %s: %w", name, store.Dir, err))
			continue
		}
		if recordsReferenceWithRole(records, id, want) {
			seen[name] = true
			contexts = append(contexts, name)
		}
	}
	sort.Strings(contexts)
	return contexts, skipped
}

// OwnerContexts returns the sibling contexts that hold an OWNER record for the
// shared component. More than one is a single-owner violation.
func OwnerContexts(stores []ContextStore, selfContext string, id SharedComponentID) ([]string, []error) {
	return OtherContextsWithRole(stores, selfContext, id, RoleOwner)
}

// ReferenceContexts returns the sibling contexts that hold a REFERENCE record for
// the shared component. A non-empty result blocks the owner's base teardown.
func ReferenceContexts(stores []ContextStore, selfContext string, id SharedComponentID) ([]string, []error) {
	return OtherContextsWithRole(stores, selfContext, id, RoleReference)
}

func recordsReferenceWithRole(records []ResourceRecord, id SharedComponentID, role string) bool {
	kind := strings.TrimSpace(id.Kind)
	name := strings.TrimSpace(id.Name)
	host := strings.TrimSpace(id.Host)
	for _, record := range records {
		if strings.TrimSpace(record.Kind) == kind &&
			strings.TrimSpace(record.Name) == name &&
			strings.TrimSpace(record.Host) == host &&
			record.EffectiveRole() == role {
			return true
		}
	}
	return false
}
