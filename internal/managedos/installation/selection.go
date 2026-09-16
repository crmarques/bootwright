package installation

import (
	"net/netip"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/managedos"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate"
)

// unsupportedCustomizations are the profile choices that carry secret bytes or
// effects this contract does not prove. A selected one refuses before
// registration rather than installing part of what was declared.
var unsupportedCustomizations = [][]string{
	{"customizations", "ssh", "initialPassword"},
	{"customizations", "security", "diskEncryption"},
}

// Unsupported lists every installation this contract cannot realize. A profile
// arm it refuses is named through the Machine that selects it, because that is
// the object an operator removes or changes.
func Unsupported(catalog api.Catalog) []string {
	var found []string
	for _, machine := range InstalledMachines(catalog) {
		profile, ok := catalog.Find(api.MachineInstallProfile, machine.Spec().Get("os", "installProfileRef").Text())
		if !ok {
			continue
		}
		if !profile.Spec().Has("installer", "anaconda") {
			found = append(found, machine.Identity())
			continue
		}
		source := profile.Spec().Get("installer", "anaconda", "packageSource")
		if source.Has("mirror") || source.Has("fromSubscription") {
			found = append(found, machine.Identity())
			continue
		}
		if profile.Spec().Has("subscription") {
			found = append(found, machine.Identity())
			continue
		}
		for _, path := range unsupportedCustomizations {
			if profile.Spec().Has(path...) {
				found = append(found, machine.Identity())
				break
			}
		}
	}
	slices.Sort(found)
	return slices.Compact(found)
}

// InstalledMachines lists the Machines whose operating system Bootwright
// installs, in canonical name order.
func InstalledMachines(catalog api.Catalog) []api.Object {
	var found []api.Object
	for _, machine := range catalog.OfKind(api.Machine) {
		provided := machine.Spec().Get("os", "provided")
		if provided.Type() != api.Boolean || provided.Bool() {
			continue
		}
		if machine.Spec().Get("os", "installProfileRef").Text() == "" {
			continue
		}
		found = append(found, machine)
	}
	slices.SortFunc(found, func(x, y api.Object) int { return strings.Compare(x.Name(), y.Name()) })
	return found
}

// Requirements are the API objects one installation waits for: the Machine's
// own realization, every artifact server it publishes through and every
// resolver and time source the guest uses while installing.
type Requirements struct {
	ArtifactServers []string
	DNSServers      []string
	Machine         string
	NTPServers      []string
}

// Requests derives one frozen request per Bootwright-installed Machine, in
// canonical object order. It reads no host, endpoint or Secret material.
func Requests(catalog api.Catalog, controllerMachine, contextName string) ([]Request, []Requirements, error) {
	if !api.ValidLexical("name", contextName) {
		return nil, nil, refusal("lifecycle.state", "the lifecycle context identity is invalid", "")
	}
	var requests []Request
	var requirements []Requirements
	for _, machine := range InstalledMachines(catalog) {
		request, needs, err := requestFor(catalog, machine, controllerMachine, contextName)
		if err != nil {
			return nil, nil, err
		}
		requests = append(requests, request)
		requirements = append(requirements, needs)
	}
	return requests, requirements, nil
}

func requestFor(catalog api.Catalog, machine api.Object, controllerMachine, contextName string) (Request, Requirements, error) {
	name := machine.Name()
	if !substrate.SafeSegment(name) {
		return Request{}, Requirements{}, refusal("lifecycle.state", "the Machine name is not a safe host identifier", "rename "+machine.Identity())
	}
	profile, ok := catalog.Find(api.MachineInstallProfile, machine.Spec().Get("os", "installProfileRef").Text())
	if !ok {
		return Request{}, Requirements{}, refusal("api.reference", "the Machine's install profile is not in the selected graph", "declare it or correct spec.os.installProfileRef on "+machine.Identity())
	}
	if !substrate.SafeSegment(profile.Name()) {
		return Request{}, Requirements{}, refusal("lifecycle.state", "the install profile name is not a safe host identifier", "rename "+profile.Identity())
	}
	anaconda := profile.Spec().Get("installer", "anaconda")
	boot, err := mediaFor(catalog, anaconda.Get("imageRef").Text(), profile.Identity())
	if err != nil {
		return Request{}, Requirements{}, err
	}
	target, err := targetFor(catalog, machine, contextName, controllerMachine)
	if err != nil {
		return Request{}, Requirements{}, err
	}
	imageServer, image, err := publicationFor(catalog, anaconda.Get("redfishVirtualMedia", "artifactServerEndpoint"), contextName, name, "install.iso", machine.Identity())
	if err != nil {
		return Request{}, Requirements{}, err
	}
	placement, err := serverPlacement(catalog, imageServer, controllerMachine)
	if err != nil {
		return Request{}, Requirements{}, err
	}
	needs := Requirements{ArtifactServers: []string{imageServer.Name()}, Machine: name}
	request := Request{
		Address:     "",
		BootMedia:   boot,
		FleetKeyRef: fleetKeyRef(catalog),
		HostKeyPath: HostKeyPath,
		Identity:    Identity{Block: BlockID(name), Context: contextName, Object: name, Profile: profile.Name()},
		Image:       image,
		MarkerPath:  MarkerPath,
		Placement:   placement,
		Target:      target,
		User:        installUser,
		Version:     requestVersion,
	}
	if request.FleetKeyRef == "" {
		return Request{}, Requirements{}, refusal("api.required", "the Environment declares no fleet access key", "set spec.remoteMachinesAccessKey.keyRef")
	}
	source := anaconda.Get("packageSource")
	if source.Has("hostedTree") {
		tree, err := treeFor(catalog, source.Get("hostedTree"), contextName, profile, &needs)
		if err != nil {
			return Request{}, Requirements{}, err
		}
		request.Tree, request.TreeMedia = tree.publication, tree.media
	}
	install, err := installationFor(catalog, machine, profile, request, &needs)
	if err != nil {
		return Request{}, Requirements{}, err
	}
	request.Address, request.Hostname = install.Address, install.Hostname
	kickstart, err := RenderKickstart(install)
	if err != nil {
		return Request{}, Requirements{}, err
	}
	request.Kickstart = kickstart
	needs.ArtifactServers = SortedUnique(needs.ArtifactServers)
	needs.DNSServers, needs.NTPServers = SortedUnique(needs.DNSServers), SortedUnique(needs.NTPServers)
	return request, needs, nil
}

