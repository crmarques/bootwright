package installation

import (
	"net/netip"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactserver"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/substrate"
)

// refusedNetwork says why the installation cannot install the network a
// Machine composes, or nothing when it can. The Kickstart configures the static
// IPv4 install address on its install interface, its default route and the
// selected name servers; network convergence applies every other composed
// content after installation, so this refuses only a missing static install
// address and an install interface the line cannot configure. It reads the
// composed network, so an override is read as the Machine realizes it; a
// network that does not compose is admission's refusal.
func refusedNetwork(catalog api.Catalog, machine api.Object, host installHost) (reason, remediation string) {
	composed, issues := machineref.ComposeNetwork(machine, catalog)
	if len(issues) != 0 {
		return "", ""
	}
	network := machine.Spec().Get("network")
	entry, found := findNamed(network.Get("addresses"), "name", network.Get("installAddressRef").Text())
	iface := entry.Get("interface").Text()
	prefix, err := netip.ParsePrefix(entry.Get("address").Text())
	if !found || iface == "" || err != nil || !prefix.Addr().Is4() {
		return "a managed-OS installation configures one static IPv4 install address, and the Machine selects none; DHCP installation is not supported",
			"assign an IPv4 address with its prefix to the install interface in spec.network.addresses of " + machine.Identity() +
				" and select it with spec.network.installAddressRef"
	}
	if !composed.Present() {
		return "", ""
	}
	template, err := substrate.NetworkTemplate(catalog, machine)
	if err != nil {
		return "", ""
	}
	return installInterfaceRefusal(catalog, machine, template, iface, host)
}

func available(iface api.Value) bool {
	state := iface.Get("state").Text()
	return state != "absent" && state != "ignore"
}

// refusedPublication says why the servers an installation publishes through
// cannot serve it, or nothing when they can. The installer image is built and
// the package tree extracted on the controller, where the controller stage
// provides the tooling, so a server placed on any other Machine would publish
// what was never built there. A virtual Machine's emulated controller is
// reached over verified HTTPS but fetches media without verifying
// the server, so that leg must never leave the provider host it runs on. A
// selection that does not resolve is the request builder's refusal.
func refusedPublication(catalog api.Catalog, machine, profile api.Object, derived substrate.Target, derivedOK bool, controllerMachine string) (reason, remediation string) {
	anaconda := profile.Spec().Get("installer", "anaconda")
	imageServer, imageErr := artifactserver.Selected(catalog, anaconda.Get("redfishVirtualMedia", "artifactServerEndpoint"), profile.Identity())
	imageHost := ""
	if imageErr == nil {
		imageHost = imageServer.Spec().Get("machineRef").Text()
	}
	if controllerMachine != "" {
		if imageHost != "" && imageHost != controllerMachine {
			return "a managed-OS installation builds and publishes its installer image on the controller, and " + imageServer.Identity() + " is placed on " + machineIdentity(imageHost),
				"place " + imageServer.Identity() + " on the controller Machine, or select a server placed there in spec.installer.anaconda.redfishVirtualMedia.artifactServerEndpoint on " + profile.Identity()
		}
		if source := anaconda.Get("packageSource", "hostedTree"); source.Present() {
			treeServer, treeErr := artifactserver.Selected(catalog, source.Get("artifactServerEndpoint"), profile.Identity())
			treeHost := ""
			if treeErr == nil {
				treeHost = treeServer.Spec().Get("machineRef").Text()
			}
			if treeHost != "" && treeHost != controllerMachine {
				return "a managed-OS installation extracts and publishes its package tree on the controller, and " + treeServer.Identity() + " is placed on " + machineIdentity(treeHost),
					"place " + treeServer.Identity() + " on the controller Machine, or select a server placed there in spec.installer.anaconda.packageSource.hostedTree.artifactServerEndpoint on " + profile.Identity()
			}
		}
	}
	if derivedOK && !derived.Physical && imageHost != "" && derived.PlacementMachine.Name() != imageHost {
		return "an emulated controller fetches the installer image without verifying its server, so the provider host it runs on is the Machine the artifact server is placed on",
			machine.Identity() + " is booted through a controller on " + derived.PlacementMachine.Identity() + " and " + imageServer.Identity() + " is placed on " +
				machineIdentity(imageHost) + "; place " + string(api.InfraProvider) + "/" + derived.Provider + " on " + machineIdentity(imageHost)
	}
	return "", ""
}

func machineIdentity(name string) string { return string(api.Machine) + "/" + name }
