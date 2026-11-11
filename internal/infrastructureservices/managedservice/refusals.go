package managedservice

import (
	"fmt"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate"
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
	if machineref.Installed(machine) {
		return append(refused, InstalledHostRefusals(catalog, service, machine)...)
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

func InstalledHostRefusals(catalog api.Catalog, service, machine api.Object) []lifecycle.Refusal {
	server, host := service.Identity(), machine.Identity()
	var refused []lifecycle.Refusal
	profile, found := catalog.Find(api.MachineInstallProfile, machine.Spec().Get("os", "installProfileRef").Text())
	if enabled := profile.Spec().Get("customizations", "security", "firewall", "enabled"); found && (enabled.Type() != api.Boolean || enabled.Bool()) {
		refused = append(refused, lifecycle.RefusalOf(service,
			"this executable opens no host firewall port for a managed service, and "+host+", where "+server+" runs, is installed by "+profile.Identity()+", which leaves its firewall enabled",
			"set spec.customizations.security.firewall.enabled: false on "+profile.Identity()+", or place "+server+" on an OS-ready Machine"))
	}
	if found && !slices.Contains(profile.Spec().Get("customizations", "packages", "install").Strings(), "podman") {
		refused = append(refused, lifecycle.RefusalOf(service,
			"a managed service runs as a container, and "+host+", where "+server+" runs, is installed by "+profile.Identity()+", which does not install podman",
			"add podman to spec.customizations.packages.install of "+profile.Identity()+", or place "+server+" on an OS-ready Machine"))
	}
	if mirror, declared := infrastructureservices.ImageMirrorOf(catalog); service.Kind() == api.Registry && declared && mirror.Managed && mirror.Registry.Name() == service.Name() {
		refused = append(refused, lifecycle.RefusalOf(service,
			"this executable fills a managed image mirror only on an OS-ready Machine or the controller, and "+server+" is on "+host+", which Bootwright installs",
			"place "+server+" on an OS-ready Machine reached over SSH, or select an external Registry in spec.mirror.registry.registryRef"))
	}
	if service.Kind() == api.ArtifactServer {
		mirror, declared := infrastructureservices.ArtifactMirrorOf(catalog)
		consumer, named := artifactConsumer(catalog, service.Name())
		switch {
		case !declared || !mirror.Managed || mirror.Server.Name() != service.Name():
			refused = append(refused, lifecycle.RefusalOf(service,
				"an ArtifactServer on a Machine Bootwright installs serves only the Environment's managed artifact mirror, and "+server+" on "+host+" is not the server spec.mirror.artifacts.serverRef selects",
				"place "+server+" on the controller Machine or on an OS-ready Machine reached over SSH"))
		case named:
			refused = append(refused, lifecycle.RefusalOf(service,
				"the artifact mirror on a Machine Bootwright installs is filled by its own fill alone, and "+consumer+" publishes into "+server+" on "+host+" through an artifactServerEndpoint",
				"select a server on the controller Machine or on an OS-ready Machine in that artifactServerEndpoint of "+consumer))
		}
		if provider, realized := machineref.Provider(machine, catalog); realized && (len(substrate.ProviderTools(provider)) != 0 || substrate.RealizesPhysicalNICs(provider)) {
			refused = append(refused, lifecycle.RefusalOf(service,
				"the artifact mirror is filled before the controller stage installs its clients, and "+host+", where "+server+" runs, is installed through "+provider.Identity()+", whose machine blocks this executable runs only after that stage",
				"place "+server+" on an OS-ready Machine or on one a libvirt provider realizes"))
		}
	}
	selections := map[api.Kind][]api.Value{api.NTPServer: profile.Spec().Get("ntp").Items()}
	if override := machine.Spec().Get("os", "install", "ntp"); override.Present() {
		selections[api.NTPServer] = override.Items()
	}
	if network, err := substrate.NetworkSpec(catalog, machine); err == nil {
		selections[api.DNSServer] = network.Get("dns").Items()
	}
	for _, selection := range selections[service.Kind()] {
		if selection.Get("serverRef").Text() == service.Name() {
			refused = append(refused, lifecycle.RefusalOf(service,
				"the installation of "+host+" uses "+server+", which runs on "+host+" and so answers only once that installation completes",
				"select a "+string(service.Kind())+" placed on another Machine for the installation of "+host+", or place "+server+" on another Machine"))
			break
		}
	}
	return refused
}

func artifactConsumer(catalog api.Catalog, server string) (string, bool) {
	for _, object := range catalog.Objects() {
		if selectsServer(object.Spec(), server) {
			return object.Identity(), true
		}
	}
	return "", false
}

func selectsServer(value api.Value, server string) bool {
	for _, field := range value.Fields() {
		if field.Name == "artifactServerEndpoint" && field.Value.Get("serverRef").Text() == server || selectsServer(field.Value, server) {
			return true
		}
	}
	return slices.ContainsFunc(value.Items(), func(item api.Value) bool { return selectsServer(item, server) })
}

// ReservationRefusals refuses a managed service whose claim exceeds the keys
// one controller host reservation holds, so the operation refuses before
// registration for state that bypassed admission. It reads no material.
func ReservationRefusals(catalog api.Catalog, service api.Object) []lifecycle.Refusal {
	machine, found := catalog.Find(api.Machine, service.Spec().Get("machineRef").Text())
	if !found || infrastructureservices.ReservationSockets(service, machine) <= infrastructureservices.MaxReservationSockets {
		return nil
	}
	if service.Kind() == api.ArtifactServer {
		return []lifecycle.Refusal{lifecycle.RefusalOf(service,
			fmt.Sprintf("an artifact server claims one host reservation key for each listener and, under a wildcard bindAddress, for each distinct endpoint address of each listener, beside its unit and its content root, within the 64 keys one reservation holds, so its listeners and endpoints claim at most %d sockets", infrastructureservices.MaxReservationSockets),
			fmt.Sprintf("declare fewer listeners in spec.listeners or fewer endpoints with distinct addresses in spec.endpoints of %s, so they claim at most %d sockets", service.Identity(), infrastructureservices.MaxReservationSockets))}
	}
	return []lifecycle.Refusal{lifecycle.RefusalOf(service,
		fmt.Sprintf("a managed service bound to a wildcard claims one host reservation key for each distinct endpoint address beside its unit, its content root and its bind socket, within the 64 keys one reservation holds, so it declares at most %d distinct endpoint addresses", infrastructureservices.MaxReservationSockets-1),
		fmt.Sprintf("declare at most %d distinct endpoint addresses in spec.endpoints of %s", infrastructureservices.MaxReservationSockets-1, service.Identity()))}
}

// Unsupported refuses every managed service of this kind the capability
// cannot realize, with its reason and remedy, before registration.
func (c Capability) Unsupported(state *compilation.State) []lifecycle.Refusal {
	if state == nil {
		return nil
	}
	var refused []lifecycle.Refusal
	for _, service := range ManagedObjects(state.Effective(), c.definition) {
		refused = append(refused, PlacementRefusals(state.Effective(), service)...)
		refused = append(refused, ReservationRefusals(state.Effective(), service)...)
	}
	return lifecycle.SortRefusals(refused)
}
