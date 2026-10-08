package installation

import (
	"context"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate"
)

// uncarriedReason is the reason of the refusal-table row for network content
// the Kickstart cannot carry.
const uncarriedReason = "a managed-OS installation configures only the install address, its default route and the selected name servers, and cannot carry the other network content the Machine declares"

// networkWith is the lab network configuration declaring the given interfaces.
func networkWith(interfaces ...api.Value) api.Object {
	config := networkConfig()
	return config.WithSpec(config.Spec().WithPath(api.ListValue(interfaces...), "nmstate", "interfaces"))
}

// withRoutes replaces the routes a network configuration declares.
func withRoutes(config api.Object, routes ...api.Value) api.Object {
	return config.WithSpec(config.Spec().WithPath(api.ListValue(routes...), "nmstate", "routes", "config"))
}

// hypervisorHost is an operator-provided Machine reached over SSH that hosts
// libvirt, so a provider placed on it emulates its guests' controllers there.
func hypervisorHost() api.Object {
	return api.NewObject(api.Machine, "hv-01", api.Value{}, api.MapValue(
		field("capabilities", api.StringList("libvirt")),
		field("os", api.MapValue(field("provided", api.BoolValue(true)))),
		field("network", api.MapValue(field("addresses", api.ListValue(
			api.MapValue(text("name", "fqdn"), text("address", "hv-01.lab.example.test")),
			api.MapValue(text("name", "ip"), text("address", "192.0.2.3")),
		)))),
		field("access", api.MapValue(field("ssh", api.MapValue(
			text("addressRef", "ip"), text("user", "root"),
			field("auth", api.MapValue(text("privateKeyRef", "hv-01-ssh-key"))),
			text("knownHostsRef", "hv-01-known-hosts"),
		)))),
	))
}

// externalService is a site name or time service this product does not run,
// reached at the address it declares.
func externalService(kind api.Kind, name, address string) api.Object {
	return api.NewObject(kind, name, api.Value{}, api.MapValue(text("management", "external"), text("address", address)))
}

// siteServicesCatalog is the lab-rhel shape with its name and time services
// the site's own, selected without an endpoint as an external selection must
// be.
func siteServicesCatalog() api.Catalog {
	config := networkConfig()
	config = config.WithSpec(config.Spec().With("dns", api.ListValue(api.MapValue(text("serverRef", "lab-dns")))))
	return labCatalog(config, installProfile(field("ntp", api.ListValue(api.MapValue(text("serverRef", "lab-ntp"))))),
		externalService(api.DNSServer, "lab-dns", "203.0.113.53"), externalService(api.NTPServer, "lab-ntp", "ntp.example.test"))
}

func planRequires(t *testing.T, catalog api.Catalog) []reconciliation.ObjectRef {
	t.Helper()
	plan, err := New(nil).WithMedia(labMedia()).Plan(context.Background(), lifecycle.PlanInput{
		Verb: reconciliation.Apply, State: compilation.NewState(catalog, catalog, nil),
		Controller: "controller", Context: lifecycle.ContextIdentity{Name: testContext},
	})
	if err != nil || len(plan.Definitions) != 1 {
		t.Fatalf("plan = %+v (%v)", plan, diagnostics.Of(err))
	}
	return plan.Definitions[0].Requires
}

// A site's own name and time services are the norm outside a lab. The guest
// resolves and synchronizes through their declared addresses, and the block
// waits for neither, because no block of this product realizes them; a
// managed one beside them is still waited for.
func TestExternalNameAndTimeServicesAreUsedByTheirDeclaredAddress(t *testing.T) {
	catalog := siteServicesCatalog()
	if refused := Refusals(catalog); len(refused) != 0 {
		t.Fatalf("refusals = %+v", refused)
	}
	request, needs := onlyRequest(t, catalog)
	for _, line := range []string{"--nameserver=203.0.113.53", "--ntpservers=ntp.example.test"} {
		if !strings.Contains(request.Kickstart, line) {
			t.Fatalf("the Kickstart carries no %s:\n%s", line, request.Kickstart)
		}
	}
	if len(needs.DNSServers) != 0 || len(needs.NTPServers) != 0 {
		t.Fatalf("requirements = %+v", needs)
	}
	want := []reconciliation.ObjectRef{{Kind: "Machine", Object: "rhel-01"}, {Kind: "ArtifactServer", Object: "lab-artifacts"}}
	if requires := planRequires(t, catalog); !slices.Equal(requires, want) {
		t.Fatalf("requires = %v", requires)
	}

	config := networkConfig()
	config = config.WithSpec(config.Spec().With("dns", api.ListValue(
		api.MapValue(text("serverRef", "lab-dns"), text("endpointRef", "ip")), api.MapValue(text("serverRef", "site-dns")),
	)))
	mixed := labCatalog(config, externalService(api.DNSServer, "site-dns", "203.0.113.53"))
	request, needs = onlyRequest(t, mixed)
	if !strings.Contains(request.Kickstart, "--nameserver=192.0.2.1,203.0.113.53") {
		t.Fatalf("the Kickstart carries not both name servers:\n%s", request.Kickstart)
	}
	if !slices.Equal(needs.DNSServers, []string{"lab-dns"}) || !slices.Equal(needs.NTPServers, []string{"lab-ntp"}) {
		t.Fatalf("requirements = %+v", needs)
	}
}

