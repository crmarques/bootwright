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

// unsupportedSelections are the profile choices that carry secret bytes or
// effects this contract does not prove. A selected one refuses before
// registration rather than installing part of what was declared.
var unsupportedSelections = [][]string{
	{"installer", "anaconda", "packageSource", "mirror"},
	{"installer", "anaconda", "packageSource", "fromSubscription"},
	{"subscription"},
	{"customizations", "ssh", "initialPassword"},
	{"customizations", "security", "diskEncryption"},
}

// refusedProfile says why a profile selects what this contract cannot
// install, or nothing when it can. FIPS is read rather than detected, because
// a profile that declares it disabled has selected nothing, while one that
// enables it would otherwise install a machine that is not in the mode it
// asked for.
func refusedProfile(profile api.Object) (reason, remediation string) {
	spec := profile.Spec()
	if !spec.Has("installer", "anaconda") {
		return "this executable installs an operating system only through the anaconda installer, which the install profile does not select",
			"select spec.installer.anaconda on " + profile.Identity()
	}
	for _, path := range unsupportedSelections {
		if spec.Has(path...) {
			field := "spec." + strings.Join(path, ".")
			return "the install profile selects " + field + ", which carries secret bytes or effects this executable does not prove",
				"remove " + field + " from " + profile.Identity()
		}
	}
	if spec.Get("customizations", "ssh", "passwordAuthentication").Bool() {
		return "the install profile enables spec.customizations.ssh.passwordAuthentication, but no password can be set while spec.customizations.ssh.initialPassword is refused",
			"set spec.customizations.ssh.passwordAuthentication to false on " + profile.Identity()
	}
	if spec.Get("customizations", "security", "fips", "enabled").Bool() {
		return "the install profile enables FIPS, which carries effects this executable does not prove",
			"disable spec.customizations.security.fips on " + profile.Identity()
	}
	return "", ""
}

