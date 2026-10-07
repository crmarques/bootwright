package infrastructureservices

import (
	"fmt"
	"net/netip"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func Normalize(o api.Object, _ api.Catalog) (api.Object, []api.Issue) {
	if !IsService(o.Kind()) {
		return o, nil
	}
	value := normalizeServiceImages(normalizeServiceIPs(o.Spec()))
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

func normalizeServiceImages(value api.Value) api.Value {
	image := value.Get("image")
	for _, field := range []string{"local", "public"} {
		reference := image.Get(field)
		if canonical := api.CanonicalImage(reference.Text()); reference.Type() == api.String && canonical != reference.Text() {
			image = image.With(field, api.StringValue(canonical))
			value = value.With("image", image)
		}
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
	issues = add(issues, validateWildcardProbes(o.Kind(), value)...)
	machine, placed := c.Find(api.Machine, value.Get("machineRef").Text())
	if placed {
		issues = add(issues, validatePlacement(o.Kind(), value, machine)...)
	}
	if o.Kind() != api.ArtifactServer {
		return issues
	}
	for index, endpoint := range value.Get("endpoints").Items() {
		if endpoint.Get("listenerRef").Present() && !hasNamed(value.Get("listeners"), endpoint.Get("listenerRef").Text()) {
			issues = add(issues, reference(fmt.Sprintf("$.spec.endpoints[%d].listenerRef", index), "artifact endpoint listener must name a listener on this ArtifactServer", "set listenerRef to a name in spec.listeners"))
		}
	}
	if placed {
		issues = add(issues, validateCertificateCoverage(value, machine, c)...)
	}
	return issues
}

// validateCertificateCoverage holds a generated serving certificate to every
// address an HTTPS endpoint answers on, which the server's own validation
// proves at apply only after the operation registered: an IP must be one of
// the certificate's IP addresses, compared as Go's hostname verification
// compares them, so an IPv4-mapped IPv6 address names its IPv4 one, and a name
// must be one of its DNS names, compared without case. The common name is no
// subject alternative name, and a DNS name holds no wildcard, so neither
// covers anything else. Admission reads no material, so a contextStore
// certificate is proved at apply alone.
func validateCertificateCoverage(value api.Value, machine api.Object, c api.Catalog) []api.Issue {
	name := value.Get("tls", "secretRef").Text()
	secret, found := c.Find(api.Secret, name)
	if !found || secret.Spec().Get("type").Text() != "tlsCertificate" {
		return nil
	}
	generated := secret.Spec().Get("source", "generated")
	if !generated.Present() {
		return nil
	}
	issues := []api.Issue{}
	for index, endpoint := range value.Get("endpoints").Items() {
		reference := endpoint.Get("addressRef").Text()
		if listenerProtocol(value, endpoint.Get("listenerRef").Text()) != "https" || !hasNamed(machine.Spec().Get("network", "addresses"), reference) {
			continue
		}
		address := machineAddress(machine, reference)
		if address == "" || certificateNames(generated, address) {
			continue
		}
		field := "dnsNames"
		if _, err := netip.ParseAddr(address); err == nil {
			field = "ipAddresses"
		}
		issues = add(issues, issue(fmt.Sprintf("$.spec.endpoints[%d]", index),
			"endpoint "+endpoint.Get("name").Text()+" serves HTTPS at "+address+", which the generated certificate of Secret/"+name+" does not name",
			"add "+address+" to spec.source.generated."+field+" of Secret/"+name+", import the change with bootwright context update --name <context> --input-dir <dir>, "+
				"then run bootwright secret generate --name "+name+" --context <context>"))
	}
	return issues
}

// listenerProtocol is the protocol of the one listener named so, or nothing.
func listenerProtocol(value api.Value, name string) string {
	if !hasNamed(value.Get("listeners"), name) {
		return ""
	}
	for _, listener := range value.Get("listeners").Items() {
		if listener.Get("name").Text() == name {
			return listener.Get("protocol").Text()
		}
	}
	return ""
}

// certificateNames reports whether a generated certificate's subject
// alternative names include address.
func certificateNames(generated api.Value, address string) bool {
	if ip, err := netip.ParseAddr(address); err == nil {
		for _, listed := range generated.Get("ipAddresses").Strings() {
			if san, err := netip.ParseAddr(listed); err == nil && san.Unmap() == ip.Unmap() {
				return true
			}
		}
		return false
	}
	for _, listed := range generated.Get("dnsNames").Strings() {
		if strings.EqualFold(listed, address) {
			return true
		}
	}
	return false
}

// validatePlacement holds a managed service to what its placement Machine can
// run and to the addresses its consumers can reach there.
func validatePlacement(kind api.Kind, value api.Value, machine api.Object) []api.Issue {
	issues := []api.Issue{}
	if !slices.Contains(machine.Spec().Get("capabilities").Strings(), "container-runtime") {
		issues = add(issues, issue("$.spec.machineRef", "service placement requires a Machine with container-runtime capability",
			"add container-runtime to spec.capabilities of "+machine.Identity()+", or place the service on a Machine that has it"))
	}
	if provided := machine.Spec().Get("os", "provided"); provided.Type() == api.Boolean && !provided.Bool() {
		issues = add(issues, issue("$.spec.machineRef", machine.Identity()+" does not provide its operating system (os.provided: false), and a managed service runs only on a Machine whose OS is ready",
			"set spec.machineRef to a Machine with os.provided: true"))
	}
	bind, bound := boundAddress(value)
	for index, endpoint := range value.Get("endpoints").Items() {
		if !endpoint.Get("addressRef").Present() {
			continue
		}
		field, ref := fmt.Sprintf("$.spec.endpoints[%d].addressRef", index), endpoint.Get("addressRef").Text()
		if !hasNamed(machine.Spec().Get("network", "addresses"), ref) {
			issues = add(issues, reference(field, "endpoint address must name an address on its placement Machine", "set addressRef to an entry name in spec.network.addresses of "+machine.Identity()))
			continue
		}
		resolved := machineAddress(machine, ref)
		address, err := netip.ParseAddr(resolved)
		if kind == api.DNSServer && err != nil {
			issues = add(issues, issue(field, "a DNSServer endpoint must name an IP address, because a resolver is configured by address; address "+ref+" of "+machine.Identity()+" is the name "+resolved,
				"set addressRef to an IP address declared in spec.network.addresses of "+machine.Identity()))
		}
		if err == nil && bound && address != bind {
			issues = add(issues, issue(field, "endpoint "+endpoint.Get("name").Text()+" resolves to "+address.String()+", but the service listens only on spec.bindAddress "+bind.String()+", so nothing answers there",
				"set spec.bindAddress to "+address.String()+" or to a wildcard (0.0.0.0 or ::), or name the address "+bind.String()+" in addressRef"))
		}
	}
	return issues
}

// validateWildcardProbes requires the endpoints readiness probes a service
// through when its bind address names no one address to probe.
func validateWildcardProbes(kind api.Kind, value api.Value) []api.Issue {
	bind := value.Get("bindAddress").Text()
	if !wildcard(bind) {
		return nil
	}
	host, endpoints := placementOf(value), value.Get("endpoints").Items()
	if kind != api.ArtifactServer {
		if len(endpoints) != 0 {
			return nil
		}
		return []api.Issue{issue("$.spec.endpoints", "a service bound to the wildcard "+bind+" needs at least one endpoint, because readiness proves it through its endpoint addresses",
			"add spec.endpoints naming an address of "+host+", or set spec.bindAddress to one of its IP addresses")}
	}
	issues := []api.Issue{}
	for index, listener := range value.Get("listeners").Items() {
		name := listener.Get("name").Text()
		if slices.ContainsFunc(endpoints, func(endpoint api.Value) bool { return endpoint.Get("listenerRef").Text() == name }) {
			continue
		}
		issues = add(issues, issue(fmt.Sprintf("$.spec.listeners[%d]", index), "listener "+name+" has no endpoint, so readiness under the wildcard bind "+bind+" can never prove it",
			"add an endpoint with listenerRef: "+name+", remove the listener, or set spec.bindAddress to an IP address of "+host))
	}
	return issues
}

func wildcard(bind string) bool { return bind == "0.0.0.0" || bind == "::" }

// boundAddress is the one IP address a service listens on, which a wildcard
// bind does not name.
func boundAddress(value api.Value) (netip.Addr, bool) {
	text := value.Get("bindAddress").Text()
	if wildcard(text) {
		return netip.Addr{}, false
	}
	address, err := netip.ParseAddr(text)
	return address, err == nil
}

func placementOf(value api.Value) string {
	if machine := value.Get("machineRef").Text(); machine != "" {
		return "Machine/" + machine
	}
	return "the placement Machine"
}

// machineAddress resolves a Machine-local address reference as machine.Address
// does, to the first entry of that name without its prefix; this package
// cannot import internal/machine, which imports it.
func machineAddress(machine api.Object, reference string) string {
	for _, address := range machine.Spec().Get("network", "addresses").Items() {
		if address.Get("name").Text() == reference {
			value, _, _ := strings.Cut(address.Get("address").Text(), "/")
			return value
		}
	}
	return ""
}

func validateIntrinsic(o api.Object, partial bool) []api.Issue {
	if !IsService(o.Kind()) {
		return nil
	}
	value := o.Spec()
	issues := []api.Issue{}
	mode := value.Get("management").Text()
	require := func(field string) {
		if !partial && !value.Has(field) {
			issues = add(issues, issue("$.spec."+field, "selected management mode requires this field", requirement(o.Kind(), field)))
		}
	}
	forbid := func(fields ...string) {
		for _, field := range fields {
			if value.Has(field) {
				issues = add(issues, issue("$.spec."+field, "field is not allowed for the selected management mode", "remove spec."+field+", which management: "+mode+" does not use"))
			}
		}
	}
	switch mode {
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
				issues = add(issues, issue("$.spec.connection", "external Proxy requires at least one proxy URL", "set spec.connection.httpProxy or spec.connection.httpsProxy"))
			}
		case api.DNSServer, api.NTPServer:
			require("address")
		case api.Registry:
			require("url")
		case api.ArtifactServer:
			require("endpoints")
			if value.Has("endpoints") && value.Get("endpoints").Len() == 0 {
				issues = add(issues, issue("$.spec.endpoints", "external ArtifactServer requires at least one endpoint", "add a spec.endpoints entry with a url"))
			}
		}
	}
	issues = add(issues, validateImagePins(value, mode, partial)...)
	issues = add(issues, validatePort(o.Kind(), value)...)
	if o.Kind() == api.LoadBalancer {
		issues = add(issues, validateBindAddressNames(value)...)
	}
	if o.Kind() == api.ArtifactServer {
		issues = add(issues, validateArtifactEndpoints(value, partial)...)
		issues = add(issues, validateTransport(value, partial)...)
	}
	return issues
}

func requirement(kind api.Kind, field string) string {
	if field != "implementation" {
		return "set spec." + field
	}
	if implementation, ok := api.Schema(kind).Field(field); ok && len(implementation.Shape.Enums) == 1 {
		return "set spec.implementation: " + implementation.Shape.Enums[0]
	}
	return "set spec.implementation"
}

// validateImagePins refuses a tag, because planning resolves no reference and
// so runs only an image pinned by content digest. A value outside the image
// grammar gets only that grammar's diagnostic.
func validateImagePins(value api.Value, mode string, partial bool) []api.Issue {
	image := value.Get("image")
	if !image.Present() {
		return nil
	}
	issues := []api.Issue{}
	if !image.Has("local") && !image.Has("public") && !partial {
		issues = add(issues, issue("$.spec.image", "an image pin must supply local or public intent", "set spec.image.local or spec.image.public, or remove spec.image"))
	}
	if mode == "external" {
		return issues
	}
	for _, field := range []string{"local", "public"} {
		pin := image.Get(field)
		if pin.Type() == api.String && api.ValidLexical("image", pin.Text()) && !strings.Contains(pin.Text(), "@") {
			issues = add(issues, issue("$.spec.image."+field, "a managed service image is pinned by content digest, because planning cannot resolve a tag",
				"write spec.image."+field+" as <repository>@sha256:<64 hex digits>"))
		}
	}
	return issues
}

func validatePort(kind api.Kind, value api.Value) []api.Issue {
	port := value.Get("port")
	switch {
	case kind == api.DNSServer && port.Present() && port.Text() != "53":
		return []api.Issue{issue("$.spec.port", "DNS service requires port 53", "set spec.port to 53 or omit it")}
	case kind == api.NTPServer && port.Present() && port.Text() != "123":
		return []api.Issue{issue("$.spec.port", "an NTPServer listens on port 123, because its consumers configure time servers by address alone", "set spec.port to 123 or omit it")}
	}
	return nil
}

func validateBindAddressNames(value api.Value) []api.Issue {
	issues := []api.Issue{}
	seen := map[string]bool{}
	addresses := value.Get("bindAddresses").Items()
	for index, address := range addresses {
		field, name := fmt.Sprintf("$.spec.bindAddresses[%d].name", index), address.Get("name").Text()
		if len(addresses) > 1 && name == "" {
			issues = add(issues, issue(field, "multiple load-balancer addresses require distinct names", "name every spec.bindAddresses entry"))
		}
		if name != "" && seen[name] {
			issues = add(issues, issue(field, "load-balancer bind-address names must be unique", fmt.Sprintf("give spec.bindAddresses[%d] a name no other entry uses", index)))
		}
		seen[name] = true
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
				issues = add(issues, issue(field+"."+name, "artifact endpoint requires this field for its management mode", "set "+strings.TrimPrefix(field, "$.")+"."+name))
			}
		}
		for _, name := range forbidden {
			if endpoint.Has(name) {
				issues = add(issues, issue(field+"."+name, "artifact endpoint field conflicts with its management mode", "remove "+strings.TrimPrefix(field, "$.")+"."+name))
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
			issues = add(issues, issue(fmt.Sprintf("$.spec.listeners[%d].port", index), "artifact listener ports must be unique", fmt.Sprintf("give spec.listeners[%d] a port no other listener uses", index)))
		}
		ports[port] = true
	}
	if https && !value.Has("tls") && !partial {
		issues = add(issues, issue("$.spec.tls", "HTTPS artifact listeners require TLS configuration", "set spec.tls.secretRef to a tlsCertificate Secret"))
	} else if !https && value.Has("tls") {
		issues = add(issues, issue("$.spec.tls", "HTTP-only artifact listeners forbid TLS configuration", "remove spec.tls, or add an https listener"))
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

func issue(field, message, remediation string) api.Issue {
	return api.Issue{Code: "api.invariant", Field: field, Message: message, Remediation: remediation}
}

func reference(field, message, remediation string) api.Issue {
	i := issue(field, message, remediation)
	i.Code = "api.reference"
	return i
}

func add(issues []api.Issue, more ...api.Issue) []api.Issue {
	return append(issues, more[:min(len(more), 999-len(issues))]...)
}
