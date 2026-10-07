package managedservice

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// PlacementRefusals refuses a managed service whose placement Machine this
// executable cannot reach or whose image it cannot acquire, so the operation
// refuses before registration instead of at plan. It reads no material. A
// placement admission already refuses is admission's alone, and without one
// Environment no controller exempts a Machine from the SSH rows.
func PlacementRefusals(catalog api.Catalog, service api.Object) []lifecycle.Refusal {
	machine, found := catalog.Find(api.Machine, service.Spec().Get("machineRef").Text())
	if !found {
		return nil
	}
	server, host := service.Identity(), machine.Identity()
	var refused []lifecycle.Refusal
	refuse := func(reason, remedy string) { refused = append(refused, lifecycle.RefusalOf(service, reason, remedy)) }
	choice := machine.Spec().Get("proxy")
	if proxy, found := catalog.Find(api.Proxy, choice.Get("proxyRef").Text()); choice.Has("proxyRef") && found {
		egress := proxy.Identity()
		connection := proxy.Spec().Get("connection")
		if proxy.Spec().Get("management").Text() != "external" {
			refuse("this executable acquires a managed service's image only directly or through an external Proxy that is already ready, and "+host+", where "+server+" runs, selects the managed "+egress+" in spec.proxy.proxyRef",
				"select direct: {} or an external Proxy in spec.proxy on "+host+", or place "+server+" on another Machine")
		} else {
			if connection.Has("auth", "proxyAuthRef") {
				refuse("this executable acquires a managed service's image through no authenticated proxy, and "+egress+", which "+host+" selects for "+server+", sets spec.connection.auth.proxyAuthRef",
					"select direct: {} or an external Proxy that needs no authentication in spec.proxy on "+host)
			}
			if connection.Has("trustBundleRef") {
				refuse("this executable acquires a managed service's image with the host's system trust store alone, and "+egress+", which "+host+" selects for "+server+", sets spec.connection.trustBundleRef",
					"select direct: {} or an external Proxy without spec.connection.trustBundleRef in spec.proxy on "+host)
			}
		}
	}
	controller, err := lifecycle.ControllerMachine(catalog)
	if err != nil || machine.Name() == controller {
		return refused
	}
	ssh := machine.Spec().Get("access", "ssh")
	keyRemedy := "author spec.access.ssh.auth.privateKeyRef and spec.access.ssh.knownHostsRef on " + host + ", or place " + server + " on the controller Machine"
	for _, field := range []string{"operatorIdentity", "passwordRef"} {
		if ssh.Has("auth", field) {
			refuse("this executable reaches a placement host other than the controller only with a bound SSH private key, and "+host+", where "+server+" runs, uses spec.access.ssh.auth."+field, keyRemedy)
		}
	}
	if ssh.Present() && !ssh.Has("knownHostsRef") {
		refuse("this executable connects to a placement host other than the controller only against a bound SSH host key, and "+host+", where "+server+" runs, declares no spec.access.ssh.knownHostsRef",
			"author spec.access.ssh.knownHostsRef on "+host+", or place "+server+" on the controller Machine")
	}
	return refused
}

// Unsupported refuses every managed service of this kind the capability
// cannot realize, with its reason and remedy, before registration.
func (c Capability) Unsupported(state *compilation.State) []lifecycle.Refusal {
	if state == nil {
		return nil
	}
	var refused []lifecycle.Refusal
	for _, service := range ManagedObjects(state.Effective(), c.definition.Kind) {
		refused = append(refused, PlacementRefusals(state.Effective(), service)...)
	}
	return lifecycle.SortRefusals(refused)
}
