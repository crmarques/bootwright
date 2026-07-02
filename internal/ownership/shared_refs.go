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

// OtherContextsWithRole returns the names of contexts (other than selfContext) whose
// ownership store records the given shared component with the given effective role
// (RoleOwner or RoleReference), matched by Kind, Name, and Host. ReferenceContexts
// wraps it to drive the owner-refuse-while-referenced destroy gate. Stores are loaded
// with LoadResources (not LoadContext), so a sibling's own context-stamped record
// counts (LoadContext would drop records stamped with the sibling's own context); an
// unreadable store is reported in skipped and the scan continues, failing safe by
// over-counting referrers (keep the shared service) rather than stranding a destroy.
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
