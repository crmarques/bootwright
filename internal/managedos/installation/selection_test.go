package installation

import (
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/substrate"
)

func expectRefusal(t *testing.T, err error, code string) {
	t.Helper()
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != code {
		t.Fatalf("refusal = %#v, want %s", reported, code)
	}
}

func onlyRequest(t *testing.T, catalog api.Catalog) (Request, Requirements) {
	t.Helper()
	requests, requirements, err := Requests(catalog, "controller", testContext)
	if err != nil || len(requests) != 1 {
		t.Fatalf("requests = %d (%v)", len(requests), err)
	}
	return requests[0], requirements[0]
}

func TestRequestFreezesThePublishedContentAndItsURLs(t *testing.T) {
	request, _ := onlyRequest(t, labCatalog())
	if request.Identity.Block != "os-install-rhel-01" || request.Identity.Profile != "rhel-9-8" {
		t.Fatalf("identity = %+v", request.Identity)
	}
	root := "/var/lib/bootwright-services/lab/artifact-server/lab-artifacts/public"
	if request.Image.Path != root+"/os/rhel-01/install.iso" {
		t.Fatalf("image path = %q", request.Image.Path)
	}
	if request.Image.URL != "https://192.0.2.1:8443/os/rhel-01/install.iso" {
		t.Fatalf("image url = %q", request.Image.URL)
	}
	if request.Tree == nil || request.Tree.Path != root+"/os/rhel-9-8/tree" {
		t.Fatalf("tree = %+v", request.Tree)
	}
	if request.Tree.URL != "http://192.0.2.1:8080/os/rhel-9-8/tree" {
		t.Fatalf("tree url = %q", request.Tree.URL)
	}
	if request.BootMedia.Name != "rhel-9.8-x86_64-boot.iso" || request.TreeMedia.Name != "rhel-9.8-x86_64-dvd.iso" {
		t.Fatalf("media = %+v %+v", request.BootMedia, request.TreeMedia)
	}
	if !slices.Equal(request.MediaNames(), []string{"rhel-9.8-x86_64-boot.iso", "rhel-9.8-x86_64-dvd.iso"}) {
		t.Fatalf("media names = %v", request.MediaNames())
	}
}

// The Machine is booted through its own controller, at the endpoint its
// provider's allocation rule derives, and proved through the channel that
// substrate offers. The installation reads both from the derived target and
// names no substrate itself.
func TestRequestBootsThroughTheMachinesOwnController(t *testing.T) {
	request, _ := onlyRequest(t, labCatalog())
	if !strings.HasPrefix(request.Target.Controller.Endpoint, "http://192.0.2.1:8000/redfish/v1/Systems/") {
		t.Fatalf("endpoint = %q", request.Target.Controller.Endpoint)
	}
	if request.Target.Controller.CredentialsRef != "lab-bmc-credentials" {
		t.Fatalf("credentials = %q", request.Target.Controller.CredentialsRef)
	}
	if request.Target.Domain != "bootwright-lab-rhel-01" || request.Target.URI != "qemu:///system" {
		t.Fatalf("identity operation target = %q over %q", request.Target.Domain, request.Target.URI)
	}
	if request.Target.Channel != substrate.ChannelGuestAgent || request.Target.Physical {
		t.Fatalf("target = %+v", request.Target)
	}
	if request.Target.Hardware != nil {
		t.Fatal("a machine its substrate creates carries no hardware to prove")
	}
}

// Every service the guest uses while installing is a requirement, so those
// blocks complete before the installer needs them.
func TestRequirementsNameEveryServiceTheGuestUses(t *testing.T) {
	_, needs := onlyRequest(t, labCatalog())
	if needs.Machine != "rhel-01" {
		t.Fatalf("machine = %q", needs.Machine)
	}
	if !slices.Equal(needs.ArtifactServers, []string{"lab-artifacts"}) {
		t.Fatalf("artifact servers = %v", needs.ArtifactServers)
	}
	if !slices.Equal(needs.DNSServers, []string{"lab-dns"}) || !slices.Equal(needs.NTPServers, []string{"lab-ntp"}) {
		t.Fatalf("services = %v %v", needs.DNSServers, needs.NTPServers)
	}
}

// A Machine's own NTP override replaces the profile's list as a whole, which is
// what the API contract says it does.
func TestAMachineNTPOverrideReplacesTheProfileList(t *testing.T) {
	override := guest(field("os", guest().Spec().Get("os").With("install", api.MapValue(
		field("rootDeviceHints", api.MapValue(text("deviceName", "/dev/vda"))),
		field("ntp", api.ListValue()),
	))))
	request, needs := onlyRequest(t, labCatalog(override))
	if len(needs.NTPServers) != 0 {
		t.Fatalf("an empty override kept %v", needs.NTPServers)
	}
	if strings.Contains(request.Kickstart, "--ntpservers=") {
		t.Fatal("an empty override still rendered time sources")
	}
}

