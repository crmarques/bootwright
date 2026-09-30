package secretstore

import (
	"reflect"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

type ImplementationCatalog struct {
	implementations []SecretStoreImplementation
	selections      []Selection
	backends        []string
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
		backend := implementation.Backend()
		if !validSelection(selection) || !api.ValidLexical("name", backend) || seen["type:"+selection.Type] || seen["backend:"+backend] {
			c.invalid = true
		}
		seen["type:"+selection.Type], seen["backend:"+backend] = true, true
		c.backends = append(c.backends, backend)
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
			if c.implementations[i].Selection() != selection || c.implementations[i].Backend() != c.backends[i] {
				return nil, Failure("store.implementation", "secret store implementation identity changed")
			}
			return c.implementations[i], nil
		}
	}
	return nil, Failure("store.implementation", "requested secret store type is unavailable")
}

func (c *ImplementationCatalog) Reopen(backend string) (SecretStoreImplementation, error) {
	if c == nil || c.invalid || !api.ValidLexical("name", backend) {
		return nil, Failure("store.implementation", "secret store implementation catalog is invalid")
	}
	for i, id := range c.backends {
		if id == backend {
			implementation := c.implementations[i]
			if implementation.Backend() != id || implementation.Selection() != c.selections[i] {
				return nil, Failure("store.implementation", "secret store implementation identity changed")
			}
			return implementation, nil
		}
	}
	return nil, diagnostics.NewFailureWithRemediation("secret.store.implementation",
		"this context's secret store is "+backend+", which this Bootwright build cannot open", "",
		"destroy this context's effects with the Bootwright build that created it, then run bootwright context delete --name <context> --purge and create the context again")
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
