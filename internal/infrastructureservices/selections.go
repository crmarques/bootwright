package infrastructureservices

import (
	"fmt"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// NormalizeProxy fills only a direct choice or an unambiguous managed endpoint.
// It preserves references so service connection facts stay on their owner.
func NormalizeProxy(selection api.Value, c api.Catalog) api.Value {
	if !selection.Present() {
		return api.MapValue(api.FieldValue{Name: "direct", Value: api.MapValue()})
	}
	return normalizeEndpoint(selection, c, api.Proxy, "proxyRef")
}

func ValidateProxy(selection api.Value, c api.Catalog, field string, externalOnly bool) []api.Issue {
	if !selection.Present() {
		return nil
	}
	if issues := ValidateProxyChoice(selection, field); len(issues) != 0 {
		return issues
	}
	if !selection.Has("proxyRef") && !selection.Has("direct") {
		return []api.Issue{issue(field, "proxy choice requires exactly one proxyRef or direct block")}
	}
	if selection.Has("direct") {
		return nil
	}
	issues := validateEndpoint(selection, c, api.Proxy, "proxyRef", field)
	if externalOnly {
		if proxy, ok := c.Find(api.Proxy, selection.Get("proxyRef").Text()); ok && proxy.Spec().Get("management").Text() != "external" {
			issues = add(issues, issue(field+".proxyRef", "machine OS installation requires an external Proxy"))
		}
	}
	return issues
}

// ValidateProxyChoice checks local contradictions without completing partial
// defaults or resolving references outside the selected graph.
func ValidateProxyChoice(selection api.Value, field string) []api.Issue {
	if selection.Has("proxyRef") && selection.Has("direct") {
		return []api.Issue{issue(field, "proxy choice requires exactly one proxyRef or direct block")}
	}
	if selection.Has("direct") && (selection.Has("endpointRef") || selection.Has("noProxy")) {
		return []api.Issue{issue(field, "direct proxy choice forbids endpointRef and noProxy")}
	}
	return nil
}

func NormalizeServerSelections(selections api.Value, c api.Catalog, kind api.Kind) api.Value {
	if !selections.Present() {
		return selections
	}
	items := selections.Items()
	for index, selection := range items {
		items[index] = normalizeEndpoint(selection, c, kind, "serverRef")
	}
	return api.ListValue(items...)
}

func ValidateServerSelections(selections api.Value, c api.Catalog, kind api.Kind, field string) []api.Issue {
	issues := []api.Issue{}
	seen := map[[2]string]bool{}
	for index, selection := range selections.Items() {
		path := fmt.Sprintf("%s[%d]", field, index)
		issues = add(issues, validateEndpoint(selection, c, kind, "serverRef", path)...)
		resolved := normalizeEndpoint(selection, c, kind, "serverRef")
		key := [2]string{resolved.Get("serverRef").Text(), resolved.Get("endpointRef").Text()}
		if seen[key] {
			issues = add(issues, issue(path, "server selections must identify distinct service and endpoint pairs"))
		}
		seen[key] = true
	}
	return issues
}

func NormalizeRegistrySelection(selection api.Value, c api.Catalog) api.Value {
	return normalizeEndpoint(selection, c, api.Registry, "registryRef")
}

func ValidateRegistrySelection(selection api.Value, c api.Catalog, field string) []api.Issue {
	if !selection.Present() {
		return nil
	}
	return validateEndpoint(selection, c, api.Registry, "registryRef", field)
}

func normalizeEndpoint(selection api.Value, c api.Catalog, kind api.Kind, ref string) api.Value {
	if !selection.Has(ref) || selection.Has("endpointRef") {
		return selection
	}
	service, ok := c.Find(kind, selection.Get(ref).Text())
	if !ok || service.Spec().Get("management").Text() != "managed" {
		return selection
	}
	endpoints := service.Spec().Get("endpoints").Items()
	if len(endpoints) == 1 && endpoints[0].Has("name") {
		return selection.With("endpointRef", endpoints[0].Get("name"))
	}
	return selection
}

func validateEndpoint(selection api.Value, c api.Catalog, kind api.Kind, ref, field string) []api.Issue {
	if !selection.Has(ref) {
		return []api.Issue{reference(field+"."+ref, "service selection requires an explicit typed reference")}
	}
	service, ok := c.Find(kind, selection.Get(ref).Text())
	if !ok {
		return []api.Issue{reference(field+"."+ref, "service reference must resolve to one "+string(kind)+" object")}
	}
	switch service.Spec().Get("management").Text() {
	case "external":
		if selection.Has("endpointRef") {
			return []api.Issue{issue(field+".endpointRef", "external service selections forbid endpointRef")}
		}
	case "managed":
		resolved := normalizeEndpoint(selection, c, kind, ref)
		if !resolved.Has("endpointRef") {
			return []api.Issue{reference(field+".endpointRef", "managed service selection requires an endpointRef unless exactly one endpoint exists")}
		}
		if !hasNamed(service.Spec().Get("endpoints"), resolved.Get("endpointRef").Text()) {
			return []api.Issue{reference(field+".endpointRef", "endpoint reference must resolve on the selected service")}
		}
	}
	return nil
}

// ArtifactEndpoint resolves only explicitly selected managed ArtifactServers.
func ArtifactEndpoint(selection api.Value, c api.Catalog) (api.Object, bool) {
	service, ok := c.Find(api.ArtifactServer, selection.Get("serverRef").Text())
	return service, ok && selection.Has("serverRef") && service.Spec().Get("management").Text() == "managed"
}

func ValidateArtifactEndpoint(selection api.Value, c api.Catalog, field string, requireHTTP bool) []api.Issue {
	if !selection.Present() {
		return nil
	}
	service, ok := ArtifactEndpoint(selection, c)
	if !ok {
		return []api.Issue{reference(field+".serverRef", "artifact consumer requires an explicit reference to a managed ArtifactServer")}
	}
	if !selection.Has("endpointRef") {
		return []api.Issue{reference(field+".endpointRef", "artifact consumer requires an explicit endpointRef")}
	}
	for _, endpoint := range service.Spec().Get("endpoints").Items() {
		if endpoint.Get("name").Text() != selection.Get("endpointRef").Text() {
			continue
		}
		if requireHTTP {
			for _, listener := range service.Spec().Get("listeners").Items() {
				if listener.Get("name").Equal(endpoint.Get("listenerRef")) && listener.Get("protocol").Text() != "http" {
					return []api.Issue{issue(field+".endpointRef", "hosted package content requires an HTTP artifact endpoint")}
				}
			}
		}
		return nil
	}
	return []api.Issue{reference(field+".endpointRef", "artifact endpoint does not exist on the selected ArtifactServer")}
}