// The boot media must be an entry of the host store, because an installation
// serves exactly what the operator imported and proved.
func TestOnlyStoreMediaIsSupported(t *testing.T) {
	remote := api.NewObject(api.MachineImage, "rhel-9-8-boot", api.Value{}, api.MapValue(
		text("bootMedia", "https://example.test/boot.iso"),
		text("checksum", strings.Repeat("a", 64)),
	))
	_, _, err := Requests(labCatalog(remote), "controller", testContext)
	expectRefusal(t, err, "lifecycle.state")
}

// A declared checksum is canonicalized and compared with the store's record,
// never frozen in its place: the request freezes the record alone.
func TestADeclaredChecksumIsComparedNotFrozen(t *testing.T) {
	pinned := api.NewObject(api.MachineImage, "rhel-9-8-boot", api.Value{}, api.MapValue(
		text("bootMedia", "local-media:rhel-9.8-x86_64-boot.iso"),
		text("checksum", "SHA256:"+strings.Repeat("A", 64)),
	))
	request, _ := onlyRequest(t, labCatalog(pinned))
	if request.BootMedia.SHA256 != "" || request.BootMedia.declared != strings.Repeat("a", 64) {
		t.Fatalf("boot media = %+v", request.BootMedia)
	}
	records := labRecords()
	records[bootImageName] = MediaRecord{SHA256: strings.Repeat("a", 64), Size: 7, Observed: 7}
	frozen, err := pinMedia(labCatalog(pinned), request, records)
	if err != nil || frozen.BootMedia.SHA256 != strings.Repeat("a", 64) || frozen.BootMedia.Size != 7 {
		t.Fatalf("frozen boot media = %+v (%v)", frozen.BootMedia, diagnostics.Of(err))
	}
	if _, err := pinMedia(labCatalog(pinned), request, labRecords()); err == nil {
		t.Fatal("a record differing from the declared checksum was frozen")
	}
	malformed := api.NewObject(api.MachineImage, "rhel-9-8-boot", api.Value{}, api.MapValue(
		text("bootMedia", "local-media:rhel-9.8-x86_64-boot.iso"), text("checksum", "abc"),
	))
	_, _, err = Requests(labCatalog(malformed), "controller", testContext)
	expectRefusal(t, err, "api.value")
}

// A profile with no package source installs from the boot media itself, which
// is the DVD case, and publishes no tree.
func TestAProfileWithoutAPackageSourceInstallsFromItsMedia(t *testing.T) {
	anaconda := installProfile().Spec().Get("installer", "anaconda")
	plain := installProfile(field("installer", api.MapValue(field("anaconda", api.MapValue(
		text("imageRef", anaconda.Get("imageRef").Text()),
		field("redfishVirtualMedia", anaconda.Get("redfishVirtualMedia")),
	)))))
	request, needs := onlyRequest(t, labCatalog(plain))
	if request.Tree != nil || request.TreeMedia != nil {
		t.Fatalf("a profile without a source published %+v", request.Tree)
	}
	if !strings.Contains(request.Kickstart, "\ncdrom\n") {
		t.Fatal("the installer was not pointed at its own media")
	}
	if !slices.Equal(needs.ArtifactServers, []string{"lab-artifacts"}) {
		t.Fatalf("artifact servers = %v", needs.ArtifactServers)
	}
}

