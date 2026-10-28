package v1alpha1

import "strings"

type Scope struct {
	Noun    string
	Owner   []string
	Sibling string
	Kind    Kind
	Each    []string
	Select  []string
	Within  []string
	Entries []string
	Key     []string
}

type ScopeOwner struct {
	Object Object
	Names  []string
}

func (s *Scope) Owners(object Object, record Value, catalog Catalog) ([]ScopeOwner, bool) {
	var owners []Object
	switch {
	case s.Sibling != "":
		owner, found := catalog.Find(s.Kind, record.Get(s.Sibling).Text())
		if !found {
			return nil, false
		}
		owners = append(owners, owner)
	case len(s.Each) > 0:
		selected := object.Spec().Get(s.Select...).Text()
		for _, entry := range object.Spec().Get(s.Each...).Items() {
			if len(s.Select) > 0 && entry.Get("name").Text() != selected {
				continue
			}
			if owner, found := catalog.Find(s.Kind, entry.Get(s.Owner...).Text()); found {
				owners = append(owners, owner)
			}
		}
	case len(s.Owner) > 0:
		owner, found := catalog.Find(s.Kind, object.Spec().Get(s.Owner...).Text())
		if !found {
			return nil, false
		}
		owners = append(owners, owner)
	default:
		owners = append(owners, object)
	}
	var scoped []ScopeOwner
	for _, owner := range owners {
		if len(s.Within) > 0 && !owner.Spec().Has(s.Within...) {
			continue
		}
		scoped = append(scoped, ScopeOwner{Object: owner, Names: s.names(owner)})
	}
	return scoped, len(scoped) > 0
}

func (s *Scope) names(owner Object) []string {
	key := s.Key
	if len(key) == 0 {
		key = []string{"name"}
	}
	var names []string
	for _, entry := range owner.Spec().Get(s.Entries...).Items() {
		parts := []string{}
		for _, field := range key {
			if part := entry.Get(field).Text(); part != "" {
				parts = append(parts, part)
			}
		}
		names = append(names, strings.Join(parts, "."))
	}
	return names
}

func nestedIn(scope Scope) *Shape {
	return &Shape{Type: String, MinLength: 1, Nested: NestedReference, Scope: &scope}
}

func endpointOf(sibling string, kind Kind) *Shape {
	return &Shape{Type: String, MinLength: 1, Nested: EndpointReference,
		Scope: &Scope{Noun: "endpoint", Sibling: sibling, Kind: kind, Entries: []string{"endpoints"}}}
}

func nativeServiceOf(owner ...string) *Shape {
	scope := Scope{Noun: "native service", Entries: []string{"services"}, Key: []string{"service_type", "service_id"}}
	if len(owner) > 0 {
		scope.Owner, scope.Kind = owner, CephCluster
	}
	return &Shape{Type: String, MinLength: 1, Nested: NativeServiceReference, Scope: &scope}
}
