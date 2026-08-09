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
	relations, skipped := OtherContextsWithRolesForComponents(stores, selfContext, []SharedComponentID{id}, role)
	return relations[normalizeSharedComponentID(id)], skipped
}

func OtherContextsWithRolesForComponents(stores []ContextStore, selfContext string, ids []SharedComponentID, roles ...string) (map[SharedComponentID][]string, []error) {
	self := strings.TrimSpace(selfContext)
	wantedIDs := map[SharedComponentID]bool{}
	for _, id := range ids {
		id = normalizeSharedComponentID(id)
		if id.Kind != "" && id.Name != "" && id.Host != "" {
			wantedIDs[id] = true
		}
	}
	if len(wantedIDs) == 0 {
		return map[SharedComponentID][]string{}, nil
	}
	wantedRoles := map[string]bool{}
	for _, role := range roles {
		if role = strings.TrimSpace(role); role != "" {
			wantedRoles[role] = true
		}
	}
	relations := map[SharedComponentID][]string{}
	seenContexts := map[string]bool{}
	seenRelations := map[SharedComponentID]map[string]bool{}
	var skipped []error
	for _, store := range stores {
		name := strings.TrimSpace(store.Context)
		if name == "" || name == self || seenContexts[name] {
			continue
		}
		seenContexts[name] = true
		records, warnings, err := LoadResourcesWithWarnings(store.Dir)
		for _, warning := range warnings {
			skipped = append(skipped, fmt.Errorf("scan context %q ownership store %s: %w", name, store.Dir, warning))
		}
		if err != nil {
			skipped = append(skipped, fmt.Errorf("scan context %q ownership store %s: %w", name, store.Dir, err))
			continue
		}
		for _, record := range records {
			id := normalizeSharedComponentID(SharedComponentID{Kind: record.Kind, Name: record.Name, Host: record.Host})
			if id.Host == "" && sharedComponentKindNameWanted(wantedIDs, id.Kind, id.Name) {
				skipped = append(skipped, fmt.Errorf("scan context %q ownership store %s: ownership resource %s/%s has no host, so its shared-component identity cannot be distinguished", name, store.Dir, id.Kind, id.Name))
				continue
			}
			if !wantedIDs[id] || !wantedRoles[record.EffectiveRole()] {
				continue
			}
			if seenRelations[id] == nil {
				seenRelations[id] = map[string]bool{}
			}
			if seenRelations[id][name] {
				continue
			}
			seenRelations[id][name] = true
			relations[id] = append(relations[id], name)
		}
	}
	for id := range relations {
		sort.Strings(relations[id])
	}
	return relations, skipped
}

func sharedComponentKindNameWanted(ids map[SharedComponentID]bool, kind, name string) bool {
	for id := range ids {
		if id.Kind == kind && id.Name == name {
			return true
		}
	}
	return false
}

func ReferenceContexts(stores []ContextStore, selfContext string, id SharedComponentID) ([]string, []error) {
	return OtherContextsWithRole(stores, selfContext, id, RoleReference)
}

func normalizeSharedComponentID(id SharedComponentID) SharedComponentID {
	return SharedComponentID{
		Kind: strings.TrimSpace(id.Kind),
		Name: strings.TrimSpace(id.Name),
		Host: strings.TrimSpace(id.Host),
	}
}