// Every profile arm that carries secret bytes or effects this contract cannot
// prove is refused before registration, through the Machine that selects it,
// saying what the profile selects and naming the field to change on it.
func TestUnsupportedNamesEveryProfileArmThisContractRefuses(t *testing.T) {
	customizations := installProfile().Spec().Get("customizations")
	selected := func(field string) [2]string {
		return [2]string{"the install profile selects " + field + ", which carries secret bytes or effects this executable does not prove",
			"remove " + field + " from MachineInstallProfile/rhel-9-8"}
	}
	for name, test := range map[string]struct {
		profile api.Object
		refusal [2]string
	}{
		"from subscription": {installProfile(field("installer", api.MapValue(field("anaconda", api.MapValue(
			text("imageRef", "rhel-9-8-boot"),
			field("packageSource", api.MapValue(field("fromSubscription", api.MapValue(text("entitlementRef", "rhel"))))),
		))))), selected("spec.installer.anaconda.packageSource.fromSubscription")},
		"mirror": {installProfile(field("installer", api.MapValue(field("anaconda", api.MapValue(
			text("imageRef", "rhel-9-8-boot"),
			field("packageSource", api.MapValue(field("mirror", api.MapValue(text("baseURL", "https://example.test/os"))))),
		))))), selected("spec.installer.anaconda.packageSource.mirror")},
		"template clone": {installProfile(field("installer", api.MapValue(field("templateClone", api.MapValue(
			field("seed", api.MapValue(field("cloudInit", api.MapValue()))),
		))))), [2]string{
			"this executable installs an operating system only through the anaconda installer, which the install profile does not select",
			"select spec.installer.anaconda on MachineInstallProfile/rhel-9-8",
		}},
		"subscription": {installProfile(field("subscription", api.MapValue(text("entitlementRef", "rhel")))), selected("spec.subscription")},
		"initial password": {installProfile(field("customizations", customizations.With("ssh", api.MapValue(
			field("initialPassword", api.MapValue(text("secretRef", "root-password"))),
		)))), selected("spec.customizations.ssh.initialPassword")},
		"disk encryption": {installProfile(field("customizations", customizations.With("security", api.MapValue(
			field("diskEncryption", api.MapValue(text("recoveryPassphraseRef", "luks"))),
		)))), selected("spec.customizations.security.diskEncryption")},
		"fips": {installProfile(field("customizations", customizations.With("security", api.MapValue(
			field("fips", api.MapValue(field("enabled", api.BoolValue(true)))),
		)))), [2]string{
			"the install profile enables FIPS, which carries effects this executable does not prove",
			"disable spec.customizations.security.fips on MachineInstallProfile/rhel-9-8",
		}},
	} {
		t.Run(name, func(t *testing.T) {
			unsupported := lifecycle.Identities(Refusals(labCatalog(test.profile)))
			if !slices.Equal(unsupported, []string{"Machine/rhel-01"}) {
				t.Fatalf("unsupported = %v", unsupported)
			}
			want := []lifecycle.Refusal{{Kind: "Machine", Name: "rhel-01", Reason: test.refusal[0], Remediation: test.refusal[1]}}
			if refused := Refusals(labCatalog(test.profile)); !slices.Equal(refused, want) {
				t.Fatalf("refusals = %+v, want %+v", refused, want)
			}
		})
	}
	if unsupported := lifecycle.Identities(Refusals(labCatalog())); len(unsupported) != 0 {
		t.Fatalf("the supported shape reported %v", unsupported)
	}
	// A profile that declares FIPS disabled has selected nothing, so it
	// installs exactly as one that never mentioned it.
	disabled := installProfile(field("customizations", customizations.With("security", api.MapValue(
		field("fips", api.MapValue(field("enabled", api.BoolValue(false)))),
	))))
	if unsupported := lifecycle.Identities(Refusals(labCatalog(disabled))); len(unsupported) != 0 {
		t.Fatalf("a disabled FIPS declaration reported %v", unsupported)
	}
}

// A physical installation erases the disk it names, so a Machine that names
// none by path refuses before registration rather than leaving the installer
// to clear every disk the server holds. A wwn alone is admitted as a selector
// but not yet derived into a device, so it refuses the same way.
func TestAPhysicalInstallationWithoutANamedRootDeviceRefuses(t *testing.T) {
	for name, hints := range map[string]api.Value{
		"wwn only":        api.MapValue(text("wwn", "0x5000c500a1b2c3d4")),
		"predicates only": api.MapValue(text("model", "PERC H755"), number("minSizeGigabytes", "400")),
	} {
		t.Run(name, func(t *testing.T) {
			catalog := labCatalog(metalProvider(), server(hints))
			if unsupported := lifecycle.Identities(Refusals(catalog)); !slices.Equal(unsupported, []string{"Machine/metal-01"}) {
				t.Fatalf("unsupported = %v", unsupported)
			}
			_, _, err := Requests(catalog, "controller", testContext)
			expectRefusal(t, err, "lifecycle.unsupported")
			remediation := diagnostics.Of(err)[0].Remediation
			if !strings.Contains(remediation, "spec.os.install.rootDeviceHints.deviceName on Machine/metal-01") ||
				!strings.Contains(remediation, "wwn-only selection is not yet supported") {
				t.Fatalf("remediation = %q", remediation)
			}
		})
	}
}

// A Machine whose installation delivers private material cannot let its
// controller fetch the installer without verifying the server, so the
// per-Machine exception refuses before registration, ahead of the refusal of
// an unverified controller leg, and Unsupported agrees with the request
// builder.
func TestAPrivateInstallationRefusesDisabledVerification(t *testing.T) {
	metal := server(api.MapValue(text("deviceName", "/dev/sda")))
	metal = metal.WithSpec(metal.Spec().WithPath(api.StringValue(substrate.TrustDisableVerification),
		"hardware", "management", "bmc", "virtualMedia", "tls", "trust"))
	catalog := labCatalog(metalProvider(), metal)
	if unsupported := lifecycle.Identities(Refusals(catalog)); !slices.Equal(unsupported, []string{"Machine/metal-01"}) {
		t.Fatalf("unsupported = %v", unsupported)
	}
	_, _, err := Requests(catalog, "controller", testContext)
	expectRefusal(t, err, "lifecycle.unsupported")
	reported := diagnostics.Of(err)[0]
	if reported.Message != "a Machine that delivers private material through its installation cannot let its controller fetch without verifying the artifact server" {
		t.Fatalf("message = %q", reported.Message)
	}
	if reported.Remediation != "declare hardware.management.bmc.virtualMedia.tls.trust: import-certificate on Machine/metal-01, or established when its controller already trusts the server" {
		t.Fatalf("remediation = %q", reported.Remediation)
	}
}