// installUser is the product-owned account every installed Machine carries.
const installUser = "bootwright"

func fleetKeyRef(catalog api.Catalog) string {
	environments := catalog.OfKind(api.Environment)
	if len(environments) != 1 {
		return ""
	}
	return environments[0].Spec().Get("remoteMachinesAccessKey", "keyRef").Text()
}

// mediaFor resolves one MachineImage to the store entry it names. Only store
// media is supported here, because a lifecycle installation serves exactly what
// the operator imported and proved.
func mediaFor(catalog api.Catalog, reference, identity string) (Media, error) {
	image, ok := catalog.Find(api.MachineImage, reference)
	if !ok {
		return Media{}, refusal("api.reference", "the profile's boot image is not in the selected graph", "declare "+reference+" or correct imageRef on "+identity)
	}
	return storeMedia(image.Spec().Get("bootMedia").Text(), image.Spec().Get("checksum").Text(), image.Identity())
}

func storeMedia(reference, checksum, identity string) (Media, error) {
	name, local := strings.CutPrefix(reference, "local-media:")
	if !local || !managedos.ValidMediaName(name) {
		return Media{}, refusal("lifecycle.state", "this installation supports only media the host store holds", "import the image with bootwright media add and reference it as local-media:<name> on "+identity)
	}
	media := Media{Name: name}
	if checksum != "" {
		digest, ok := managedos.NormalizeMediaDigest(checksum)
		if !ok {
			return Media{}, refusal("api.value", "the declared media checksum is not a SHA-256 digest", "correct the checksum on "+identity)
		}
		media.SHA256 = digest
	}
	return media, nil
}

type hostedTree struct {
	media       *Media
	publication *Publication
}

func treeFor(catalog api.Catalog, source api.Value, contextName string, profile api.Object, needs *Requirements) (hostedTree, error) {
	media, err := storeMedia(source.Get("fromMedia").Text(), "", profile.Identity())
	if err != nil {
		return hostedTree{}, err
	}
	server, publication, err := publicationFor(catalog, source.Get("artifactServerEndpoint"), contextName, profile.Name(), "tree", profile.Identity())
	if err != nil {
		return hostedTree{}, err
	}
	needs.ArtifactServers = append(needs.ArtifactServers, server.Name())
	return hostedTree{media: &media, publication: &publication}, nil
}

// publicationFor derives the owned subtree and the URL a consumer fetches it
// at, from the exact managed endpoint the profile selects.
func publicationFor(catalog api.Catalog, selection api.Value, contextName, object, leaf, identity string) (api.Object, Publication, error) {
	server, ok := catalog.Find(api.ArtifactServer, selection.Get("serverRef").Text())
	if !ok {
		return api.Object{}, Publication{}, refusal("api.reference", "the selected artifact server is not in the selected graph", "declare it or correct artifactServerEndpoint.serverRef on "+identity)
	}
	if server.Spec().Get("management").Text() != "managed" {
		return api.Object{}, Publication{}, refusal("lifecycle.state", "an installation publishes only into a managed artifact server", "select a managed server on "+identity)
	}
	base, err := endpointURL(catalog, server, selection.Get("endpointRef").Text(), identity)
	if err != nil {
		return api.Object{}, Publication{}, err
	}
	root := contentRoot(contextName, server.Name())
	return server, Publication{
		Path: root + "/" + servedRoot + "/" + consumerPrefix + "/" + object + "/" + leaf,
		URL:  base + "/" + consumerPrefix + "/" + object + "/" + leaf,
	}, nil
}

