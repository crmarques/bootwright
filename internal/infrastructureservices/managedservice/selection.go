package managedservice

import (
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

func ContentRoot(contextID, slug, service string) string {
	return ContentRootPrefix + "/" + contextID + "/" + slug + "/" + service
}

func UnitName(contextID, slug, service string) string {
	return UnitPrefix + "-" + contextID + "-" + slug + "-" + service
}

func ValidContextID(contextID string) bool {
	return strings.HasPrefix(contextID, "ctx-") && len(contextID) == 36
}

// PlacementFor selects the arm one service runs through. The controller is
// local; every other host must author the SSH access the operation binds.
func PlacementFor(machine api.Object, controllerMachine string) (Placement, error) {
	if machine.Name() == controllerMachine {
		return Placement{Connection: ConnectionLocal, Machine: machine.Name()}, nil
	}
	ssh := machine.Spec().Get("access", "ssh")
	if !ssh.Present() {
		return Placement{}, Refusal("lifecycle.state", "a managed service host must be the controller or declare SSH access", "place the service on the controller Machine, or author access.ssh on "+machine.Identity())
	}
	if ssh.Has("auth", "operatorIdentity") {
		return Placement{}, Refusal("lifecycle.state", "operator SSH identity is unsupported for managed service placement", "author access.ssh.auth.privateKeyRef on "+machine.Identity())
	}
	if ssh.Has("auth", "passwordRef") {
		return Placement{}, Refusal("lifecycle.state", "password SSH authentication is unsupported for managed service placement", "author access.ssh.auth.privateKeyRef on "+machine.Identity())
	}
	if !ssh.Has("auth", "privateKeyRef") {
		return Placement{}, Refusal("lifecycle.state", "managed service placement requires an SSH private key reference", "author access.ssh.auth.privateKeyRef on "+machine.Identity())
	}
	if !ssh.Has("knownHostsRef") {
		return Placement{}, Refusal("lifecycle.state", "managed service placement requires a bound SSH host key", "author access.ssh.knownHostsRef on "+machine.Identity())
	}
	address, err := MachineAddress(machine, ssh.Get("addressRef").Text())
	if err != nil {
		return Placement{}, err
	}
	port := 22
	if value, ok := ssh.Get("port").Int64(); ok && value > 0 {
		port = int(value)
	}
	user := ssh.Get("user").Text()
	if user == "" {
		user = "root"
	}
	return Placement{
		Address: address, Connection: ConnectionSSH, KnownHostsRef: ssh.Get("knownHostsRef").Text(),
		Machine: machine.Name(), Port: port, PrivateKeyRef: ssh.Get("auth", "privateKeyRef").Text(),
		SudoPasswordRef: ssh.Get("sudoPasswordRef").Text(), User: user,
	}, nil
}

// MachineAddress resolves a Machine-local address reference to the value a
// consumer receives: the host IP without its prefix, or the DNS name.
func MachineAddress(machine api.Object, reference string) (string, error) {
	if reference == "" {
		return "", Refusal("api.required", "the address reference is empty", "name an address on "+machine.Identity())
	}
	for _, address := range machine.Spec().Get("network", "addresses").Items() {
		if address.Get("name").Text() != reference {
			continue
		}
		value := address.Get("address").Text()
		if host, _, found := strings.Cut(value, "/"); found {
			value = host
		}
		if value == "" {
			return "", Refusal("api.value", "the referenced Machine address is empty", "correct the address on "+machine.Identity())
		}
		return value, nil
	}
	return "", Refusal("api.reference", "the address reference does not resolve on its Machine", "name an address declared on "+machine.Identity())
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
		address, err := MachineAddress(machine, item.Get("addressRef").Text())
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