// Importing the server's certificate is the one trust with no fallback: it
// needs the image served over https and the certificate that server presents,
// and a request that lacks either refuses naming the Machine. No selection
// reaches this today, because only a delivered-key target imports and it
// always publishes privately, so the rule is proved on its own statement.
func TestImportCertificateNeedsAnHttpsImageAndItsCertificate(t *testing.T) {
	machine := server(api.MapValue(text("deviceName", "/dev/sda")))
	importing := func(url string) Request {
		return Request{
			Private:           &Publication{URL: url},
			Target:            Target{Controller: Controller{VirtualMedia: VirtualMedia{Trust: substrate.TrustImportCertificate}}},
			TLSCertificateRef: "lab-artifacts-tls",
		}
	}
	if err := mediaTrustRefusal(machine, importing("https://artifacts.lab.example.test/private/os/metal-01")); err != nil {
		t.Fatalf("an https image with its certificate refused: %v", err)
	}
	public := importing("")
	public.Private, public.Image = nil, &Publication{URL: "https://artifacts.lab.example.test/os/metal-01/install.iso"}
	if err := mediaTrustRefusal(machine, public); err != nil {
		t.Fatalf("an https public image with its certificate refused: %v", err)
	}
	for name, change := range map[string]func(*Request){
		"a plain http image":     func(r *Request) { r.Private.URL = "http://artifacts.lab.example.test/private/os/metal-01" },
		"no server certificate":  func(r *Request) { r.TLSCertificateRef = "" },
		"neither of them at all": func(r *Request) { r.Private.URL, r.TLSCertificateRef = "http://a.test/p", "" },
	} {
		t.Run(name, func(t *testing.T) {
			request := importing("https://artifacts.lab.example.test/private/os/metal-01")
			change(&request)
			err := mediaTrustRefusal(machine, request)
			expectRefusal(t, err, "lifecycle.state")
			reported := diagnostics.Of(err)[0]
			if !strings.Contains(reported.Message, "Machine/metal-01") ||
				reported.Remediation != "select an https artifactServerEndpoint on a server declaring spec.tls.secretRef" {
				t.Fatalf("refusal = %#v", reported)
			}
		})
	}
	for _, trust := range []string{substrate.TrustEstablished, substrate.TrustDisableVerification} {
		request := importing("http://a.test/p")
		request.TLSCertificateRef = ""
		request.Target.Controller.VirtualMedia.Trust = trust
		if err := mediaTrustRefusal(machine, request); err != nil {
			t.Fatalf("%s needs no certificate to import, yet refused: %v", trust, err)
		}
	}
}

// A delivered-key installation hands its controller the private installer
// image URL, so a controller reached over plain http, or over https without
// verifying its certificate, refuses before registration with the one reason
// and remedy, and Unsupported agrees with the request builder.
func TestAPrivateInstallationRefusesAnUnverifiedController(t *testing.T) {
	const reason = "a Machine whose installation delivers private material hands its controller the private installer image URL, " +
		"so the controller must be reached over https with its certificate verified"
	const remedy = "address the controller of Machine/metal-01 with an https:// spec.hardware.management.bmc.address and remove the tls.verify opt-out " +
		"on Machine/metal-01 or in spec.baremetal.defaults.bmc of InfraProvider/lab-metal; " +
		"name the authority that issued its certificate in tls.trustBundleRef when the system trust store does not hold it"
	for name, change := range map[string]func(api.Value) api.Value{
		"an http controller": func(spec api.Value) api.Value {
			return spec.WithPath(api.StringValue("http://bmc-01.lab.example.test/redfish/v1/Systems/1"), "hardware", "management", "bmc", "address")
		},
		"verification disabled": func(spec api.Value) api.Value {
			return spec.WithPath(api.BoolValue(false), "hardware", "management", "bmc", "tls", "verify")
		},
	} {
		t.Run(name, func(t *testing.T) {
			metal := server(api.MapValue(text("deviceName", "/dev/sda")))
			metal = metal.WithSpec(change(metal.Spec()))
			catalog := labCatalog(metalProvider(), metal, sshKeySecret(api.MapValue(field("generated", api.MapValue(text("keyType", "ed25519"))))))
			refusals := Refusals(catalog)
			if unsupported := lifecycle.Identities(refusals); !slices.Equal(unsupported, []string{"Machine/metal-01"}) {
				t.Fatalf("unsupported = %v", unsupported)
			}
			_, _, err := Requests(catalog, "controller", testContext)
			expectRefusal(t, err, "lifecycle.unsupported")
			reported := diagnostics.Of(err)[0]
			if reported.Message != reason || reported.Remediation != remedy {
				t.Fatalf("refusal = %q / %q", reported.Message, reported.Remediation)
			}
		})
	}
}