// An external service that declares no address leaves nothing to resolve
// through. Admission requires the field, so this is the reader's backstop.
func TestAnExternalServiceWithoutAnAddressRefuses(t *testing.T) {
	config := networkConfig()
	config = config.WithSpec(config.Spec().With("dns", api.ListValue(api.MapValue(text("serverRef", "site-dns")))))
	silent := api.NewObject(api.DNSServer, "site-dns", api.Value{}, api.MapValue(text("management", "external")))
	_, _, err := Requests(labCatalog(config, silent), "controller", testContext)
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "api.required" ||
		reported[0].Message != "the selected external DNSServer declares no address" ||
		reported[0].Remediation != "set spec.address on DNSServer/site-dns" {
		t.Fatalf("refusal = %#v", reported)
	}
	if _, _, err := Requests(labCatalog(silent), "controller", testContext); err != nil {
		t.Fatalf("an unselected external service refused: %v", diagnostics.Of(err))
	}
}

// What the lab-rhel network declares beyond the install line changes nothing
// the line would configure differently, so it installs: an MTU the system
// takes anyway, IPv6 off, a search domain, and the default route's own table
// and metric on the install interface.
func TestTheInstallLineContentIsNotRefused(t *testing.T) {
	config := withRoutes(networkWith(api.MapValue(
		text("name", "enp1s0"), text("type", "ethernet"), text("state", "up"), number("mtu", "1500"),
		field("ipv4", api.MapValue(field("enabled", api.BoolValue(true)), field("dhcp", api.BoolValue(false)))),
		field("ipv6", api.MapValue(field("enabled", api.BoolValue(false)))),
	)), api.MapValue(
		text("destination", "0.0.0.0/0"), text("next-hop-address", "198.51.100.1"), text("next-hop-interface", "enp1s0"),
		number("table-id", "254"), number("metric", "100"),
	))
	config = config.WithSpec(config.Spec().WithPath(api.MapValue(field("config", api.MapValue(
		field("search", api.StringList("lab.example.test")),
	))), "nmstate", "dns-resolver"))
	catalog := labCatalog(config)
	if refused := Refusals(catalog); len(refused) != 0 {
		t.Fatalf("refusals = %+v", refused)
	}
	request, _ := onlyRequest(t, catalog)
	if !strings.Contains(request.Kickstart, "--gateway=198.51.100.1") {
		t.Fatalf("the Kickstart carries no gateway:\n%s", request.Kickstart)
	}
}

// The network rows describe what the Kickstart carries, so a profile that
// selects another installer is refused for that choice, whatever network its
// Machine declares, as admission admits a DHCP network for it.
func TestAnotherInstallerIsRefusedBeforeItsNetwork(t *testing.T) {
	dhcp := guest(field("network", guest().Spec().Get("network").Without("installAddressRef").With("addresses", api.ListValue(
		api.MapValue(text("name", "fqdn"), text("address", "rhel-01.lab.example.test")),
	))))
	clone := installProfile(field("installer", api.MapValue(field("templateClone", api.MapValue(
		field("seed", api.MapValue(field("cloudInit", api.MapValue()))),
	)))))
	want := []lifecycle.Refusal{{Kind: "Machine", Name: "rhel-01",
		Reason:      "this executable installs an operating system only through the anaconda installer, which the install profile does not select",
		Remediation: "select spec.installer.anaconda on MachineInstallProfile/rhel-9-8"}}
	if refused := Refusals(labCatalog(dhcp, clone)); !slices.Equal(refused, want) {
		t.Fatalf("refusals = %+v", refused)
	}
}

