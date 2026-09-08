package environment

import (
	"fmt"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func ValidatePartial(object api.Object, _ api.Catalog) []api.Issue {
	if object.Kind() != api.Environment {
		return nil
	}
	issues := []api.Issue{}
	add := func(field, message string) {
		if len(issues) < 999 {
			issues = append(issues, api.Issue{Code: "api.invariant", Field: field, Message: message})
		}
	}
	for _, entry := range []struct {
		name     string
		external []string
	}{{"proxies", []string{"connection"}}, {"nameResolution", []string{"address"}}, {"artifactServers", []string{"endpoints"}}, {"registries", []string{"url"}}, {"ntp", []string{"address"}}} {
		defaults := 0
		for i, row := range object.Spec().Get("infraComponents", entry.name).Items() {
			field := fmt.Sprintf("$.spec.infraComponents.%s[%d]", entry.name, i)
			if row.Get("default").Bool() {
				defaults++
			}
			if row.Get("name").Text() == "none" {
				add(field+".name", "catalog name is reserved")
			}
			switch row.Get("management").Text() {
			case "managed":
				for _, name := range entry.external {
					if row.Has(name) {
						add(field+"."+name, "managed catalog entries forbid external connection facts")
					}
				}
			case "external":
				for _, name := range []string{"componentRef", "endpointRef"} {
					if row.Has(name) {
						add(field+"."+name, "external catalog entries forbid managed component references")
					}
				}
			}
		}
		if defaults > 1 {
			add("$.spec.infraComponents."+entry.name, "catalog has more than one default entry")
		}
	}
	return issues
}