// A delivered key's URL is named by the Kickstart implanted in the installer
// image, so that image is never derived as a public publication, and an
// operation an earlier build froze with a public image beside its private
// publication refuses its apply before anything is published.
func TestAPhysicalInstallationRefusesWhileItsHostKeyWouldBePublic(t *testing.T) {
	catalog := labCatalog(metalProvider(), server(api.MapValue(text("deviceName", "/dev/sda"))),
		sshKeySecret(api.MapValue(field("generated", api.MapValue()))))
	requests, _, err := Requests(catalog, "controller", testContext)
	if err != nil {
		t.Fatalf("requests: %v", diagnostics.Of(err))
	}
	delivered := 0
	for _, request := range requests {
		if request.Target.Channel != substrate.ChannelDeliveredKey {
			continue
		}
		delivered++
		if request.Image != nil || request.Private == nil {
			t.Fatalf("a delivered-key installation of %s derived a public image: %+v", request.Identity.Object, request.Image)
		}
		exposed := request
		exposed.Image = &Publication{Path: request.Private.Path + "/install.iso", URL: request.Private.URL + "/install.iso"}
		err := refusedContinuation(testContext, exposed)
		expectRefusal(t, err, "lifecycle.state")
		if reported := diagnostics.Of(err)[0]; reported.Message !=
			"this operation froze an installation of Machine/metal-01 with no single installer image publication, which this executable refuses" {
			t.Fatalf("message = %q", reported.Message)
		}
		if err := refusedContinuation(testContext, request); err != nil {
			t.Fatalf("the derived request refused: %v", diagnostics.Of(err))
		}
	}
	if delivered != 1 {
		t.Fatalf("%d delivered-key installations derived, want 1", delivered)
	}
}

// A physical Machine whose controller is reached over verified https derives
// a request that publishes its installer image only beneath its private
// subtree: no public image, the private parent and URL, the serving
// certificate the installer verifies, and the key type it installs.
func TestAVerifiedPhysicalInstallationDerivesAPrivateRequest(t *testing.T) {
	catalog := labCatalog(metalProvider(), server(api.MapValue(text("deviceName", "/dev/sda"))),
		sshKeySecret(api.MapValue(field("generated", api.MapValue(text("keyType", "rsa"))))))
	if refusals := Refusals(catalog); len(refusals) != 0 {
		t.Fatalf("refusals = %v", refusals)
	}
	requests, requirements, err := Requests(catalog, "controller", testContext)
	if err != nil {
		t.Fatalf("requests: %v", diagnostics.Of(err))
	}
	var request Request
	var needs Requirements
	for index, candidate := range requests {
		if candidate.Identity.Object == "metal-01" {
			request, needs = candidate, requirements[index]
		}
	}
	root := "/var/lib/bootwright-services/lab/artifact-server/lab-artifacts/public"
	if request.Image != nil {
		t.Fatalf("a delivered-key installation froze a public image: %+v", request.Image)
	}
	if request.Private == nil || request.Private.Path != root+"/private/os/metal-01" || request.Private.URL != "https://192.0.2.1:8443/private/os/metal-01" {
		t.Fatalf("private = %+v", request.Private)
	}
	if request.TLSCertificateRef != "lab-artifacts-tls" || request.Target.HostKeyType != "rsa" || !request.Target.Physical {
		t.Fatalf("request = %+v", request)
	}
	if !slices.Contains(needs.ArtifactServers, "lab-artifacts") || needs.Machine != "metal-01" {
		t.Fatalf("requirements = %+v", needs)
	}
	if keys := request.ReservationKeys(); !slices.Contains(keys, "path:"+root+"/private/os/metal-01") || slices.Contains(keys, "path:") {
		t.Fatalf("reservation keys = %v", keys)
	}
	if !strings.Contains(request.Kickstart, PrivateURLToken) {
		t.Fatalf("the kickstart names no private URL")
	}
}