// contentRoot repeats the artifact server's own owned layout, because a
// consumer publishes beneath the root that server created.
func contentRoot(contextName, server string) string {
	return "/var/lib/bootwright-services/" + contextName + "/artifact-server/" + server
}

func endpointURL(catalog api.Catalog, server api.Object, endpointRef, identity string) (string, error) {
	endpoint, ok := findNamed(server.Spec().Get("endpoints"), "name", endpointRef)
	if !ok {
		return "", refusal("api.reference", "the selected artifact server endpoint does not resolve", "correct artifactServerEndpoint.endpointRef on "+identity)
	}
	listener, ok := findNamed(server.Spec().Get("listeners"), "name", endpoint.Get("listenerRef").Text())
	if !ok {
		return "", refusal("api.reference", "the selected endpoint names no listener on its server", "correct the endpoint on "+server.Identity())
	}
	machine, ok := catalog.Find(api.Machine, server.Spec().Get("machineRef").Text())
	if !ok {
		return "", refusal("api.reference", "the artifact server's placement Machine is not in the selected graph", "declare it or correct machineRef on "+server.Identity())
	}
	address, err := lifecycle.MachineAddress(machine, endpoint.Get("addressRef").Text())
	if err != nil {
		return "", err
	}
	port, ok := listener.Get("port").Int64()
	if !ok || port < 1 {
		return "", refusal("api.value", "the selected listener declares no port", "correct the listener on "+server.Identity())
	}
	return listener.Get("protocol").Text() + "://" + address + ":" + substrate.FormatPort(int(port)), nil
}

func serverPlacement(catalog api.Catalog, server api.Object, controllerMachine string) (lifecycle.Placement, error) {
	machine, ok := catalog.Find(api.Machine, server.Spec().Get("machineRef").Text())
	if !ok {
		return lifecycle.Placement{}, refusal("api.reference", "the artifact server's placement Machine is not in the selected graph", "declare it or correct machineRef on "+server.Identity())
	}
	return lifecycle.PlacementFor(machine, controllerMachine)
}

// targetFor reads the realized machine this installation acts on. Everything
// substrate-specific about the installation follows from the answer, so this
// is the one place the installation asks and it never asks which substrate.
func targetFor(catalog api.Catalog, machine api.Object, contextName, controllerMachine string) (Target, error) {
	derived, err := substrate.TargetFor(catalog, machine, contextName, controllerMachine)
	if err != nil {
		return Target{}, err
	}
	target := Target{
		Channel: derived.Identity.Channel,
		Controller: Controller{
			CredentialsRef: derived.Controller.CredentialsRef,
			Endpoint:       derived.Controller.Endpoint,
			TLSVerify:      derived.Controller.TLSVerify,
			VirtualMedia: VirtualMedia{
				RemoveCertificate:   derived.Controller.VirtualMedia.RemoveCertificate,
				RestoreVerification: derived.Controller.VirtualMedia.RestoreVerification,
				Trust:               derived.Controller.VirtualMedia.Trust,
			},
		},
		Domain:     derived.Identity.Domain,
		HostKeyRef: derived.Identity.HostKeyRef,
		Physical:   derived.Physical,
		Substrate:  derived.Substrate,
		URI:        derived.Identity.URI,
	}
	if derived.Controller.VirtualMedia.Trust == substrate.TrustImportCertificate {
		return Target{}, refusal("lifecycle.state", "importing a certificate into a management controller is not implemented",
			"select disable-verification or established virtual-media trust on "+machine.Identity())
	}
	if len(derived.Hardware.Interfaces) != 0 {
		hardware := Hardware{RootDevice: derived.Hardware.RootDevice}
		for _, declared := range derived.Hardware.Interfaces {
			hardware.Interfaces = append(hardware.Interfaces, Interface{MACAddress: declared.MACAddress, Name: declared.Name})
		}
		target.Hardware = &hardware
	}
	return target, nil
}

func findNamed(values api.Value, key, name string) (api.Value, bool) {
	if name == "" {
		return api.Value{}, false
	}
	for _, value := range values.Items() {
		if value.Get(key).Text() == name {
			return value, true
		}
	}
	return api.Value{}, false
}

func refusal(code, message, remediation string) error {
	return diagnostics.NewFailureWithRemediation(code, message, "", remediation)
}

// netmaskPrefix reads the prefix length an address assignment carries.
func netmaskPrefix(value string) (string, int, bool) {
	prefix, err := netip.ParsePrefix(value)
	if err != nil {
		return "", 0, false
	}
	return prefix.Addr().String(), prefix.Bits(), true
}
