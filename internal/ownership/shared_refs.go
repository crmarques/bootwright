package ownership

import (
	"fmt"
	"sort"
	"strings"
)

type SharedComponentID struct {
	Kind string
	Name string
	Host string
}

type ContextStore struct {
	Context string
	Dir     string
}

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

func ReferenceContexts(stores []ContextStore, selfContext string, id SharedComponentID) ([]string, []error) {
	return OtherContextsWithRole(stores, selfContext, id, RoleReference)
}

func OwnerContextsForComponent(stores []ContextStore, selfContext, kind, name string) (contexts []string, skipped []error) {
	self := strings.TrimSpace(selfContext)
	wantKind := strings.TrimSpace(kind)
	wantName := strings.TrimSpace(name)
	seen := map[string]bool{}
	for _, store := range stores {
		ctxName := strings.TrimSpace(store.Context)
		if ctxName == "" || ctxName == self || seen[ctxName] {
			continue
		}
		records, err := LoadResources(store.Dir)
		if err != nil {
			skipped = append(skipped, fmt.Errorf("scan context %q ownership store %s: %w", ctxName, store.Dir, err))
			continue
		}
		for _, record := range records {
			if strings.TrimSpace(record.Kind) == wantKind &&
				strings.TrimSpace(record.Name) == wantName &&
				record.EffectiveRole() == RoleOwner {
				seen[ctxName] = true
				contexts = append(contexts, ctxName)
				break
			}
		}
	}
	sort.Strings(contexts)
	return contexts, skipped
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