// The Kickstart selects its root disk by name alone, so a Machine declaring any
// other hint refuses before registration on either arm, naming each field to
// remove, rather than being installed onto a disk those hints did not select. A
// physical Machine reaches this refusal before the delivered-key one, so its
// remediation is the hint and not the key.
func TestAnInstallationRefusesARootDeviceHintItCannotCarry(t *testing.T) {
	onGuest := func(hints api.Value) api.Object {
		declared := guest()
		return declared.WithSpec(declared.Spec().WithPath(hints, "os", "install", "rootDeviceHints"))
	}
	for name, test := range map[string]struct {
		overrides   []api.Object
		machine     string
		remediation string
	}{
		"a physical model": {
			[]api.Object{metalProvider(), server(api.MapValue(text("deviceName", "/dev/sda"), text("model", "PERC H755")))},
			"Machine/metal-01", "remove spec.os.install.rootDeviceHints.model from Machine/metal-01",
		},
		"a physical wwn": {
			[]api.Object{metalProvider(), server(api.MapValue(text("deviceName", "/dev/sda"), text("wwn", "0x5000c500a1b2c3d4")))},
			"Machine/metal-01", "remove spec.os.install.rootDeviceHints.wwn from Machine/metal-01",
		},
		"a guest's rotational": {
			[]api.Object{onGuest(api.MapValue(text("deviceName", "/dev/vda"), field("rotational", api.BoolValue(false))))},
			"Machine/rhel-01", "remove spec.os.install.rootDeviceHints.rotational from Machine/rhel-01",
		},
		"a guest's model alone": {
			[]api.Object{onGuest(api.MapValue(text("model", "QEMU HARDDISK")))},
			"Machine/rhel-01", "remove spec.os.install.rootDeviceHints.model from Machine/rhel-01",
		},
		"a guest's wwn and size": {
			[]api.Object{onGuest(api.MapValue(text("wwn", "0x5000c500a1b2c3d4"), number("minSizeGigabytes", "40")))},
			"Machine/rhel-01",
			"remove spec.os.install.rootDeviceHints.minSizeGigabytes, spec.os.install.rootDeviceHints.wwn from Machine/rhel-01",
		},
	} {
		t.Run(name, func(t *testing.T) {
			catalog := labCatalog(test.overrides...)
			if unsupported := lifecycle.Identities(Refusals(catalog)); !slices.Equal(unsupported, []string{test.machine}) {
				t.Fatalf("unsupported = %v", unsupported)
			}
			_, _, err := Requests(catalog, "controller", testContext)
			expectRefusal(t, err, "lifecycle.unsupported")
			reported := diagnostics.Of(err)[0]
			if reported.Message != "a managed-OS installation selects its root disk by deviceName alone and cannot carry the other root-device hints the Machine declares" {
				t.Fatalf("message = %q", reported.Message)
			}
			if reported.Remediation != test.remediation {
				t.Fatalf("remediation = %q", reported.Remediation)
			}
			// Plan and apply refuse before registration with this same
			// reason and remedy, naming the Machine.
			name := strings.TrimPrefix(test.machine, "Machine/")
			want := []lifecycle.Refusal{{Kind: "Machine", Name: name, Reason: reported.Message, Remediation: test.remediation}}
			if refused := Refusals(catalog); !slices.Equal(refused, want) {
				t.Fatalf("refusals = %+v, want %+v", refused, want)
			}
		})
	}
	request, _ := onlyRequest(t, labCatalog(onGuest(api.MapValue(text("deviceName", "/dev/vda")))))
	if !strings.Contains(request.Kickstart, "ignoredisk --only-use=vda") {
		t.Fatal("a guest naming its device alone was not installed onto it")
	}
}

func TestSelectionRefusesWhatItCannotDerive(t *testing.T) {
	noKey := api.NewObject(api.Environment, "lab-rhel", api.Value{}, api.MapValue(
		field("controller", api.MapValue(text("machineRef", "controller"))),
	))
	noAddress := guest(field("network", guest().Spec().Get("network").With("installAddressRef", api.StringValue("absent"))))
	noInterface := guest(field("network", guest().Spec().Get("network").With("addresses", api.ListValue(
		api.MapValue(text("name", "fqdn"), text("address", "rhel-01.lab.example.test")),
		api.MapValue(text("name", "ip"), text("address", "198.51.100.11/24")),
	))))
	noFQDN := guest(field("network", guest().Spec().Get("network").With("addresses", api.ListValue(
		api.MapValue(text("name", "ip"), text("address", "198.51.100.11/24"), text("interface", "enp1s0")),
	))))
	for name, test := range map[string]struct {
		catalog api.Catalog
		code    string
	}{
		"no fleet key": {labCatalog(noKey), "api.required"},
		"no address":   {labCatalog(noAddress), "api.reference"},
		"no interface": {labCatalog(noInterface), "api.value"},
		"no fqdn":      {labCatalog(noFQDN), "api.required"},
		// An https publication is verified against its server's certificate
		// before any machine is given it, so a server declaring none refuses.
		"no serving certificate": {labCatalog(artifactServer(field("tls", api.MapValue()))), "api.required"},
		"absent server":          {catalogOf(environment(), controller(), provider(), networkConfig(), bootImage(), installProfile(), guest()), "api.reference"},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := Requests(test.catalog, "controller", testContext)
			expectRefusal(t, err, test.code)
		})
	}
}

// An unmanaged artifact server is not this product's to publish through,
// because nothing proves it serves what the installation publishes. Name and
// time services are another matter: an external one is used at the address it
// declares.
func TestOnlyManagedServicesAreUsed(t *testing.T) {
	external := api.NewObject(api.ArtifactServer, "lab-artifacts", api.Value{}, api.MapValue(
		text("management", "external"), text("machineRef", "controller"),
	))
	_, _, err := Requests(labCatalog(external), "controller", testContext)
	expectRefusal(t, err, "lifecycle.state")
}