// Refusals refuses every installation this contract cannot realize. A target,
// its network, a profile arm or the servers it publishes through are refused
// in that order, and only the first refusal a Machine meets is named, through
// that Machine, because that is the object an operator removes or changes; its
// remedy names the field to change.
func Refusals(catalog api.Catalog) []lifecycle.Refusal {
	// A graph naming no controller leaves a physical target underived, and the
	// request builder refuses it for that reason instead.
	controllerMachine, _ := lifecycle.ControllerMachine(catalog)
	var found []lifecycle.Refusal
	for _, machine := range InstalledMachines(catalog) {
		// Nothing a target is refused for depends on the context, so none is
		// named here.
		derived, err := substrate.TargetFor(catalog, machine, "", controllerMachine)
		derivedOK := err == nil
		if derivedOK {
			if reason, remediation := refusedTarget(machine, derived); reason != "" {
				found = append(found, lifecycle.RefusalOf(machine, reason, remediation))
				continue
			}
		}
		profile, ok := catalog.Find(api.MachineInstallProfile, machine.Spec().Get("os", "installProfileRef").Text())
		if !ok {
			continue
		}
		// Only the anaconda installer has a Kickstart whose network line
		// could drop what the Machine declares; any other installer is
		// refused as the profile's own choice.
		if profile.Spec().Has("installer", "anaconda") {
			if reason, remediation := refusedNetwork(catalog, machine); reason != "" {
				found = append(found, lifecycle.RefusalOf(machine, reason, remediation))
				continue
			}
		}
		if reason, remediation := refusedProfile(profile); reason != "" {
			found = append(found, lifecycle.RefusalOf(machine, reason, remediation))
			continue
		}
		if reason, remediation := refusedProxy(catalog, machine, profile); reason != "" {
			found = append(found, lifecycle.RefusalOf(machine, reason, remediation))
			continue
		}
		if reason, remediation := refusedPublication(catalog, machine, profile, derived, derivedOK, controllerMachine); reason != "" {
			found = append(found, lifecycle.RefusalOf(machine, reason, remediation))
		}
	}
	return lifecycle.SortRefusals(found)
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
// managed resolver and time source the guest uses while installing. An
// external one is used at its declared address and is waited for by nothing.
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
	imageServer, image, private, certificate, err := installerPublication(catalog,
		anaconda.Get("redfishVirtualMedia", "artifactServerEndpoint"), target, contextName, name, profile.Identity())
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
	// the key travels only beneath the private subtree.
	if private != nil {
		if target.HostKeyRef == "" {
			return Request{}, Requirements{}, refusal("api.required", "the Machine declares no SSH host key for its installation to deliver",
				"set spec.os.install.hostKeyRef on "+machine.Identity())
		}
		keyType, err := hostKeyType(catalog, machine, target.HostKeyRef)
		if err != nil {
			return Request{}, Requirements{}, err
		}
		request.Target.HostKeyType = keyType
		request.Private, request.TLSCertificateRef = private, certificate
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
	return storeMedia(image.Spec().Get("bootMedia").Text(), image.Spec().Get("checksum").Text(), "spec.bootMedia", image.Identity())
}

// storeMedia reads one media store reference, and its remedy names the field
// that holds it on the object that declares it.
func storeMedia(reference, checksum, field, identity string) (Media, error) {
	name, local := strings.CutPrefix(reference, "local-media:")
	if !local || !managedos.ValidMediaName(name) {
		return Media{}, refusal("lifecycle.state", "this installation supports only media the host store holds",
			"import the image with bootwright media add --name <filename.iso> and set "+field+" to local-media:<filename.iso> on "+identity)
	}
	media := Media{Name: name}
	if checksum != "" {
		digest, ok := managedos.NormalizeMediaDigest(checksum)
		if !ok {
			return Media{}, refusal("api.value", "the declared media checksum is not a SHA-256 digest", "correct the checksum on "+identity)
		}
		media.declared = digest
	}
	return media, nil
}

// pinMedia freezes the store record of every image a request names, refusing
// one the store does not hold, one whose bytes no longer have the recorded
// size and one whose record differs from the digest its MachineImage declares.
func pinMedia(catalog api.Catalog, request Request, records map[string]MediaRecord) (Request, error) {
	profile, _ := catalog.Find(api.MachineInstallProfile, request.Identity.Profile)
	image, _ := catalog.Find(api.MachineImage, profile.Spec().Get("installer", "anaconda", "imageRef").Text())
	boot, err := pinned(request.BootMedia, records, image.Identity(), "spec.bootMedia", image.Identity())
	if err != nil {
		return Request{}, err
	}
	request.BootMedia = boot
	if request.TreeMedia != nil {
		tree, err := pinned(*request.TreeMedia, records, profile.Identity(), "spec.installer.anaconda.packageSource.hostedTree.fromMedia", image.Identity())
		if err != nil {
			return Request{}, err
		}
		request.TreeMedia = &tree
	}
	return request, nil
}

// pinned is one image frozen at its store record. owner and field name where
// the image is declared, and image the MachineImage whose checksum a declared
// digest came from.
func pinned(media Media, records map[string]MediaRecord, owner, field, image string) (Media, error) {
	record, ok := records[media.Name]
	if !ok {
		return Media{}, refusal("lifecycle.state", "the host media store holds no image "+media.Name+", which "+owner+" names in "+field,
			"import it with "+mediaAdd(media.Name, "")+", then repeat the command")
	}
	if record.Failure != "" {
		return Media{}, refusal("lifecycle.state", "the host media store cannot read its image "+media.Name+", which "+owner+" names in "+field+": "+record.Failure,
			"inspect it with bootwright media list, remove it with bootwright media delete --name "+media.Name+
				", import it again with "+mediaAdd(media.Name, "")+", then repeat the command")
	}
	if digest, ok := managedos.NormalizeMediaDigest(record.SHA256); !ok || digest == "" || digest != record.SHA256 || record.Size <= 0 {
		return Media{}, refusal("lifecycle.state", "the host media store's record of "+media.Name+" names no SHA-256 and size a plan can freeze",
			"inspect it with bootwright media list, remove it with bootwright media delete --name "+media.Name+
				", import it again with "+mediaAdd(media.Name, "")+", then repeat the command")
	}
	if record.Observed != record.Size {
		return Media{}, refusal("lifecycle.state", "the host media store's image "+media.Name+" no longer has the size its record names",
			"compare it with bootwright media list --checksums and import it again with "+mediaAdd(media.Name, ""))
	}
	if media.declared != "" && media.declared != record.SHA256 {
		return Media{}, refusal("lifecycle.state", "the host media store's image "+media.Name+" has SHA-256 "+record.SHA256+", not the "+
			media.declared+" spec.checksum declares on "+image,
			"import the declared image with "+mediaAdd(media.Name, media.declared)+", or correct spec.checksum on "+image)
	}
	media.SHA256, media.Size = record.SHA256, record.Size
	return media, nil
}

// mediaAdd is the import command a remedy names: media add always takes one
// source, and a digest the remedy knows verifies either.
func mediaAdd(name, digest string) string {
	if digest == "" {
		return "bootwright media add --name " + name + " --from-file <path>, or --from-url <url> --sha256 <digest>"
	}
	return "bootwright media add --name " + name + " --from-file <path> --sha256 " + digest + ", or --from-url <url> --sha256 " + digest
}

// mediaRefusals are the diagnostics an apply names for a store entry that no
// longer has the size and SHA-256 its operation froze, which the attempt
// proves before the entry's first use.
func mediaRefusals(contextName string, request Request) map[string]error {
	refused := func(name string) error {
		return &diagnostics.Failure{Diagnostics: []diagnostics.Diagnostic{{
			Severity: "error", Code: "lifecycle.state",
			Message: "the media store's image " + name + " no longer has the size and SHA-256 this operation froze, so nothing was published or booted",
			Remediation: "take this context back with bootwright destroy --context " + contextName + ", import the image again with " +
				mediaAdd(name, "") + ", then run bootwright apply --context " + contextName,
			Object: &diagnostics.ObjectIdentity{APIVersion: api.APIVersion, Kind: string(api.Machine), Name: request.Identity.Object},
		}}}
	}
	refusals := map[string]error{"media-changed-boot": refused(request.BootMedia.Name)}
	if request.TreeMedia != nil {
		refusals["media-changed-tree"] = refused(request.TreeMedia.Name)
	}
	return refusals
}

type hostedTree struct {
	media       *Media
	publication *Publication
}

func treeFor(catalog api.Catalog, source api.Value, contextName string, profile api.Object, needs *Requirements) (hostedTree, error) {
	media, err := storeMedia(source.Get("fromMedia").Text(), "", "spec.installer.anaconda.packageSource.hostedTree.fromMedia", profile.Identity())
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
// installerPublication is where one Machine's installer image is published:
// the server it is published through, and either the public image or the
// private subtree with the certificate its fetch verifies. A machine proved
// by a key this installation delivers boots an image whose Kickstart names
// the private URL of that key, so the image itself is published only beneath
// the private subtree and never publicly.
func installerPublication(catalog api.Catalog, selection api.Value, target Target, contextName, name, identity string) (api.Object, *Publication, *Publication, string, error) {
	if target.Channel != substrate.ChannelDeliveredKey {
		server, public, err := publicationFor(catalog, selection, contextName, name, "install.iso", identity)
		if err != nil {
			return api.Object{}, nil, nil, "", err
		}
		return server, &public, nil, "", nil
	}
	server, err := artifactserver.Selected(catalog, selection, identity)
	if err != nil {
		return api.Object{}, nil, nil, "", err
	}
	published, certificate, err := artifactserver.PrivatePath(catalog, server, selection, contextName, consumerPrefix, name, identity)
	if err != nil {
		return api.Object{}, nil, nil, "", err
	}
	return server, nil, &Publication{Path: published.Path, URL: published.URL}, certificate, nil
}

func publicationFor(catalog api.Catalog, selection api.Value, contextName, object, leaf, identity string) (api.Object, Publication, error) {
	server, err := artifactserver.Selected(catalog, selection, identity)
	if err != nil {
		return api.Object{}, Publication{}, err
	}
	published, err := artifactserver.PublicPath(catalog, server, selection, contextName, consumerPrefix, object, leaf, identity)
	if err != nil {
		return api.Object{}, Publication{}, err
	}
	publication := Publication{Path: published.Path, URL: published.URL}
	// An https publication is fetched through its listener before any machine
	// is given it, verified against the certificate its server presents.
	if strings.HasPrefix(published.URL, "https://") {
		publication.CertificateRef = server.Spec().Get("tls", "secretRef").Text()
		if publication.CertificateRef == "" {
			return api.Object{}, Publication{}, refusal("api.required", "the selected artifact server declares no serving certificate to verify",
				"set spec.tls.secretRef on "+server.Identity())
		}
	}
	return server, publication, nil
}

// targetFor reads the realized machine this installation acts on. Everything
// substrate-specific about the installation follows from the answer, so this
// is the one place the installation asks and it never asks which substrate.
func targetFor(catalog api.Catalog, machine api.Object, contextName, controllerMachine string) (Target, error) {
	derived, err := substrate.TargetFor(catalog, machine, contextName, controllerMachine)
	if err != nil {
		return Target{}, err
	}
	if reason, remediation := refusedTarget(machine, derived); reason != "" {
		return Target{}, refusal("lifecycle.unsupported", reason, remediation)
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
func refusedTarget(machine api.Object, target substrate.Target) (reason, remediation string) {
	// A physical machine already holds whatever it holds, and an installation
	// that names no disk leaves the installer to clear every one. A wwn is
	// admitted as a selector but not yet derived into a device the installer
	// and its own target proof can name.
	if target.Physical && target.RootDeviceHints.DeviceName == "" {
		return "a physical installation erases only a root device named by path, and the Machine names none",
			"set spec.os.install.rootDeviceHints.deviceName on " + machine.Identity() + "; a wwn-only selection is not yet supported"
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
		return "a managed-OS installation selects its root disk by deviceName alone and cannot carry the other root-device hints the Machine declares",
			"remove " + strings.Join(uncarried, ", ") + " from " + machine.Identity()
	}
	// A controller that fetches without verifying the artifact server boots
	// whatever image answers, and the installer it boots is what receives the
	// delivered key. Only a delivered-key target publishes privately, so this
	// is exactly the private consumer the exception is refused for.
	if target.Identity.Channel == substrate.ChannelDeliveredKey && target.Controller.VirtualMedia.Trust == substrate.TrustDisableVerification {
		return "a Machine that delivers private material through its installation cannot let its controller fetch without verifying the artifact server",
			"declare hardware.management.bmc.virtualMedia.tls.trust: import-certificate on " + machine.Identity() + ", or established when its controller already trusts the server"
	}
	// The controller is handed the tokenized URL of the private installer
	// image, so whoever answers or reads the controller leg holds the token.
	// A trust bundle implies verification, so https with verification is the
	// whole test.
	if target.Identity.Channel == substrate.ChannelDeliveredKey &&
		(!strings.HasPrefix(target.Controller.Endpoint, "https://") || !target.Controller.TLSVerify) {
		return "a Machine whose installation delivers private material hands its controller the private installer image URL, so the controller must be reached over https with its certificate verified",
			"address the controller of " + machine.Identity() + " with an https:// spec.hardware.management.bmc.address and remove the tls.verify opt-out on " +
				machine.Identity() + " or in spec.baremetal.defaults.bmc of InfraProvider/" + target.Provider +
				"; name the authority that issued its certificate in tls.trustBundleRef when the system trust store does not hold it"
	}
	return "", ""
}

// hostKeyType is the type of the key pair a delivered-key installation
// installs, which decides the path the Kickstart installs it at and the
// algorithm every connection pins. Only a generated declaration names its type
// before the material exists, so a key of any other source refuses here,
// before registration, rather than at the installer.
func hostKeyType(catalog api.Catalog, machine api.Object, reference string) (string, error) {
	secret, ok := catalog.Find(api.Secret, reference)
	if !ok {
		return "", refusal("api.reference", "the Machine's SSH host key Secret is not in the selected graph",
			"declare Secret/"+reference+" or correct spec.os.install.hostKeyRef on "+machine.Identity())
	}
	generated := secret.Spec().Get("source", "generated")
	if !generated.Present() {
		return "", refusal("lifecycle.unsupported",
			"a physical installation installs its delivered SSH host key at the path of the type its generated declaration names, and Secret/"+reference+" declares no generated source",
			"declare spec.source.generated.keyType on Secret/"+reference+", or name a generated sshKeyPair Secret in spec.os.install.hostKeyRef on "+machine.Identity())
	}
	keyType := generated.Get("keyType").Text()
	if _, known := deliveredHostKeys[keyType]; !known {
		return "", refusal("api.value", "Secret/"+reference+" generates no SSH host key type an installation can deliver",
			"declare spec.source.generated.keyType as ed25519, rsa, ecdsa-p256, ecdsa-p384 or ecdsa-p521 on Secret/"+reference)
	}
	return keyType, nil
}

// mediaTrustRefusal says why the controller cannot be made to trust the server
// it fetches the installer image from. Importing a certificate needs the image
// served over https and the certificate that server presents; there is no
// other trust to fall back to.
func mediaTrustRefusal(machine api.Object, request Request) error {
	if request.Target.Controller.VirtualMedia.Trust != substrate.TrustImportCertificate {
		return nil
	}
	if strings.HasPrefix(request.installerImage().URL, "https://") && request.TLSCertificateRef != "" {
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
