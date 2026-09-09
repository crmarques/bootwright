package storage

import (
	"reflect"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// ImplementationCatalog is a frozen set built by the composition root. Selection
// is exact; registering another implementation never changes existing stores.
type ImplementationCatalog struct {
	implementations []SecretStoreImplementation
	selections      []Selection
	invalid         bool
}

func NewCatalog(implementations ...SecretStoreImplementation) *ImplementationCatalog {
	c := &ImplementationCatalog{implementations: slices.Clone(implementations)}
	seen := map[string]bool{}
	for _, implementation := range c.implementations {
		if implementation == nil || reflect.ValueOf(implementation).Kind() == reflect.Pointer && reflect.ValueOf(implementation).IsNil() {
			c.invalid = true
			continue
		}
		selection := implementation.Selection()
		if !validSelection(selection) || seen[selection.Type] {
			c.invalid = true
		}
		seen[selection.Type] = true
		c.selections = append(c.selections, selection)
	}
	return c
}

func (c *ImplementationCatalog) Types() []string {
	if c == nil || c.invalid {
		return []string{}
	}
	result := make([]string, 0, len(c.selections))
	for _, selection := range c.selections {
		result = append(result, selection.Type)
	}
	slices.Sort(result)
	return result
}

func (c *ImplementationCatalog) Select(kind string) (SecretStoreImplementation, error) {
	if c == nil || c.invalid || !api.ValidLexical("name", kind) {
		return nil, Failure("store.implementation", "secret store implementation catalog is invalid")
	}
	for i, selection := range c.selections {
		if selection.Type == kind {
			if c.implementations[i].Selection() != selection {
				return nil, Failure("store.implementation", "secret store implementation identity changed")
			}
			return c.implementations[i], nil
		}
	}
	return nil, Failure("store.implementation", "requested secret store type is unavailable")
}

func (c *ImplementationCatalog) Reopen(selection Selection) (SecretStoreImplementation, error) {
	implementation, err := c.Select(selection.Type)
	if err != nil {
		return nil, err
	}
	if implementation.Selection() != selection {
		return nil, Failure("store.implementation", "persisted secret store implementation is incompatible")
	}
	return implementation, nil
}

func validSelection(s Selection) bool {
	if !api.ValidLexical("name", s.Type) {
		return false
	}
	for _, ref := range []ComponentRef{s.Store, s.KeyCustody} {
		if !api.ValidLexical("name", ref.ID) || ref.InterfaceVersion < 1 || ref.StateVersion < 1 || ref.ConfigVersion < 1 {
			return false
		}
	}
	return true
}