func TestFrozenRequestsRoundTripExactly(t *testing.T) {
	request, _ := onlyRequest(t, labCatalog())
	data, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeRequest(data)
	if err != nil || decoded.Kickstart != request.Kickstart {
		t.Fatalf("round trip: %v", err)
	}
	for name, damaged := range map[string][]byte{
		"trailing data": append(slices.Clone(data), '{', '}'),
		"unknown field": []byte(`{"unexpected":1}`),
		"empty":         {},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeRequest(damaged); err == nil {
				t.Fatal("a malformed frozen request was accepted")
			}
		})
	}
}

// The marker names the request digest, so it can only be built at execution,
// and it is what completion compares byte for byte.
func TestTheMarkerNamesTheRequestItProves(t *testing.T) {
	request, _ := onlyRequest(t, labCatalog())
	marker, err := MarkerFor(request, "digest")
	if err != nil {
		t.Fatal(err)
	}
	want := `{"context":"lab","image":"rhel-9.8-x86_64-boot.iso","machine":"rhel-01","profile":"rhel-9-8","request":"digest"}`
	if string(marker) != want {
		t.Fatalf("marker = %s", marker)
	}
	other, _ := MarkerFor(request, "another")
	if string(other) == string(marker) {
		t.Fatal("two requests produced one marker")
	}
}

// Every version but the one this build writes refuses, and the refusal names
// it so the remedy is the executable that registered the operation.
func TestAFrozenRequestOfAnyOtherVersionRefuses(t *testing.T) {
	for _, version := range []string{"os-install-anaconda-v1", "os-install-anaconda-v2", "os-install-anaconda-v3", "os-install-anaconda-v4", "os-install-anaconda-v5", "os-install-anaconda-v6", "os-install-anaconda-v8"} {
		_, err := DecodeRequest([]byte(`{"version":"` + version + `"}`))
		if err == nil {
			t.Fatalf("version %q was accepted", version)
		}
		reported := diagnostics.Of(err)
		if len(reported) == 0 || !strings.Contains(reported[0].Message, version) {
			t.Fatalf("the refusal did not name %q: %v", version, err)
		}
	}
}

// No password can be set while initialPassword is refused, so a profile that
// enables password authentication would install a system that cannot honor
// it; it refuses before registration through the Machine.
func TestPasswordAuthenticationTrueRefuses(t *testing.T) {
	profile := installProfile(field("customizations", installProfile().Spec().Get("customizations").With("ssh", api.MapValue(
		field("passwordAuthentication", api.BoolValue(true)),
	))))
	want := []lifecycle.Refusal{{Kind: "Machine", Name: "rhel-01",
		Reason:      "the install profile enables spec.customizations.ssh.passwordAuthentication, but no password can be set while spec.customizations.ssh.initialPassword is refused",
		Remediation: "set spec.customizations.ssh.passwordAuthentication to false on MachineInstallProfile/rhel-9-8"}}
	if refused := Refusals(labCatalog(profile)); !slices.Equal(refused, want) {
		t.Fatalf("refusals = %+v, want %+v", refused, want)
	}
	if unsupported := lifecycle.Identities(Refusals(labCatalog(profile))); !slices.Equal(unsupported, []string{"Machine/rhel-01"}) {
		t.Fatalf("unsupported = %v", unsupported)
	}
	disabled := installProfile(field("customizations", installProfile().Spec().Get("customizations").With("ssh", api.MapValue(
		field("passwordAuthentication", api.BoolValue(false)),
	))))
	if unsupported := lifecycle.Identities(Refusals(labCatalog(disabled))); len(unsupported) != 0 {
		t.Fatalf("passwordAuthentication false reported %v", unsupported)
	}
}

// A .repo file carries a proxy URL and nothing else, so a Proxy that needs a
// credential or a private trust anchor refuses while a repository would reach
// it, both before registration and in the request builder.
func TestAProxyWithCredentialsRefusesWhileRepositoriesUseIt(t *testing.T) {
	remediation := "select direct: {} or an external Proxy without spec.connection.auth and spec.connection.trustBundleRef in spec.proxy of Machine/rhel-01 or of MachineInstallProfile/rhel-9-8"
	credentialed := "the installed system's repositories would reach Proxy/corporate through a credential or private trust anchor, which an installation cannot carry"
	for name, test := range map[string]struct {
		proxy  api.Object
		reason string
	}{
		"auth": {externalProxy("corporate", text("httpsProxy", "http://proxy.example.test:3128"),
			field("auth", api.MapValue(text("proxyAuthRef", "proxy-credentials")))), credentialed},
		"trust bundle": {externalProxy("corporate", text("httpsProxy", "http://proxy.example.test:3128"),
			text("trustBundleRef", "corporate-ca")), credentialed},
		"no URL": {externalProxy("corporate"),
			"the installed system's repositories would be reached through Proxy/corporate, which declares no proxy URL"},
	} {
		t.Run(name, func(t *testing.T) {
			catalog := labCatalog(test.proxy, proxiedGuest(), repositoryProfile("https://mirror.example.test/a"))
			want := []lifecycle.Refusal{{Kind: "Machine", Name: "rhel-01", Reason: test.reason, Remediation: remediation}}
			if refused := Refusals(catalog); !slices.Equal(refused, want) {
				t.Fatalf("refusals = %+v, want %+v", refused, want)
			}
			_, _, err := Requests(catalog, "controller", testContext)
			reported := diagnostics.Of(err)
			if len(reported) != 1 || reported[0].Message != test.reason || reported[0].Remediation != remediation {
				t.Fatalf("the request builder refused %#v", reported)
			}
		})
	}
}

