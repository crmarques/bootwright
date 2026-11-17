package controller_test

import (
	"strconv"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func pinnedRefusal(err error) string {
	var lines []string
	for _, reported := range diagnostics.Of(err) {
		object := ""
		if reported.Object != nil {
			object = reported.Object.Kind + "/" + reported.Object.Name
		}
		lines = append(lines, strings.Join([]string{object, reported.Code, reported.Field, reported.Message, reported.Remediation}, " | "))
	}
	return strings.Join(lines, "\n")
}

// TestTheControllerMachineAndProxyRefusalsKeepTheirWords pins every Select
// refusal a controller Machine's own fields or its Proxy's fields raise, word
// for word, so reading them through the facts their contexts publish changes
// none of them.
func TestTheControllerMachineAndProxyRefusalsKeepTheirWords(t *testing.T) {
	withMachine := func(edit func(api.Value) api.Value) func([]api.Object) []api.Object {
		return func(objects []api.Object) []api.Object {
			objects[1] = objects[1].WithSpec(edit(objects[1].Spec()))
			return objects
		}
	}
	viaProxy := func(proxy api.Value, choice api.Value) func([]api.Object) []api.Object {
		return func(objects []api.Object) []api.Object {
			objects[1] = objects[1].WithSpec(objects[1].Spec().With("proxy", choice))
			return append(objects, api.NewObject(api.Proxy, "egress", api.Value{}, proxy))
		}
	}
	ref := api.MapValue().With("proxyRef", api.StringValue("egress"))
	external := func(connection api.Value) api.Value {
		return api.MapValue().With("management", api.StringValue("external")).With("connection", connection)
	}
	secure := api.MapValue().With("httpsProxy", api.StringValue("https://proxy.example.test:3128"))
	for _, test := range []struct {
		name string
		edit func([]api.Object) []api.Object
		want string
	}{
		{"os not provided", withMachine(func(spec api.Value) api.Value {
			return spec.With("os", api.MapValue().With("clusterInstalled", api.MapValue()))
		}),
			"Machine/controller | api.invariant | $.spec.os.provided | controller setup requires an OS-ready Machine with os.provided: {} | set spec.os.provided: {} on Machine/controller"},
		{"ssh access", withMachine(func(spec api.Value) api.Value {
			return spec.WithPath(api.StringValue("ip"), "access", "ssh", "addressRef")
		}), "Machine/controller | api.invariant | $.spec.access | controller setup requires access.local: {} and no SSH access | set spec.access.local: {} and remove spec.access.ssh on Machine/controller"},
		{"not local", withMachine(func(spec api.Value) api.Value { return spec.Without("access") }),
			"Machine/controller | api.invariant | $.spec.access | controller setup requires access.local: {} and no SSH access | set spec.access.local: {} and remove spec.access.ssh on Machine/controller"},
		{"proxy choice not a mapping", withMachine(func(spec api.Value) api.Value { return spec.With("proxy", api.StringValue("direct")) }),
			"Machine/controller | api.value | $.spec.proxy | controller setup requires a normalized proxy choice | set spec.proxy on Machine/controller to direct: {} or a proxyRef"},
		{"managed proxy", viaProxy(api.MapValue().With("management", api.StringValue("managed")).With("endpoints", api.ListValue(api.MapValue().With("name", api.StringValue("only")))), ref),
			"Machine/controller | controller.unsupported | $.spec.proxy.proxyRef | this executable's controller stage acquires only directly or through an external Proxy that is already ready, and Machine/controller selects the managed Proxy/egress | select direct: {} or an external Proxy in spec.proxy on Machine/controller"},
		{"authenticated proxy", viaProxy(external(secure.WithPath(api.StringValue("credentials"), "auth", "proxyAuthRef")), ref),
			"Proxy/egress | controller.unsupported | $.spec.connection.auth.proxyAuthRef | this executable's controller stage acquires through no authenticated proxy, and Proxy/egress sets spec.connection.auth.proxyAuthRef | select direct: {} or an external Proxy that needs no authentication in spec.proxy on Machine/controller"},
		{"privately trusted proxy", viaProxy(external(secure.With("trustBundleRef", api.StringValue("corporate-ca"))), ref),
			"Proxy/egress | controller.unsupported | $.spec.connection.trustBundleRef | this executable's controller stage trusts only the host's system trust store, and Proxy/egress sets spec.connection.trustBundleRef | select direct: {} or an external Proxy without spec.connection.trustBundleRef that the host's system trust store verifies in spec.proxy on Machine/controller"},
		{"http proxy alone", viaProxy(external(api.MapValue().With("httpProxy", api.StringValue("http://proxy.example.test:3128"))), ref),
			"Proxy/egress | controller.unsupported | $.spec.connection.httpsProxy | every dependency source is HTTPS, so the spec.connection.httpProxy of Proxy/egress alone selects no controller acquisition route | set connection.httpsProxy on Proxy/egress to the proxy endpoint; every dependency source is HTTPS"},
		{"endpoint not text", viaProxy(external(api.MapValue().With("httpsProxy", api.IntegerValue("3128"))), ref),
			"Proxy/egress | api.value | $.spec.connection.httpsProxy | the controller route requires spec.connection.httpsProxy to be a bare HTTP or HTTPS proxy endpoint | set spec.connection.httpsProxy on Proxy/egress to " + api.ProxyEndpointForm},
		{"no endpoint", viaProxy(external(api.MapValue()), ref),
			"Proxy/egress | api.value | $.spec.connection | external Proxy requires at least one HTTP(S) proxy URL | set spec.connection.httpsProxy on Proxy/egress to the proxy endpoint"},
		{"bypass not a sequence", viaProxy(external(secure), ref.With("noProxy", api.StringValue(".example.test"))),
			"Machine/controller | api.value | $.spec.proxy.noProxy | proxy bypass entries must be a sequence | write spec.proxy.noProxy on Machine/controller as a list of bypass entries"},
		{"repeated bypass", viaProxy(external(secure), ref.With("noProxy", api.StringList(".example.test", ".example.test"))),
			"Machine/controller | api.value | $.spec.proxy.noProxy[1] | proxy bypass entries must be distinct nonempty strings | remove the empty or repeated spec.proxy.noProxy[1] from Machine/controller"},
		{"direct with content", withMachine(func(spec api.Value) api.Value {
			return spec.With("proxy", api.MapValue().With("direct", api.MapValue().With("x", api.StringValue("y"))))
		}), "Machine/controller | api.value | $.spec.proxy.direct | direct acquisition requires an empty object | write spec.proxy.direct on Machine/controller as {}"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := controller.Select(api.NewCatalog(test.edit(selectionObjects())))
			if got := pinnedRefusal(err); got != test.want {
				t.Fatalf("refusal:\n%s\nwant:\n%s", got, test.want)
			}
		})
	}
}