// The installation reads the network the Machine composes, so content only an
// override declares is refused as surely as content the template declares.
func TestOverrideContentIsRefused(t *testing.T) {
	for name, test := range map[string]struct {
		overrides api.Value
		network   string
	}{
		"an mtu": {api.MapValue(field("interfaces", api.ListValue(api.MapValue(text("name", "enp1s0"), number("mtu", "9000"))))), "mtu 9000 on enp1s0"},
	} {
		t.Run(name, func(t *testing.T) {
			overridden := guest(field("network", guest().Spec().Get("network").With("overrides", test.overrides)))
			want := []lifecycle.Refusal{{Kind: "Machine", Name: "rhel-01", Reason: uncarriedReason,
				Remediation: "remove " + test.network + " from the network of Machine/rhel-01, or install its operating system outside Bootwright"}}
			if refused := Refusals(labCatalog(overridden)); !slices.Equal(refused, want) {
				t.Fatalf("refusals = %+v, want %+v", refused, want)
			}
		})
	}
}

// The install line configures its gateway on the install interface once, so
// only the first default route through that interface, or through none named,
// is carried. A default route through another interface would be installed on
// the install interface instead, and one behind the carried route would be
// dropped, so either is refused rather than installed in part.
func TestOnlyTheDefaultRouteTheLineCarriesIsExempt(t *testing.T) {
	defaultRoute := func(fields ...api.FieldValue) api.Value {
		return api.MapValue(append([]api.FieldValue{text("destination", "0.0.0.0/0"), text("next-hop-address", "198.51.100.1")}, fields...)...)
	}
	for name, routes := range map[string][]api.Value{
		"through another interface": {defaultRoute(text("next-hop-interface", "enp2s0"))},
		"behind the carried one":    {defaultRoute(), defaultRoute(number("table-id", "100"))},
	} {
		t.Run(name, func(t *testing.T) {
			config := withRoutes(networkWith(
				api.MapValue(text("name", "enp1s0"), text("type", "ethernet")),
				api.MapValue(text("name", "enp2s0"), text("type", "ethernet")),
			), routes...)
			want := []lifecycle.Refusal{{Kind: "Machine", Name: "rhel-01", Reason: uncarriedReason,
				Remediation: "remove route 0.0.0.0/0 from the network of Machine/rhel-01, or install its operating system outside Bootwright"}}
			if refused := Refusals(labCatalog(config)); !slices.Equal(refused, want) {
				t.Fatalf("refusals = %+v, want %+v", refused, want)
			}
		})
	}
}

// A Machine meets the refusal-table rows in their order: its target, its
// network, its profile, then the servers it publishes through. Only the first
// it meets is reported, so a Machine refused for two reasons names one.
func TestOnlyTheFirstRefusalAMachineMeetsIsReported(t *testing.T) {
	hinted := guest()
	hinted = hinted.WithSpec(hinted.Spec().WithPath(
		api.MapValue(text("deviceName", "/dev/vda"), field("rotational", api.BoolValue(false))), "os", "install", "rootDeviceHints"))
	jumbo := networkWith(api.MapValue(text("name", "enp1s0"), text("type", "ethernet"), number("mtu", "9000")))
	mirrored := installProfile()
	mirrored = mirrored.WithSpec(mirrored.Spec().WithPath(api.MapValue(field("mirror", api.MapValue(
		text("baseURL", "https://example.test/os")))), "installer", "anaconda", "packageSource"))
	onServices := artifactServer(text("machineRef", "services"), text("bindAddress", "192.0.2.2"))
	for name, test := range map[string]struct {
		catalog             api.Catalog
		reason, remediation string
	}{
		"a target before its network": {labCatalog(hinted, jumbo),
			"a managed-OS installation selects its root disk by deviceName alone and cannot carry the other root-device hints the Machine declares",
			"remove spec.os.install.rootDeviceHints.rotational from Machine/rhel-01"},
		"a network before its profile": {labCatalog(jumbo, mirrored), uncarriedReason,
			"remove mtu 9000 on enp1s0 from the network of Machine/rhel-01, or install its operating system outside Bootwright"},
		"a profile before its servers": {labCatalog(mirrored, servicesHost(), onServices),
			"the install profile selects spec.installer.anaconda.packageSource.mirror, which carries secret bytes or effects this executable does not prove",
			"remove spec.installer.anaconda.packageSource.mirror from MachineInstallProfile/rhel-9-8"},
	} {
		t.Run(name, func(t *testing.T) {
			want := []lifecycle.Refusal{{Kind: "Machine", Name: "rhel-01", Reason: test.reason, Remediation: test.remediation}}
			if refused := Refusals(test.catalog); !slices.Equal(refused, want) {
				t.Fatalf("refusals = %+v, want %+v", refused, want)
			}
		})
	}
}

