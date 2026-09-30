package installation

import (
	"net/netip"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactserver"
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

// refusedCustomization reports whether a profile selects an arm this contract
// cannot install. FIPS is read rather than detected, because a profile that
// declares it disabled has selected nothing, while one that enables it would
// otherwise install a machine that is not in the mode it asked for.
func refusedCustomization(spec api.Value) bool {
	for _, path := range unsupportedCustomizations {
		if spec.Has(path...) {
			return true
		}
	}
	return spec.Get("customizations", "security", "fips", "enabled").Bool()
}

// Unsupported lists every installation this contract cannot realize. A profile
// arm or a target it refuses is named through the Machine that selects it,
// because that is the object an operator removes or changes.
func Unsupported(catalog api.Catalog) []string {
	// A graph naming no controller leaves a physical target underived, and the
	// request builder refuses it for that reason instead.
	controllerMachine, _ := lifecycle.ControllerMachine(catalog)
	var found []string
	for _, machine := range InstalledMachines(catalog) {
		// Nothing a target is refused for depends on the context, so none is
		// named here.
		derived, err := substrate.TargetFor(catalog, machine, "", controllerMachine)
		if err == nil && refusedTarget(machine, derived) != nil {
			found = append(found, machine.Identity())
			continue
		}
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
		if refusedCustomization(profile.Spec()) {
			found = append(found, machine.Identity())
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
	placement, err := artifactserver.PlacementFor(catalog, imageServer, controllerMachine)
	if err != nil {
		return Request{}, Requirements{}, err
	}
	needs := Requirements{ArtifactServers: []string{imageServer.Name()}, Machine: name}
	request := Request{
		Address:     "",
		BootMedia:   boot,
		Budgets:     installationBudgets,
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
	// A machine proved by a key this installation delivers needs that key, and
	// the key cannot travel in publicly served content.
	if target.Channel == substrate.ChannelDeliveredKey {
		published, certificate, err := artifactserver.PrivatePath(catalog, imageServer,
			anaconda.Get("redfishVirtualMedia", "artifactServerEndpoint"), contextName, consumerPrefix, name, machine.Identity())
		if err != nil {
			return Request{}, Requirements{}, err
		}
		private := Publication{Path: published.Path, URL: published.URL}
		if target.HostKeyRef == "" {
			return Request{}, Requirements{}, refusal("api.required", "the Machine declares no SSH host key for its installation to deliver",
				"set spec.os.install.hostKeyRef on "+machine.Identity())
		}
		request.Private, request.TLSCertificateRef = &private, certificate
	}
	if err := mediaTrustRefusal(machine, request); err != nil {
		return Request{}, Requirements{}, err
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

// publicationFor is this block's own subtree beneath the selected server's
// served root, in the shape that server's publication contract fixes.
func publicationFor(catalog api.Catalog, selection api.Value, contextName, object, leaf, identity string) (api.Object, Publication, error) {
	server, err := artifactserver.Selected(catalog, selection, identity)
	if err != nil {
		return api.Object{}, Publication{}, err
	}
	published, err := artifactserver.PublicPath(catalog, server, selection, contextName, consumerPrefix, object, leaf, identity)
	if err != nil {
		return api.Object{}, Publication{}, err
	}
	return server, Publication{Path: published.Path, URL: published.URL}, nil
}

// targetFor reads the realized machine this installation acts on. Everything
// substrate-specific about the installation follows from the answer, so this
// is the one place the installation asks and it never asks which substrate.
func targetFor(catalog api.Catalog, machine api.Object, contextName, controllerMachine string) (Target, error) {
	derived, err := substrate.TargetFor(catalog, machine, contextName, controllerMachine)
	if err != nil {
		return Target{}, err
	}
	if err := refusedTarget(machine, derived); err != nil {
		return Target{}, err
	}
	target := Target{
		Channel: derived.Identity.Channel,
		Controller: Controller{
			CredentialsRef: derived.Controller.CredentialsRef,
			Endpoint:       derived.Controller.Endpoint,
			TLSVerify:      derived.Controller.TLSVerify,
			TrustBundleRef: derived.Controller.TrustBundleRef,
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
	// Only a physical machine proves itself by hardware address. A machine its
	// substrate created is addressed by the interface name that realization
	// fixed, so its derived interfaces are deliberately left out of what the
	// installation freezes.
	if derived.Physical {
		hardware := Hardware{RootDevice: derived.RootDeviceHints.DeviceName}
		for _, declared := range derived.Interfaces {
			hardware.Interfaces = append(hardware.Interfaces, Interface{MACAddress: declared.MACAddress, Name: declared.Name})
		}
		target.Hardware = &hardware
	}
	return target, nil
}

// refusedTarget says why this contract cannot install onto a Machine's
// realized target, or nothing when it can. It is the one statement of that
// refusal, so Unsupported and the request builder never disagree about it.
func refusedTarget(machine api.Object, target substrate.Target) error {
	// A physical machine already holds whatever it holds, and an installation
	// that names no disk leaves the installer to clear every one. A wwn is
	// admitted as a selector but not yet derived into a device the installer
	// and its own target proof can name.
	if target.Physical && target.RootDeviceHints.DeviceName == "" {
		return refusal("lifecycle.unsupported", "a physical installation erases only a root device named by path, and the Machine names none",
			"set spec.os.install.rootDeviceHints.deviceName on "+machine.Identity()+"; a wwn-only selection is not yet supported")
	}
	// The Kickstart selects its disk by name alone, so any other hint would be
	// ignored and the disk it selected installed over regardless.
	var uncarried []string
	for _, name := range target.RootDeviceHints.Names() {
		if name != "deviceName" {
			uncarried = append(uncarried, "spec.os.install.rootDeviceHints."+name)
		}
	}
	if len(uncarried) != 0 {
		return refusal("lifecycle.unsupported",
			"a managed-OS installation selects its root disk by deviceName alone and cannot carry the other root-device hints the Machine declares",
			"remove "+strings.Join(uncarried, ", ")+" from "+machine.Identity())
	}
	// A controller that fetches without verifying the artifact server boots
	// whatever image answers, and the installer it boots is what receives the
	// delivered key. Only a delivered-key target publishes privately, so this
	// is exactly the private consumer the exception is refused for.
	if target.Identity.Channel == substrate.ChannelDeliveredKey && target.Controller.VirtualMedia.Trust == substrate.TrustDisableVerification {
		return refusal("lifecycle.unsupported", "a Machine that delivers private material through its installation cannot let its controller fetch without verifying the artifact server",
			"declare hardware.management.bmc.virtualMedia.tls.trust: import-certificate on "+machine.Identity()+", or established when its controller already trusts the server")
	}
	// A delivered key reaches the installer at a tokenized URL the Kickstart
	// names, and the Kickstart is implanted in an installer image served
	// without authentication, so the token would protect nothing.
	if target.Identity.Channel == substrate.ChannelDeliveredKey {
		return refusal("lifecycle.unsupported", "a delivered host key would be readable from the publicly served installer image",
			"physical managed-OS installation is disabled until private delivery is repaired; remove "+machine.Identity()+
				" from the selected Environment or install its operating system outside Bootwright")
	}
	return nil
}

// mediaTrustRefusal says why the controller cannot be made to trust the server
// it fetches the installer image from. Importing a certificate needs the image
// served over https and the certificate that server presents; there is no
// other trust to fall back to.
func mediaTrustRefusal(machine api.Object, request Request) error {
	if request.Target.Controller.VirtualMedia.Trust != substrate.TrustImportCertificate {
		return nil
	}
	if strings.HasPrefix(request.Image.URL, "https://") && request.TLSCertificateRef != "" {
		return nil
	}
	return refusal("lifecycle.state", "importing the artifact server's certificate into the controller of "+machine.Identity()+" needs an https installer image and the certificate its server presents",
		"select an https artifactServerEndpoint on a server declaring spec.tls.secretRef")
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
