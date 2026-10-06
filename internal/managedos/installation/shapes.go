package installation

import (
	"net/netip"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactserver"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/substrate"
)

// carriedMTU is the one MTU an installed system takes without being told,
// so declaring it asks for nothing the installation leaves out.
const carriedMTU = 1500

// refusedNetwork says why the installation cannot carry the network a Machine
// composes, or nothing when it can. The Kickstart configures one static IPv4
// address on one ethernet device, its default route and the selected name
// servers; anything else the Machine declares would be dropped, so it is
// refused rather than installed in part. It reads the composed network, so an
// override is read as the Machine realizes it; a network that does not compose
// is admission's refusal.
func refusedNetwork(catalog api.Catalog, machine api.Object) (reason, remediation string) {
	composed, issues := machineref.ComposeNetwork(machine, catalog)
	if len(issues) != 0 {
		return "", ""
	}
	network := machine.Spec().Get("network")
	selected := network.Get("installAddressRef").Text()
	entry, found := findNamed(network.Get("addresses"), "name", selected)
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
	if declared, ok := findNamed(composed.Get("interfaces"), "name", iface); ok && declared.Get("type").Text() != "ethernet" {
		return "a managed-OS installation configures its install interface as one ethernet device and cannot carry a bonded, VLAN or other logical install interface",
			"assign the install address of " + machine.Identity() + " to an ethernet interface; a bonded or VLAN install interface is not yet supported"
	}
	if uncarried := uncarriedNetwork(catalog, machine, composed, selected, iface); len(uncarried) != 0 {
		return "a managed-OS installation configures only the install address, its default route and the selected name servers, and cannot carry the other network content the Machine declares",
			"remove " + strings.Join(uncarried, ", ") + " from the network of " + machine.Identity() + ", or install its operating system outside Bootwright"
	}
	return "", ""
}

// uncarriedNetwork names the composed network content the install line leaves
// out: every other assigned address, every other available logical interface,
// every MTU but the one the system takes anyway, and every route but the
// default route the line carries. Search domains, a disabled IPv6 family and
// the default route's own table and metric are accepted although the line
// does not carry them, until the installed system converges to its network.
func uncarriedNetwork(catalog api.Catalog, machine api.Object, composed api.Value, selected, iface string) []string {
	var uncarried []string
	for _, address := range machine.Spec().Get("network", "addresses").Items() {
		if address.Has("interface") && address.Get("name").Text() != selected {
			uncarried = append(uncarried, "address "+address.Get("name").Text())
		}
	}
	interfaces := composed.Get("interfaces").Items()
	for _, declared := range interfaces {
		name := declared.Get("name").Text()
		if name != iface && available(declared) && declared.Get("type").Text() != "ethernet" {
			uncarried = append(uncarried, "interface "+name)
		}
	}
	for _, declared := range interfaces {
		mtu := declared.Get("mtu")
		if value, ok := mtu.Int64(); available(declared) && mtu.Present() && (!ok || value != carriedMTU) {
			uncarried = append(uncarried, "mtu "+mtu.Text()+" on "+declared.Get("name").Text())
		}
	}
	// The line carries the gateway of the first default route the network
	// template declares, so only a route that is exactly that one is carried.
	gateway := ""
	if template, err := substrate.NetworkTemplate(catalog, machine); err == nil {
		gateway = substrate.DefaultGateway(template)
	}
	carried := false
	for _, route := range composed.Get("routes", "config").Items() {
		if route.Get("state").Text() == "absent" {
			continue
		}
		destination := route.Get("destination").Text()
		hop := route.Get("next-hop-interface")
		if !carried && destination == "0.0.0.0/0" && (!hop.Present() || hop.Text() == iface) && route.Get("next-hop-address").Text() == gateway {
			carried = true
			continue
		}
		uncarried = append(uncarried, "route "+destination)
	}
	return uncarried
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
// reached over plain HTTP with its credential and fetches without verifying
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
		return "an emulated controller is reached over plain HTTP with its credential and fetches the installer image without verifying its server, so the provider host it runs on is the Machine the artifact server is placed on",
			machine.Identity() + " is booted through a controller on " + derived.PlacementMachine.Identity() + " and " + imageServer.Identity() + " is placed on " +
				machineIdentity(imageHost) + "; place " + string(api.InfraProvider) + "/" + derived.Provider + " on " + machineIdentity(imageHost)
	}
	return "", ""
}

func machineIdentity(name string) string { return string(api.Machine) + "/" + name }
