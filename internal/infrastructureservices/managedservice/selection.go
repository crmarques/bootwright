package managedservice

import (
	machineref "github.com/crmarques/bootwright/internal/machine"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// ContentRootPrefix is outside the Bootwright state root, so an owned service
// directory can never expose context storage.
const ContentRootPrefix = "/var/lib/bootwright-services"

// UnitPrefix namespaces every host unit these capabilities own.
const UnitPrefix = "bootwright"

func ContentRoot(contextName, slug, service string) string {
	return ContentRootPrefix + "/" + contextName + "/" + slug + "/" + service
}

func UnitName(contextName, slug, service string) string {
	return UnitPrefix + "-" + contextName + "-" + slug + "-" + service
}

// ValidContextName repeats the workspace name grammar at this boundary, because
// the context name reaches a unit name and an owned path.
func ValidContextName(contextName string) bool {
	return api.ValidLexical("name", contextName)
}

// ImageFor selects the service image. Planning is pure, so it cannot resolve a
// tag to a digest and refuses any reference that is not already pinned.
func ImageFor(spec api.Value, identity, compiled string) (string, error) {
	reference := spec.Get("image", "local").Text()
	if reference == "" {
		reference = spec.Get("image", "public").Text()
	}
	if reference == "" {
		return compiled, nil
	}
	if !DigestPinned(reference) {
		return "", Refusal("api.value", "a managed service image must be pinned by content digest", "set spec.image to a reference ending in @sha256:<digest> on "+identity)
	}
	return reference, nil
}

func DigestPinned(reference string) bool {
	_, digest, found := strings.Cut(reference, "@sha256:")
	if !found || len(digest) != 64 {
		return false
	}
	return !strings.ContainsFunc(digest, func(c rune) bool {
		return !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f')
	})
}

// EndpointsFor derives the named addresses a consumer selects this service
// through, in canonical name order.
func EndpointsFor(spec api.Value, machine api.Object) ([]Endpoint, error) {
	items := spec.Get("endpoints").Items()
	endpoints := make([]Endpoint, 0, len(items))
	for _, item := range items {
		address, err := machineref.ResolveAddress(machine, item.Get("addressRef").Text())
		if err != nil {
			return nil, err
		}
		endpoints = append(endpoints, Endpoint{Address: address, Name: item.Get("name").Text()})
	}
	slices.SortFunc(endpoints, func(x, y Endpoint) int { return strings.Compare(x.Name, y.Name) })
	return endpoints, nil
}

// EgressFor uses the placement Machine's normalized proxy choice and nothing
// else: no ambient variable and no fallback to direct access.
func EgressFor(catalog api.Catalog, machine api.Object) (Egress, error) {
	egress := Egress{NoProxy: []string{}}
	choice := machine.Spec().Get("proxy")
	if !choice.Present() || choice.Has("direct") {
		return egress, nil
	}
	reference := choice.Get("proxyRef").Text()
	proxy, found := catalog.Find(api.Proxy, reference)
	if !found {
		return Egress{}, Refusal("api.reference", "the placement Machine's proxy does not resolve", "correct spec.proxy on "+machine.Identity())
	}
	if proxy.Spec().Get("management").Text() != "external" {
		return Egress{}, Refusal("lifecycle.state", "a managed service host must egress directly or through an already ready external Proxy", "select an external Proxy or direct access on "+machine.Identity())
	}
	connection := proxy.Spec().Get("connection")
	if connection.Has("auth", "proxyAuthRef") || connection.Has("trustBundleRef") {
		return Egress{}, Refusal("lifecycle.state", "proxy authentication and private trust are unsupported for service image acquisition", "select a proxy without authentication or private trust on "+machine.Identity())
	}
	egress.HTTPProxy = connection.Get("httpProxy").Text()
	egress.HTTPSProxy = connection.Get("httpsProxy").Text()
	if egress.HTTPProxy == "" && egress.HTTPSProxy == "" {
		return Egress{}, Refusal("api.value", "the selected external Proxy declares no proxy URL", "set connection.httpProxy or connection.httpsProxy on "+proxy.Identity())
	}
	for _, value := range choice.Get("noProxy").Strings() {
		egress.NoProxy = append(egress.NoProxy, value)
	}
	return egress, nil
}

// BindAddress reads the effective listener address, which admission has already
// defaulted and canonicalized.
func BindAddress(spec api.Value, identity string) (string, error) {
	value := spec.Get("bindAddress").Text()
	if value == "" {
		return "", Refusal("api.required", "the managed service has no effective bind address", "set spec.bindAddress on "+identity)
	}
	if _, err := netip.ParseAddr(value); err != nil {
		return "", Refusal("api.value", "the managed service bind address is not an IP literal", "set spec.bindAddress on "+identity)
	}
	return value, nil
}

func Port(spec api.Value, identity string) (int, error) {
	value, ok := spec.Get("port").Int64()
	if !ok || value < 1 || value > 65535 {
		return 0, Refusal("api.value", "the managed service port is out of range", "correct spec.port on "+identity)
	}
	return int(value), nil
}

// ManagedObjects lists the managed objects of one kind in canonical name order.
func ManagedObjects(catalog api.Catalog, kind api.Kind) []api.Object {
	var found []api.Object
	for _, object := range catalog.OfKind(kind) {
		if object.Spec().Get("management").Text() == "managed" {
			found = append(found, object)
		}
	}
	slices.SortFunc(found, func(x, y api.Object) int { return strings.Compare(x.Name(), y.Name()) })
	return found
}

func SafeSegment(value string) bool {
	if value == "" || len(value) > 63 || strings.ContainsAny(value, "/\x00 ") {
		return false
	}
	for index, c := range value {
		alphanumeric := c >= 'a' && c <= 'z' || c >= '0' && c <= '9'
		if !alphanumeric && !(c == '-' && index != 0 && index != len(value)-1) {
			return false
		}
	}
	return true
}

func FormatPort(value int) string {
	if value <= 0 {
		return "0"
	}
	return strconv.Itoa(value)
}

func Refusal(code, message, remediation string) error {
	return diagnostics.NewFailureWithRemediation(code, message, "", remediation)
}

// hostAddress keeps only an IP literal, dropping any prefix a Machine declared
// with it. A DNS contact is not an address a service can bind or answer with.
func hostAddress(value string) string {
	host, _, _ := strings.Cut(value, "/")
	if _, err := netip.ParseAddr(host); err != nil {
		return ""
	}
	return host
}

// hostPrefix renders one declared address as the single-host prefix an access
// list needs, whatever prefix length the Machine declared it with.
func hostPrefix(value string) string {
	host := hostAddress(value)
	if host == "" {
		return ""
	}
	address, err := netip.ParseAddr(host)
	if err != nil {
		return ""
	}
	return host + "/" + strconv.Itoa(address.BitLen())
}

// ServiceEndpoint resolves one managed service selection to the address a
// consumer reaches it at, and to the object whose block must complete first.
// It is the one reader of that grammar, so every consumer of a name or time
// service resolves the same selection to the same address.
func ServiceEndpoint(catalog api.Catalog, kind api.Kind, selection api.Value, identity string) (string, string, error) {
	reference := selection.Get("serverRef").Text()
	server, ok := catalog.Find(kind, reference)
	if !ok {
		return "", "", Refusal("api.reference", "a selected "+string(kind)+" is not in the selected graph", "declare "+reference+" or correct the selection on "+identity)
	}
	if server.Spec().Get("management").Text() != "managed" {
		return "", "", Refusal("lifecycle.state", "an installation uses only managed name and time services", "select a managed "+string(kind)+" on "+identity)
	}
	placement, ok := catalog.Find(api.Machine, server.Spec().Get("machineRef").Text())
	if !ok {
		return "", "", Refusal("api.reference", "the service's placement Machine is not in the selected graph", "declare it or correct machineRef on "+server.Identity())
	}
	endpoints := server.Spec().Get("endpoints")
	entry, found := namedEndpoint(endpoints, selection.Get("endpointRef").Text())
	if !found {
		return "", "", Refusal("api.reference", "a service selection names no endpoint", "set endpointRef on the selection of "+identity)
	}
	address, err := machineref.ResolveAddress(placement, entry.Get("addressRef").Text())
	if err != nil {
		return "", "", err
	}
	return address, server.Name(), nil
}

// namedEndpoint selects the named endpoint, or the only one when a selection
// names none.
func namedEndpoint(endpoints api.Value, name string) (api.Value, bool) {
	for _, entry := range endpoints.Items() {
		if entry.Get("name").Text() == name {
			return entry, true
		}
	}
	if items := endpoints.Items(); len(items) == 1 {
		return items[0], true
	}
	return api.Value{}, false
}