// A physical controller fetches under the trust its Machine declares, so the
// provider-host rule is the emulated controller's alone: it says nothing about
// a physical target with a server off its placement, which is refused only
// for building its image off the controller, and a virtual Machine whose
// provider and server share one host off the controller is refused for that
// placement, not for its fetch.
func TestTheProviderHostRuleDoesNotRefuseAPhysicalTarget(t *testing.T) {
	sshPlaced := artifactServer(text("machineRef", "services"), text("bindAddress", "192.0.2.2"))
	metal := server(api.MapValue(text("deviceName", "/dev/sda")))
	catalog := catalogOf(environment(), controller(), servicesHost(), metalProvider(), networkConfig(), sshPlaced,
		service(api.DNSServer, "lab-dns", "53"), service(api.NTPServer, "lab-ntp", "123"), bootImage(), installProfile(), metal)
	profile, _ := catalog.Find(api.MachineInstallProfile, "rhel-9-8")
	derived, err := substrate.TargetFor(catalog, metal, "", "controller")
	if err != nil || !derived.Physical || derived.PlacementMachine.Name() != "controller" {
		t.Fatalf("target = %+v (%v)", derived, err)
	}
	if reason, remediation := refusedPublication(catalog, metal, profile, derived, true, ""); reason != "" {
		t.Fatalf("the provider-host rule refused a physical target: %q, %q", reason, remediation)
	}
	want := []lifecycle.Refusal{{Kind: "Machine", Name: "metal-01",
		Reason: "a managed-OS installation builds and publishes its installer image on the controller, and ArtifactServer/lab-artifacts is placed on Machine/services",
		Remediation: "place ArtifactServer/lab-artifacts on the controller Machine, or select a server placed there in " +
			"spec.installer.anaconda.redfishVirtualMedia.artifactServerEndpoint on MachineInstallProfile/rhel-9-8"}}
	if refused := Refusals(catalog); !slices.Equal(refused, want) {
		t.Fatalf("refusals = %+v", refused)
	}

	onServices := provider()
	onServices = onServices.WithSpec(onServices.Spec().WithPath(api.StringValue("services"), "libvirt", "machineRef"))
	shared := labCatalog(servicesHost(), sshPlaced, onServices)
	rhel, _ := shared.Find(api.Machine, "rhel-01")
	derived, err = substrate.TargetFor(shared, rhel, "", "controller")
	if err != nil || derived.Physical || derived.PlacementMachine.Name() != "services" {
		t.Fatalf("target = %+v (%v)", derived, err)
	}
	if reason, _ := refusedPublication(shared, rhel, profile, derived, true, ""); reason != "" {
		t.Fatalf("a provider and server on one host were refused for the fetch: %q", reason)
	}
	refused := Refusals(shared)
	if len(refused) != 1 || !strings.HasPrefix(refused[0].Reason, "a managed-OS installation builds and publishes its installer image on the controller") {
		t.Fatalf("refusals = %+v", refused)
	}
}

// A refusal the request builder still makes names the field that selects
// what it refuses, on the object that declares it: a publication endpoint is
// the profile's, and a store media reference is the image's or the profile's.
func TestRequestRefusalsNameTheFieldThatDeclaresIt(t *testing.T) {
	unresolved := installProfile()
	unresolved = unresolved.WithSpec(unresolved.Spec().WithPath(api.StringValue("absent"),
		"installer", "anaconda", "redfishVirtualMedia", "artifactServerEndpoint", "endpointRef"))
	treeless := installProfile()
	treeless = treeless.WithSpec(treeless.Spec().WithPath(api.StringValue("https://example.test/dvd.iso"),
		"installer", "anaconda", "packageSource", "hostedTree", "fromMedia"))
	for name, test := range map[string]struct {
		catalog     api.Catalog
		remediation string
	}{
		"an image endpoint": {labCatalog(unresolved), "correct artifactServerEndpoint.endpointRef on MachineInstallProfile/rhel-9-8"},
		"boot media": {labCatalog(api.NewObject(api.MachineImage, "rhel-9-8-boot", api.Value{}, api.MapValue(
			text("bootMedia", "https://example.test/boot.iso"),
		))), "import the image with bootwright media add --name <filename.iso> and set spec.bootMedia to local-media:<filename.iso> on MachineImage/rhel-9-8-boot"},
		"tree media": {labCatalog(treeless), "import the image with bootwright media add --name <filename.iso> and set " +
			"spec.installer.anaconda.packageSource.hostedTree.fromMedia to local-media:<filename.iso> on MachineInstallProfile/rhel-9-8"},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := Requests(test.catalog, "controller", testContext)
			if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Remediation != test.remediation {
				t.Fatalf("refusal = %#v", reported)
			}
		})
	}
}

// The content digest binds a plan to this build's behavior, so deleting the
// tooling list it once named must leave its input, and every frozen plan,
// unchanged.
func TestTheContentDigestIsPinned(t *testing.T) {
	if digest := ContentDigest(); digest != "061b55ae8e72f109a21e114e488ca4412f8de130c8950468270d65d8936e2132" {
		t.Fatalf("content digest = %s", digest)
	}
}
