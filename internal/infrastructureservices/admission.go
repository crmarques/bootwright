package infrastructureservices

import (
	"fmt"
	"net/netip"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func Normalize(o api.Object, _ api.Catalog) (api.Object, []api.Issue) {
	if !IsService(o.Kind()) {
		return o, nil
	}
	value := normalizeServiceIPs(o.Spec())
	if value.Get("management").Text() != "managed" {
		return o.WithSpec(value), nil
	}
	if o.Kind() != api.LoadBalancer {
		value = value.Default("bindAddress", api.StringValue("0.0.0.0"))
	}
	ports := map[api.Kind]string{api.Proxy: "3128", api.DNSServer: "53", api.NTPServer: "123", api.Registry: "5000"}
	if port := ports[o.Kind()]; port != "" {
		value = value.Default("port", api.IntegerValue(port))
	}
	if o.Kind() == api.ArtifactServer {
		value = value.Default("retention", api.StringValue("persistent"))
		listener := api.MapValue(api.FieldValue{Name: "name", Value: api.StringValue("https")}, api.FieldValue{Name: "protocol", Value: api.StringValue("https")}, api.FieldValue{Name: "port", Value: api.IntegerValue("8443")})
		value = value.Default("listeners", api.ListValue(listener))
		if value.Has("tls") {
			value = value.With("tls", value.Get("tls").Default("minVersion", api.StringValue("TLSv1.2")))
		}
	}
	return o.WithSpec(value), nil
}

func normalizeServiceIPs(value api.Value) api.Value {
	for _, field := range []string{"address", "bindAddress"} {
		if value.Has(field) {
			value = value.With(field, canonicalIP(value.Get(field)))
		}
	}
	for _, field := range []string{"forwarders", "upstreamSources"} {
		if !value.Has(field) {
			continue
		}
		items := value.Get(field).Items()
		for i, item := range items {
			items[i] = canonicalIP(item)
		}
		value = value.With(field, api.ListValue(items...))
	}
	if value.Has("bindAddresses") {
		items := value.Get("bindAddresses").Items()
		for i, item := range items {
			if item.Has("address") {
				items[i] = item.With("address", canonicalIP(item.Get("address")))
			}
		}
		value = value.With("bindAddresses", api.ListValue(items...))
	}
	return value
}

func canonicalIP(value api.Value) api.Value {
	if address, err := netip.ParseAddr(value.Text()); err == nil {
		return api.StringValue(address.String())
	}
	return value
}

func ValidateAuthored(o api.Object, _ api.Catalog) []api.Issue {
	return validateIntrinsic(o, true)
}

func Validate(o api.Object, c api.Catalog) []api.Issue {
	if !IsService(o.Kind()) {
		return nil
	}
	issues := validateIntrinsic(o, false)
	value := o.Spec()
	if value.Get("management").Text() != "managed" {
		return issues
	}
	machine, found := c.Find(api.Machine, value.Get("machineRef").Text())
	if found && !slices.Contains(machine.Spec().Get("capabilities").Strings(), "container-runtime") {
		issues = add(issues, issue("$.spec.machineRef", "service placement requires a Machine with container-runtime capability"))
	}
	for index, endpoint := range value.Get("endpoints").Items() {
		field := fmt.Sprintf("$.spec.endpoints[%d]", index)
		if found && endpoint.Get("addressRef").Present() && !hasNamed(machine.Spec().Get("network", "addresses"), endpoint.Get("addressRef").Text()) {
			issues = add(issues, reference(field+".addressRef", "endpoint address must name an address on its placement Machine"))
		}
		if o.Kind() == api.ArtifactServer && endpoint.Get("listenerRef").Present() && !hasNamed(value.Get("listeners"), endpoint.Get("listenerRef").Text()) {
			issues = add(issues, reference(field+".listenerRef", "artifact endpoint listener must name a listener on this ArtifactServer"))
		}
	}
	return issues
}

func validateIntrinsic(o api.Object, partial bool) []api.Issue {
	if !IsService(o.Kind()) {
		return nil
	}
	value := o.Spec()
	issues := []api.Issue{}
	require := func(field string) {
		if !partial && !value.Has(field) {
			issues = add(issues, issue("$.spec."+field, "selected management mode requires this field"))
		}
	}
	forbid := func(fields ...string) {
		for _, field := range fields {
			if value.Has(field) {
				issues = add(issues, issue("$.spec."+field, "field is not allowed for the selected management mode"))
			}
		}
	}
	switch value.Get("management").Text() {
	case "managed":
		require("machineRef")
		if o.Kind() != api.ArtifactServer {
			require("implementation")
		}
		forbid("connection", "address", "url")
	case "external":
		forbid("machineRef", "implementation", "image", "bindAddress", "port", "listeners", "retention", "tls", "forwarders", "upstreamSources")
		if o.Kind() != api.ArtifactServer {
			forbid("endpoints")
		}
		switch o.Kind() {
		case api.Proxy:
			require("connection")
			connection := value.Get("connection")
			if !partial && connection.Present() && !connection.Has("httpProxy") && !connection.Has("httpsProxy") {
				issues = add(issues, issue("$.spec.connection", "external Proxy requires at least one proxy URL"))
			}
		case api.DNSServer, api.NTPServer:
			require("address")
		case api.Registry:
			require("url")
		case api.ArtifactServer:
			require("endpoints")
			if value.Has("endpoints") && value.Get("endpoints").Len() == 0 {
				issues = add(issues, issue("$.spec.endpoints", "external ArtifactServer requires at least one endpoint"))
			}
		}
	}
	if image := value.Get("image"); image.Present() && !image.Has("local") && !image.Has("public") && !partial {
		issues = add(issues, issue("$.spec.image", "an image pin must supply local or public intent"))
	}
	if o.Kind() == api.DNSServer && value.Has("port") && value.Get("port").Text() != "53" {
		issues = add(issues, issue("$.spec.port", "DNS service requires port 53"))
	}
	if o.Kind() == api.LoadBalancer {
		seen := map[string]bool{}
		addresses := value.Get("bindAddresses").Items()
		for index, address := range addresses {
			name := address.Get("name").Text()
			if len(addresses) > 1 && name == "" {
				issues = add(issues, issue(fmt.Sprintf("$.spec.bindAddresses[%d].name", index), "multiple load-balancer addresses require distinct names"))
			}
			if name != "" && seen[name] {
				issues = add(issues, issue(fmt.Sprintf("$.spec.bindAddresses[%d].name", index), "load-balancer bind-address names must be unique"))
			}
			seen[name] = true
		}
	}
	if o.Kind() == api.ArtifactServer {
		issues = add(issues, validateArtifactEndpoints(value, partial)...)
		issues = add(issues, validateTransport(value, partial)...)
	}
	return issues
}

func validateArtifactEndpoints(value api.Value, partial bool) []api.Issue {
	issues := []api.Issue{}
	for index, endpoint := range value.Get("endpoints").Items() {
		field := fmt.Sprintf("$.spec.endpoints[%d]", index)
		required, forbidden := []string{}, []string{}
		switch value.Get("management").Text() {
		case "managed":
			required, forbidden = []string{"listenerRef", "addressRef"}, []string{"url"}
		case "external":
			required, forbidden = []string{"url"}, []string{"listenerRef", "addressRef"}
		}
		for _, name := range required {
			if !partial && !endpoint.Has(name) {
				issues = add(issues, issue(field+"."+name, "artifact endpoint requires this field for its management mode"))
			}
		}
		for _, name := range forbidden {
			if endpoint.Has(name) {
				issues = add(issues, issue(field+"."+name, "artifact endpoint field conflicts with its management mode"))
			}
		}
	}
	return issues
}

func validateTransport(value api.Value, partial bool) []api.Issue {
	if !value.Has("listeners") {
		return nil
	}
	issues := []api.Issue{}
	https := false
	ports := map[string]bool{}
	for index, listener := range value.Get("listeners").Items() {
		https = https || listener.Get("protocol").Text() == "https"
		port := listener.Get("port").Text()
		if port != "" && ports[port] {
			issues = add(issues, issue(fmt.Sprintf("$.spec.listeners[%d].port", index), "artifact listener ports must be unique"))
		}
		ports[port] = true
	}
	if https && !value.Has("tls") && !partial {
		issues = add(issues, issue("$.spec.tls", "HTTPS artifact listeners require TLS configuration"))
	} else if !https && value.Has("tls") {
		issues = add(issues, issue("$.spec.tls", "HTTP-only artifact listeners forbid TLS configuration"))
	}
	return issues
}

func IsService(kind api.Kind) bool {
	return slices.Contains([]api.Kind{api.Proxy, api.DNSServer, api.NTPServer, api.ArtifactServer, api.Registry, api.LoadBalancer}, kind)
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
	return api.Issue{Code: "api.invariant", Field: field, Message: message, Remediation: "make the service declaration consistent with its typed references and management mode"}
}

func reference(field, message string) api.Issue {
	i := issue(field, message)
	i.Code = "api.reference"
	return i
}

func add(issues []api.Issue, more ...api.Issue) []api.Issue {
	return append(issues, more[:min(len(more), 999-len(issues))]...)
}
