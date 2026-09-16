package installation

import (
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
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

// A declared checksum is canonicalized and frozen, so an attempt proves the
// entry it uses is the entry the graph named.
func TestADeclaredChecksumIsFrozenInCanonicalForm(t *testing.T) {
	pinned := api.NewObject(api.MachineImage, "rhel-9-8-boot", api.Value{}, api.MapValue(
		text("bootMedia", "local-media:rhel-9.8-x86_64-boot.iso"),
		text("checksum", "SHA256:"+strings.Repeat("A", 64)),
	))
	request, _ := onlyRequest(t, labCatalog(pinned))
	if request.BootMedia.SHA256 != strings.Repeat("a", 64) {
		t.Fatalf("checksum = %q", request.BootMedia.SHA256)
	}
	malformed := api.NewObject(api.MachineImage, "rhel-9-8-boot", api.Value{}, api.MapValue(
		text("bootMedia", "local-media:rhel-9.8-x86_64-boot.iso"), text("checksum", "abc"),
	))
	_, _, err := Requests(labCatalog(malformed), "controller", testContext)
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
// prove is named before registration, through the Machine that selects it.
func TestUnsupportedNamesEveryProfileArmThisContractRefuses(t *testing.T) {
	customizations := installProfile().Spec().Get("customizations")
	for name, profile := range map[string]api.Object{
		"from subscription": installProfile(field("installer", api.MapValue(field("anaconda", api.MapValue(
			text("imageRef", "rhel-9-8-boot"),
			field("packageSource", api.MapValue(field("fromSubscription", api.MapValue(text("entitlementRef", "rhel"))))),
		))))),
		"mirror": installProfile(field("installer", api.MapValue(field("anaconda", api.MapValue(
			text("imageRef", "rhel-9-8-boot"),
			field("packageSource", api.MapValue(field("mirror", api.MapValue(text("baseURL", "https://example.test/os"))))),
		))))),
		"template clone": installProfile(field("installer", api.MapValue(field("templateClone", api.MapValue(
			field("seed", api.MapValue(field("cloudInit", api.MapValue()))),
		))))),
		"subscription": installProfile(field("subscription", api.MapValue(text("entitlementRef", "rhel")))),
		"initial password": installProfile(field("customizations", customizations.With("ssh", api.MapValue(
			field("initialPassword", api.MapValue(text("secretRef", "root-password"))),
		)))),
		"disk encryption": installProfile(field("customizations", customizations.With("security", api.MapValue(
			field("diskEncryption", api.MapValue(text("recoveryPassphraseRef", "luks"))),
		)))),
	} {
		t.Run(name, func(t *testing.T) {
			unsupported := Unsupported(labCatalog(profile))
			if !slices.Equal(unsupported, []string{"Machine/rhel-01"}) {
				t.Fatalf("unsupported = %v", unsupported)
			}
		})
	}
	if unsupported := Unsupported(labCatalog()); len(unsupported) != 0 {
		t.Fatalf("the supported shape reported %v", unsupported)
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
		"no fleet key":  {labCatalog(noKey), "api.required"},
		"no address":    {labCatalog(noAddress), "api.reference"},
		"no interface":  {labCatalog(noInterface), "api.value"},
		"no fqdn":       {labCatalog(noFQDN), "api.required"},
		"absent server": {catalogOf(environment(), controller(), provider(), networkConfig(), bootImage(), installProfile(), guest()), "api.reference"},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := Requests(test.catalog, "controller", testContext)
			expectRefusal(t, err, test.code)
		})
	}
}

// An unmanaged service is not this product's to publish through or resolve
// against, because nothing proves it answers.
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
