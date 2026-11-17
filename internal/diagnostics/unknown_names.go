package diagnostics

import (
	"slices"
	"strings"
)

func UnknownNames(kind, contextName string, requested, present []string) error {
	var unknown []string
	for _, name := range requested {
		if !slices.Contains(present, name) {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	subject := "no " + kind + " named "
	if len(unknown) > 1 {
		subject = "no " + kind + "s named "
	}
	return NewFailureWithRemediation("access.target",
		"the context "+contextName+" selects "+subject+strings.Join(unknown, ", "), "",
		"name a "+kind+" the context selects; bootwright "+strings.ToLower(kind)+" list --context "+contextName+" lists them")
}