// TestAManagedProxyOnAMirrorsMachineAndAnotherNameForTheControllerKeepTheirWords
// pins the two mirror refusals that read the mirror Machine's own fields: its
// managed Proxy, and SSH access at an address the controller Machine declares.
func TestAManagedProxyOnAMirrorsMachineAndAnotherNameForTheControllerKeepTheirWords(t *testing.T) {
	artifacts := api.MapValue().With("artifacts", api.MapValue().With("serverRef", api.StringValue("mirror")).With("endpointRef", api.StringValue("ip")))
	server := func() api.Object {
		object := pinnedArtifactServer("")
		return object.WithSpec(object.Spec().With("machineRef", api.StringValue("mirror-host")))
	}
	addresses := api.ListValue(api.MapValue().With("name", api.StringValue("ip")).With("address", api.StringValue("192.0.2.10")))
	host := func(proxy api.Value, sshAddress string, port int64) api.Object {
		spec := api.MapValue().WithPath(addresses, "network", "addresses").With("proxy", proxy)
		if sshAddress != "" {
			spec = spec.WithPath(api.MapValue().With("addressRef", api.StringValue("own")).With("port", api.IntegerValue(strconv.FormatInt(port, 10))), "access", "ssh").
				WithPath(api.ListValue(addresses.Items()[0], api.MapValue().With("name", api.StringValue("own")).With("address", api.StringValue(sshAddress))), "network", "addresses")
		}
		return api.NewObject(api.Machine, "mirror-host", api.Value{}, spec)
	}
	managed := api.NewObject(api.Proxy, "egress", api.Value{}, api.MapValue().With("management", api.StringValue("managed")))
	for _, test := range []struct {
		name    string
		objects []api.Object
		want    []pinnedShape
	}{
		{"managed proxy", []api.Object{server(), host(api.MapValue().With("proxyRef", api.StringValue("egress")), "", 0), managed}, []pinnedShape{{"Machine/mirror-host", "$.spec.proxy.proxyRef",
			"the artifact mirror on Machine/mirror-host is filled before any managed service runs, and Machine/mirror-host reaches the publishers through the managed Proxy/egress",
			"select direct: {} or an external Proxy in spec.proxy on Machine/mirror-host"}}},
		{"another name for the controller", []api.Object{server(), host(api.MapValue().With("direct", api.MapValue()), "192.0.2.10", 22)}, []pinnedShape{{"Environment/lab", "$.spec.mirror.artifacts.serverRef",
			"this executable places a managed artifact mirror on the controller host only through its controller Machine, and ArtifactServer/mirror is placed on Machine/mirror-host, another name for that host",
			"place ArtifactServer/mirror on Machine/controller or on another Machine reached over SSH"}}},
		{"a forwarded port", []api.Object{server(), host(api.MapValue().With("direct", api.MapValue()), "192.0.2.10", 2222)}, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			var got []pinnedShape
			for _, shape := range controller.Unsupported(api.NewCatalog(mirrorGraph(artifacts, "192.0.2.1", test.objects...))) {
				if shape.Field == "$.spec.mirror.artifacts" {
					continue
				}
				got = append(got, pinnedShape{shape.Object.Identity(), shape.Field, shape.Reason, shape.Remediation})
			}
			if len(got) != len(test.want) || len(got) != 0 && got[0] != test.want[0] {
				t.Fatalf("shapes:\n%+v\nwant:\n%+v", got, test.want)
			}
		})
	}
}
