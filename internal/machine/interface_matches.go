package machine

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
)

type InterfaceMatch struct {
	NIC       string
	Interface string
}

func InterfaceMatches(o api.Object, c api.Catalog) []InterfaceMatch {
	native, issues := ComposeNetwork(o, c)
	if len(issues) != 0 || !native.Present() {
		return nil
	}
	matches, issues := resolveMatches(o, c, native)
	if len(issues) != 0 {
		return nil
	}
	resolved := make([]InterfaceMatch, 0, matches.Len())
	for _, match := range matches.Items() {
		resolved = append(resolved, InterfaceMatch{NIC: match.Get("nicRef").Text(), Interface: match.Get("interface").Text()})
	}
	return resolved
}