// With no configured repository the installed system reaches nothing through
// the proxy, so a credentialed one carries nothing and is accepted.
func TestAProxyWithCredentialsIsAcceptedWithoutRepositories(t *testing.T) {
	proxy := externalProxy("corporate", text("httpsProxy", "http://proxy.example.test:3128"), text("trustBundleRef", "corporate-ca"))
	catalog := labCatalog(proxy, proxiedGuest())
	if refused := Refusals(catalog); len(refused) != 0 {
		t.Fatalf("refusals = %+v", refused)
	}
	request, _ := onlyRequest(t, catalog)
	if strings.Contains(request.Kickstart, "proxy") {
		t.Fatalf("the kickstart names a proxy:\n%s", request.Kickstart)
	}
}

// sshKeySecret is a host key Secret of one source, as admission normalizes it.
func sshKeySecret(source api.Value) api.Object {
	declared := api.NewObject(api.Secret, "metal-01-host-key", api.Value{}, api.MapValue(
		text("type", "sshKeyPair"), field("source", source),
	))
	normalized, _ := secrets.Normalize(declared, api.Catalog{})
	return normalized
}

// The key a physical installation delivers is installed at its own type's
// path, and only a generated declaration names that type before the material
// exists: its keyType, or the ed25519 admission defaults it to. A key of any
// other source refuses, naming the Secret and the Machine that selects it.
func TestTheDeliveredHostKeyTypeIsItsGeneratedKeyType(t *testing.T) {
	machine := server(api.MapValue(text("deviceName", "/dev/sda")))
	for _, row := range []struct {
		name   string
		source api.Value
		want   string
	}{
		{"rsa", api.MapValue(field("generated", api.MapValue(text("keyType", "rsa")))), "rsa"},
		{"ecdsa-p256", api.MapValue(field("generated", api.MapValue(text("keyType", "ecdsa-p256")))), "ecdsa-p256"},
		{"defaulted", api.MapValue(field("generated", api.MapValue())), "ed25519"},
	} {
		t.Run(row.name, func(t *testing.T) {
			got, err := hostKeyType(labCatalog(metalProvider(), machine, sshKeySecret(row.source)), machine, "metal-01-host-key")
			if err != nil || got != row.want {
				t.Fatalf("host key type = %q (%v), want %q", got, diagnostics.Of(err), row.want)
			}
		})
	}

	_, err := hostKeyType(labCatalog(metalProvider(), machine,
		sshKeySecret(api.MapValue(field("contextStore", api.MapValue())))), machine, "metal-01-host-key")
	expectRefusal(t, err, "lifecycle.unsupported")
	reported := diagnostics.Of(err)[0]
	if reported.Message != "a physical installation installs its delivered SSH host key at the path of the type its generated declaration names, "+
		"and Secret/metal-01-host-key declares no generated source" {
		t.Fatalf("message = %q", reported.Message)
	}
	if reported.Remediation != "declare spec.source.generated.keyType on Secret/metal-01-host-key, "+
		"or name a generated sshKeyPair Secret in spec.os.install.hostKeyRef on Machine/metal-01" {
		t.Fatalf("remediation = %q", reported.Remediation)
	}

	_, err = hostKeyType(labCatalog(metalProvider(), machine), machine, "metal-01-host-key")
	expectRefusal(t, err, "api.reference")
	if reported := diagnostics.Of(err)[0]; reported.Remediation != "declare Secret/metal-01-host-key or correct spec.os.install.hostKeyRef on Machine/metal-01" {
		t.Fatalf("remediation = %q", reported.Remediation)
	}

	token := api.NewObject(api.Secret, "metal-01-host-key", api.Value{}, api.MapValue(
		text("type", "token"), field("source", api.MapValue(field("generated", api.MapValue(number("bytes", "32")))))))
	_, err = hostKeyType(labCatalog(metalProvider(), machine, token), machine, "metal-01-host-key")
	expectRefusal(t, err, "api.value")
}
