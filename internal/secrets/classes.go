package secrets

import (
	"maps"
	"slices"
)

type Class string

const (
	EffectMaterial   Class = "effect material"
	AccessCredential Class = "access credential"
	TrustAnchor      Class = "trust anchor"
)

type Classes map[string]Class

func (c Classes) Add(class Class, names ...string) Classes {
	if c == nil {
		c = Classes{}
	}
	for _, name := range names {
		if name == "" {
			continue
		}
		named := class
		if known, seen := c[name]; seen && known != class {
			named = EffectMaterial
		}
		c[name] = named
	}
	return c
}

func (c Classes) Merge(other Classes) Classes {
	if c == nil {
		c = Classes{}
	}
	for name, class := range other {
		c = c.Add(class, name)
	}
	return c
}

func (c Classes) Of(name string) Class { return c[name] }

func (c Classes) Named(class Class) []string {
	names := []string{}
	for _, name := range slices.Sorted(maps.Keys(c)) {
		if c[name] == class {
			names = append(names, name)
		}
	}
	return names
}
