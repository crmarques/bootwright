package infrastructureservices

import (
	"fmt"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func Normalize(o api.Object, _ api.Catalog) (api.Object, []api.Issue) { return o, nil }

func ValidateAuthored(o api.Object, c api.Catalog) []api.Issue {
	if o.Kind() != api.InfraComponent {
		return nil
	}
	return validateTransport(o)
}

func Validate(o api.Object, c api.Catalog) []api.Issue {
	if o.Kind() != api.InfraComponent {
		return nil
	}
	issues := validateTransport(o)
	arm, value := Arm(o)
	if arm == "" {
		return issues
	}
	machine, found := c.Find(api.Machine, value.Get("machineRef").Text())
	if found && arm != "ntp" && !slices.Contains(machine.Spec().Get("capabilities").Strings(), "container-runtime") {
		issues = add(issues, issue("$.spec."+arm+".machineRef", "service placement requires a Machine with container-runtime capability"))
	}
	if arm == "nameResolution" && value.Get("port").Present() && value.Get("port").Text() != "53" {
		issues = add(issues, issue("$.spec.nameResolution.port", "name resolution requires port 53"))
	}
	if arm == "loadBalancer" {
		seen := map[string]bool{}
		addresses := value.Get("bindAddresses").Items()
		for index, address := range addresses {
			name := address.Get("name").Text()
			if len(addresses) > 1 && name == "" {
				issues = add(issues, issue(fmt.Sprintf("$.spec.loadBalancer.bindAddresses[%d].name", index), "multiple load-balancer addresses require distinct names"))
			}
			if name != "" && seen[name] {
				issues = add(issues, issue(fmt.Sprintf("$.spec.loadBalancer.bindAddresses[%d].name", index), "load-balancer bind-address names must be unique"))
			}
			seen[name] = true
		}
	}
	for index, endpoint := range value.Get("endpoints").Items() {
		field := fmt.Sprintf("$.spec.%s.endpoints[%d]", arm, index)
		if found && endpoint.Get("addressRef").Present() && !hasNamed(machine.Spec().Get("network", "addresses"), endpoint.Get("addressRef").Text()) {
			issues = add(issues, reference(field+".addressRef", "endpoint address must name an address on its placement Machine"))
		}
		if arm == "artifactServer" && endpoint.Get("listenerRef").Present() && !hasNamed(value.Get("listeners"), endpoint.Get("listenerRef").Text()) {
			issues = add(issues, reference(field+".listenerRef", "artifact endpoint listener must name a listener on this component"))
		}
	}
	return issues[:min(len(issues), 999)]
}

func Arm(o api.Object) (string, api.Value) {
	selected := ""
	for _, arm := range []string{"artifactServer", "loadBalancer", "proxy", "nameResolution", "ntp", "registry"} {
		if o.Spec().Has(arm) {
			if selected != "" {
				return "", api.Value{}
			}
			selected = arm
		}
	}
	return selected, o.Spec().Get(selected)
}

func ArtifactEndpoint(selection api.Value, c api.Catalog) (api.Value, api.Object, bool) {
	environments := c.OfKind(api.Environment)
	if len(environments) != 1 {
		return api.Value{}, api.Object{}, false
	}
	rows := environments[0].Spec().Get("infraComponents", "artifactServers").Items()
	name := selection.Get("serverRef").Text()
	var selected api.Value
	count := 0
	for _, row := range rows {
		if name != "" && row.Get("name").Text() == name || name == "" && (row.Get("default").Bool() || len(rows) == 1) {
			selected = row
			count++
		}
	}
	if count != 1 || selected.Get("management").Text() != "managed" {
		return api.Value{}, api.Object{}, false
	}
	component, found := c.Find(api.InfraComponent, selected.Get("componentRef").Text())
	if !found || !component.Spec().Has("artifactServer") {
		return selected, api.Object{}, false
	}
	return selected, component, true
}

func ValidateArtifactEndpoint(selection api.Value, c api.Catalog, field string, requireHTTP bool) []api.Issue {
	if !selection.Present() || len(c.OfKind(api.Environment)) != 1 {
		return nil
	}
	_, component, ok := ArtifactEndpoint(selection, c)
	if !ok {
		return []api.Issue{reference(field+".serverRef", "artifact endpoint requires an unambiguous managed artifact-server catalog entry")}
	}
	if !selection.Get("endpointRef").Present() {
		return nil
	}
	for _, endpoint := range component.Spec().Get("artifactServer", "endpoints").Items() {
		if endpoint.Get("name").Text() != selection.Get("endpointRef").Text() {
			continue
		}
		if requireHTTP {
			for _, listener := range component.Spec().Get("artifactServer", "listeners").Items() {
				if listener.Get("name").Equal(endpoint.Get("listenerRef")) && listener.Get("protocol").Text() != "http" {
					return []api.Issue{issue(field+".endpointRef", "hosted package content requires an HTTP artifact endpoint")}
				}
			}
		}
		return nil
	}
	return []api.Issue{reference(field+".endpointRef", "artifact endpoint does not exist on the selected component")}
}

func validateTransport(o api.Object) []api.Issue {
	value := o.Spec().Get("artifactServer")
	if !value.Present() || !value.Has("listeners") {
		return nil
	}
	issues := []api.Issue{}
	https := false
	ports := map[string]bool{}
	for index, listener := range value.Get("listeners").Items() {
		https = https || listener.Get("protocol").Text() == "https"
		port := listener.Get("port").Text()
		if port != "" && ports[port] {
			issues = add(issues, issue(fmt.Sprintf("$.spec.artifactServer.listeners[%d].port", index), "artifact listener ports must be unique"))
		}
		ports[port] = true
	}
	if https && !value.Has("tls") {
		issues = add(issues, issue("$.spec.artifactServer.tls", "HTTPS artifact listeners require TLS configuration"))
	} else if !https && value.Has("tls") {
		issues = add(issues, issue("$.spec.artifactServer.tls", "HTTP-only artifact listeners forbid TLS configuration"))
	}
	return issues
}

func hasNamed(values api.Value, name string) bool {
	count := 0
	for _, value := range values.Items() {
		if value.Get("name").Text() == name {
			count++
		}
	}
	return count == 1
}

func issue(field, message string) api.Issue {
	return api.Issue{Code: "api.invariant", Field: field, Message: message, Remediation: "make the component declaration consistent with its referenced resources"}
}

func reference(field, message string) api.Issue {
	i := issue(field, message)
	i.Code = "api.reference"
	return i
}

func add(issues []api.Issue, more ...api.Issue) []api.Issue {
	return append(issues, more[:min(len(more), 999-len(issues))]...)
}
