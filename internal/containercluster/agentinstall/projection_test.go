package agentinstall

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate"
)

func onlyRequests(t *testing.T, catalog api.Catalog) (MediaRequest, InstallRequest, Requirements) {
	t.Helper()
	media, installs, needs, err := Requests(catalog, "controller", testContext)
	if err != nil {
		t.Fatalf("deriving: %v", diagnostics.Of(err))
	}
	if len(media) != 1 || len(installs) != 1 || len(needs) != 1 {
		t.Fatalf("requests = %d media, %d install", len(media), len(installs))
	}
	return media[0], installs[0], needs[0]
}

func encoded(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func refusalCode(t *testing.T, err error) string {
	t.Helper()
	reported := diagnostics.Of(err)
	if len(reported) == 0 {
		t.Fatalf("error carries no diagnostic: %v", err)
	}
	return reported[0].Code
}

// The install configuration a single-node cluster installs from is derived
// whole from effective state. A byte golden is what makes a change to any part
// of that derivation visible, because the installer reads all of it.
func TestSingleNodeInstallConfigIsDerivedWhole(t *testing.T) {
	media, _, _ := onlyRequests(t, singleNodeCatalog())
	const want = `{"apiVersion":"v1","baseDomain":"lab.example.test",` +
		`"compute":[{"name":"worker","replicas":0}],` +
		`"controlPlane":{"name":"master","replicas":1},` +
		`"metadata":{"name":"sno"},` +
		`"networking":{"clusterNetwork":[{"cidr":"10.128.0.0/14","hostPrefix":23}],` +
		`"machineNetwork":[{"cidr":"198.51.100.0/24"}],"serviceNetwork":["172.30.0.0/16"]},` +
		`"platform":{"none":{}},"pullSecret":"","sshKey":""}`
	if got := encoded(t, media.InstallConfig); got != want {
		t.Fatalf("install config =\n%s\nwant\n%s", got, want)
	}
}

// The agent configuration names each node by the hardware address its own
// substrate realized, so the host the installer registers is the host the
// declaration meant.
func TestSingleNodeAgentConfigNamesTheRealizedHardware(t *testing.T) {
	media, _, _ := onlyRequests(t, singleNodeCatalog())
	const want = `{"additionalNTPSources":["192.0.2.1"],"apiVersion":"v1beta1",` +
		`"hosts":[{"hostname":"master-0",` +
		`"interfaces":[{"macAddress":"52:54:00:06:3e:11","name":"enp1s0"}],` +
		`"networkConfig":{"dns-resolver":{"config":{"server":["192.0.2.1"]}},` +
		`"interfaces":[{"ipv4":{"address":[{"ip":"198.51.100.21","prefix-length":24}],"dhcp":false,"enabled":true},` +
		`"ipv6":{"enabled":false},"mac-address":"52:54:00:06:3e:11","name":"enp1s0","state":"up","type":"ethernet"}],` +
		`"routes":{"config":[{"destination":"0.0.0.0/0","next-hop-address":"198.51.100.1",` +
		`"next-hop-interface":"enp1s0","table-id":254}]}},` +
		`"role":"master","rootDeviceHints":{"deviceName":"/dev/vda"}}],` +
		`"kind":"AgentConfig","metadata":{"name":"sno"},"rendezvousIP":"198.51.100.21"}`
	if got := encoded(t, media.AgentConfig); got != want {
		t.Fatalf("agent config =\n%s\nwant\n%s", got, want)
	}
}

// A single-node cluster installs on no platform whatever the graph derived,
// because the agent installer refuses every platform arm for one control-plane
// node and no compute nodes.
func TestASingleNodeClusterInstallsOnNoPlatform(t *testing.T) {
	media, _, _ := onlyRequests(t, singleNodeCatalog())
	platform, ok := media.InstallConfig["platform"].(map[string]any)
	if !ok || platform["none"] == nil {
		t.Fatalf("platform = %#v", media.InstallConfig["platform"])
	}
}

// A multi-node cluster installs on the platform it declared, and the addresses
// it answers at reach the installer as that platform's own virtual addresses.
func TestAMultiNodeClusterInstallsOnItsDeclaredPlatform(t *testing.T) {
	media, _, _ := onlyRequests(t, compactCatalog())
	const want = `{"baremetal":{"apiVIPs":["198.51.100.10"],"ingressVIPs":["198.51.100.11"],"provisioningNetwork":"Disabled"}}`
	if got := encoded(t, media.InstallConfig["platform"]); got != want {
		t.Fatalf("platform = %s, want %s", got, want)
	}
	if replicas := encoded(t, media.InstallConfig["controlPlane"]); replicas != `{"name":"master","replicas":3}` {
		t.Fatalf("control plane = %s", replicas)
	}
}

// A cluster whose endpoints something outside it answers declares its load
// balancer user-managed, because the installer owns neither address.
func TestExternallyAnsweredEndpointsDeclareAUserManagedLoadBalancer(t *testing.T) {
	media, _, _ := onlyRequests(t, externalCatalog())
	platform, _ := media.InstallConfig["platform"].(map[string]any)
	arm, _ := platform["baremetal"].(map[string]any)
	balancer, ok := arm["loadBalancer"].(map[string]any)
	if !ok || balancer["type"] != "UserManaged" {
		t.Fatalf("platform = %#v", platform)
	}
}

// Every node of a physical cluster is frozen as operator-owned hardware, which
// is what makes its installation consume the authorization for the content it
// erases. Selection refuses such a cluster today, so the freezing is exercised
// directly beneath that refusal.
func TestPhysicalNodesAreFrozenAsOperatorOwnedHardware(t *testing.T) {
	catalog := physicalCatalog()
	declared, _ := catalog.Find(api.ContainerCluster, "metal")
	nodes, err := nodeProjections(catalog, declared, testContext, "controller", &Requirements{})
	if err != nil {
		t.Fatalf("projecting: %v", diagnostics.Of(err))
	}
	install := InstallRequest{Nodes: frozenNodes(nodes)}
	if len(install.Nodes) != 3 || !install.Physical() {
		t.Fatalf("nodes = %+v", install.Nodes)
	}
	for _, node := range install.Nodes {
		if !node.Physical || node.Substrate != "baremetal" {
			t.Fatalf("node = %+v", node)
		}
		if !strings.HasPrefix(node.Controller.Endpoint, "https://") {
			t.Fatalf("controller = %+v", node.Controller)
		}
	}
}

// A physical node's boot is proved against exactly the NICs its Machine
// declares, in declared order, while a node its substrate created is proved by
// its own controller and freezes none, although its target carries the
// addresses its realization derived. Each fixture server declares one NIC,
// which cannot show an order, so the targets are built here.
func TestPhysicalNodesFreezeTheirDeclaredInterfacesInOrder(t *testing.T) {
	frozen := frozenNodes([]nodeProjection{
		{
			machine: server("metal-01", "198.51.100.41/24", "aa:bb:cc:dd:ee:01"), name: "master-0",
			target: substrate.Target{Physical: true, Substrate: "baremetal", Interfaces: []substrate.Interface{
				{Name: "enp2s0", MACAddress: "aa:bb:cc:dd:ee:02"}, {Name: "enp1s0", MACAddress: "aa:bb:cc:dd:ee:01"},
			}},
		},
		{
			machine: guest("sno-01", "198.51.100.21/24"), name: "master-1",
			target: substrate.Target{Substrate: "libvirt", Interfaces: []substrate.Interface{
				{Name: "enp1s0", MACAddress: "52:54:00:06:3e:11"},
			}},
		},
	})
	want := &Hardware{Interfaces: []Interface{
		{MACAddress: "aa:bb:cc:dd:ee:02", Name: "enp2s0"}, {MACAddress: "aa:bb:cc:dd:ee:01", Name: "enp1s0"},
	}}
	if len(frozen) != 2 || !reflect.DeepEqual(frozen[0].Hardware, want) {
		t.Fatalf("physical node froze %+v, want %+v", frozen[0].Hardware, want)
	}
	if frozen[1].Hardware != nil {
		t.Fatalf("virtual node froze %+v", frozen[1].Hardware)
	}
}

// No install request this build freezes for a virtual cluster names hardware,
// so its bytes carry no hardware key at all.
func TestAVirtualNodeFreezesNoHardware(t *testing.T) {
	_, install, _ := onlyRequests(t, singleNodeCatalog())
	canonical, err := install.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(canonical, []byte(`"hardware"`)) {
		t.Fatalf("a virtual cluster's install request froze hardware: %s", canonical)
	}
}

// The rendezvous host is the first master in node-name order, because that is
// the host the installer expects to run the bootstrap control plane.
func TestTheRendezvousHostIsTheFirstMasterInNodeOrder(t *testing.T) {
	media, _, _ := onlyRequests(t, compactCatalog())
	if media.AgentConfig["rendezvousIP"] != "198.51.100.31" {
		t.Fatalf("rendezvous = %v", media.AgentConfig["rendezvousIP"])
	}
}

// The names the controller must resolve before any node boots are the two API
// names and one name beneath the applications wildcard, because a name beneath
// it is how every route is reached.
func TestTheControllerMustResolveEveryEndpointName(t *testing.T) {
	_, install, _ := onlyRequests(t, singleNodeCatalog())
	want := []string{
		"api-int.sno.lab.example.test", "api.sno.lab.example.test",
		"console-openshift-console.apps.sno.lab.example.test",
	}
	if len(install.Endpoints) != len(want) {
		t.Fatalf("endpoints = %+v", install.Endpoints)
	}
	for index, endpoint := range install.Endpoints {
		if endpoint.Name != want[index] || endpoint.Address != "198.51.100.21" {
			t.Fatalf("endpoint %d = %+v", index, endpoint)
		}
	}
}

// Every service the installation uses is a requirement, so those blocks
// complete before the installer needs what they realize.
func TestRequirementsNameEveryServiceTheInstallationUses(t *testing.T) {
	_, _, needs := onlyRequests(t, singleNodeCatalog())
	if len(needs.ArtifactServers) != 1 || needs.ArtifactServers[0] != "lab-artifacts" {
		t.Fatalf("artifact servers = %v", needs.ArtifactServers)
	}
	if len(needs.DNSServers) != 1 || needs.DNSServers[0] != "lab-dns" {
		t.Fatalf("dns servers = %v", needs.DNSServers)
	}
	if len(needs.NTPServers) != 1 || needs.NTPServers[0] != "lab-ntp" {
		t.Fatalf("ntp servers = %v", needs.NTPServers)
	}
	if len(needs.Machines) != 1 || needs.Machines[0] != "sno-01" {
		t.Fatalf("machines = %v", needs.Machines)
	}
}

// siteServicesCatalog is the lab-sno shape with its name and time services
// the site's own, selected without an endpoint as an external selection must
// be.
func siteServicesCatalog() api.Catalog {
	var objects []api.Object
	for _, object := range base() {
		switch object.Kind() {
		case api.DNSServer:
			object = api.NewObject(api.DNSServer, "lab-dns", api.Value{}, api.MapValue(
				text("management", "external"), text("address", "203.0.113.53")))
		case api.NTPServer:
			object = api.NewObject(api.NTPServer, "lab-ntp", api.Value{}, api.MapValue(
				text("management", "external"), text("address", "ntp.example.test")))
		case api.NetworkConfig:
			object = object.WithSpec(object.Spec().With("dns", api.ListValue(api.MapValue(text("serverRef", "lab-dns")))))
		}
		objects = append(objects, object)
	}
	return api.NewCatalog(append(objects, guest("sno-01", "198.51.100.21/24"), cluster("sno",
		installSelection(endpoints("198.51.100.21", "198.51.100.21", "198.51.100.21", "node")).
			With("ntp", api.ListValue(api.MapValue(text("serverRef", "lab-ntp")))),
		node("master-0", "master", "sno-01", "master-0.sno.lab.example.test"))))
}

// A site's own name and time services are used at the addresses they declare,
// and no block of this product realizes them, so neither the installation nor
// its plan waits for either.
func TestExternalServicesAreNoRequirement(t *testing.T) {
	catalog := siteServicesCatalog()
	if unsupported := Unsupported(catalog); len(unsupported) != 0 {
		t.Fatalf("unsupported = %v", unsupported)
	}
	media, _, needs := onlyRequests(t, catalog)
	if len(needs.DNSServers) != 0 || len(needs.NTPServers) != 0 {
		t.Fatalf("requirements = %+v", needs)
	}
	for _, required := range installRequires(needs) {
		if required.Kind == "DNSServer" || required.Kind == "NTPServer" {
			t.Fatalf("the installation requires %+v", required)
		}
	}
	agentConfig := encoded(t, media.AgentConfig)
	for _, address := range []string{`"server":["203.0.113.53"]`, `"additionalNTPSources":["ntp.example.test"]`} {
		if !strings.Contains(agentConfig, address) {
			t.Fatalf("the agent config carries no %s: %s", address, agentConfig)
		}
	}
}

// The boot image is published where only the machine booting it can find it,
// because it carries the pull secret in its own ignition.
func TestTheBootImageIsPublishedPrivately(t *testing.T) {
	media, install, _ := onlyRequests(t, singleNodeCatalog())
	const path = "/var/lib/bootwright-services/lab/artifact-server/lab-artifacts/public/private/clusters/sno"
	if media.Image.Path != path || media.Image.URL != "https://192.0.2.1:8443/private/clusters/sno" {
		t.Fatalf("image = %+v", media.Image)
	}
	if install.Image != media.Image {
		t.Fatal("the install block reads another image than the one its media block published")
	}
}

// Every declaration this contract cannot install refuses before an operation
// registers, naming the cluster an operator would change.
func TestUnsupportedNamesEveryClusterThisContractCannotInstall(t *testing.T) {
	for name, mutate := range map[string]func(api.Value) api.Value{
		"okd": func(spec api.Value) api.Value {
			return spec.WithPath(api.StringValue("okd"), "distribution", "type")
		},
		"image-only release": func(spec api.Value) api.Value {
			return spec.With("distribution", api.MapValue(text("type", "openshift"),
				field("release", api.MapValue(text("image", "quay.io/openshift/release:4.21.15")))))
		},
		"disconnected": func(spec api.Value) api.Value {
			return spec.WithPath(api.StringValue("disconnected"), "install", "mode")
		},
		"fips": func(spec api.Value) api.Value {
			return spec.With("security", api.MapValue(field("fips", api.MapValue(field("enabled", api.BoolValue(true))))))
		},
		"disk encryption": func(spec api.Value) api.Value {
			return spec.With("security", api.MapValue(field("diskEncryption", api.MapValue(
				field("unlock", api.MapValue(field("tpm2", api.MapValue())))))))
		},
		"registry policy": func(spec api.Value) api.Value {
			return spec.WithPath(api.MapValue(field("mirror", api.MapValue(text("registryRef", "mirror")))), "install", "registries")
		},
		"serving certificates": func(spec api.Value) api.Value {
			return spec.WithPath(api.MapValue(text("defaultCertificateRef", "ingress-tls")), "install", "servingCertificates", "ingress")
		},
		"proxied installation": func(spec api.Value) api.Value {
			return spec.WithPath(api.MapValue(text("proxyRef", "lab-proxy")), "install", "proxy")
		},
	} {
		t.Run(name, func(t *testing.T) {
			objects := append(base(), guest("sno-01", "198.51.100.21/24"))
			declared := cluster("sno",
				installSelection(endpoints("198.51.100.21", "198.51.100.21", "198.51.100.21", "node")),
				node("master-0", "master", "sno-01", "master-0.sno.lab.example.test"))
			objects = append(objects, declared.WithSpec(mutate(declared.Spec())))
			catalog := api.NewCatalog(objects)
			if unsupported := Unsupported(catalog); len(unsupported) != 1 || unsupported[0] != "ContainerCluster/sno" {
				t.Fatalf("unsupported = %v", unsupported)
			}
			if _, _, _, err := Requests(catalog, "controller", testContext); err == nil {
				t.Fatal("a cluster this contract cannot install was derived anyway")
			} else if code := refusalCode(t, err); code != "lifecycle.unsupported" {
				t.Fatalf("refusal = %s", code)
			}
		})
	}
}

// A cluster with a physical node refuses, because booting that node erases
// what it holds and that boot is not yet qualified on emulated hardware. A
// node Bootwright also installs an operating system on refuses too, because
// two installations would write its one disk. Either refusal names the bound
// Machine, because that is what an operator changes. A node whose realized
// target does not derive refuses with its substrate's own reason and the remedy
// naming the provider to correct.
func TestUnsupportedNamesEveryClusterWithANodeThisContractCannotBoot(t *testing.T) {
	installed := guest("sno-01", "198.51.100.21/24")
	installed = installed.WithSpec(installed.Spec().WithPath(api.StringValue("rhel-9-8"), "os", "installProfileRef"))
	for name, test := range map[string]struct {
		catalog     api.Catalog
		cluster     string
		reason      string
		remediation string
	}{
		"physical nodes": {
			physicalCatalog(), "ContainerCluster/metal",
			"physical cluster nodes are not supported until an emulated rehearsal qualifies them",
			"Machine/metal-01 is physical; declare ContainerCluster/metal on virtual nodes",
		},
		"installed node": {
			api.NewCatalog(append(base(), installed, cluster("sno",
				installSelection(endpoints("198.51.100.21", "198.51.100.21", "198.51.100.21", "node")),
				node("master-0", "master", "sno-01", "master-0.sno.lab.example.test")))),
			"ContainerCluster/sno",
			"a declared node selects an install profile, so two installations would write its disk",
			"remove spec.os.installProfileRef from Machine/sno-01 or drop it from ContainerCluster/sno",
		},
		"controller port past the port space": {
			singleNodeOnProvider(portPastTheSpace, guest("sno-00", "198.51.100.20/24")), "ContainerCluster/sno",
			"the Machine's emulated controller port does not allocate",
			"correct spec.libvirt.bmcEmulationDefaults.port on InfraProvider/lab-libvirt",
		},
		"no controller credential": {
			singleNodeOnProvider(noControllerCredential), "ContainerCluster/sno",
			"the provider declares no emulated controller credential",
			"set spec.libvirt.bmcEmulationDefaults.auth.credentialsRef on InfraProvider/lab-libvirt",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if unsupported := Unsupported(test.catalog); len(unsupported) != 1 || unsupported[0] != test.cluster {
				t.Fatalf("unsupported = %v", unsupported)
			}
			_, _, _, err := Requests(test.catalog, "controller", testContext)
			if err == nil {
				t.Fatal("a cluster this contract cannot boot was derived anyway")
			}
			reported := diagnostics.Of(err)
			if len(reported) != 1 || reported[0].Code != "lifecycle.unsupported" || reported[0].Message != test.reason {
				t.Fatalf("refusal = %#v", reported)
			}
			if reported[0].Remediation != test.remediation {
				t.Fatalf("remediation = %q", reported[0].Remediation)
			}
		})
	}
}

// A node's target error that carries no reason still refuses the cluster,
// because an empty reason would admit it.
func TestANodeTargetErrorWithoutAReasonStillRefuses(t *testing.T) {
	catalog := singleNodeCatalog()
	bound, _ := catalog.Find(api.Machine, "sno-01")
	for name, err := range map[string]error{
		"no diagnostic":   errors.New("no diagnostic"),
		"an empty reason": diagnostics.NewFailure("api.value", "", ""),
	} {
		t.Run(name, func(t *testing.T) {
			if reason, _ := nodeTargetRefusal(catalog, bound, err); reason != "a declared node's realized target does not derive" {
				t.Fatalf("reason = %q", reason)
			}
		})
	}
}

// A multi-node cluster on a platform the projection has no arm for refuses
// before registration with the same refusal as every other declaration this
// contract does not install, rather than later, when its installer inputs are
// derived. One node on the same platform installs on none and is accepted.
func TestAMultiNodeClusterOnAnotherPlatformRefusesBeforeRegistration(t *testing.T) {
	for _, platform := range []string{"vsphere", "external"} {
		t.Run(platform, func(t *testing.T) {
			objects := append(base(),
				guest("ocp-01", "198.51.100.31/24"), guest("ocp-02", "198.51.100.32/24"), guest("ocp-03", "198.51.100.33/24"))
			objects = append(objects, cluster("ocp",
				installSelection(
					endpoints("198.51.100.10", "198.51.100.10", "198.51.100.11", "external"),
					field("platform", api.MapValue(text("type", platform))),
				),
				node("master-0", "master", "ocp-01", "master-0.ocp.lab.example.test"),
				node("master-1", "master", "ocp-02", "master-1.ocp.lab.example.test"),
				node("master-2", "master", "ocp-03", "master-2.ocp.lab.example.test")))
			catalog := api.NewCatalog(objects)
			if unsupported := Unsupported(catalog); len(unsupported) != 1 || unsupported[0] != "ContainerCluster/ocp" {
				t.Fatalf("unsupported = %v", unsupported)
			}
			_, _, _, err := Requests(catalog, "controller", testContext)
			reported := diagnostics.Of(err)
			if len(reported) != 1 || reported[0].Code != "lifecycle.unsupported" ||
				reported[0].Message != "this executable installs no multi-node cluster on the declared platform" {
				t.Fatalf("refusal = %#v", reported)
			}

			single := append(base(), guest("sno-01", "198.51.100.21/24"), cluster("sno",
				installSelection(
					endpoints("198.51.100.21", "198.51.100.21", "198.51.100.21", "node"),
					field("platform", api.MapValue(text("type", platform))),
				),
				node("master-0", "master", "sno-01", "master-0.sno.lab.example.test")))
			if unsupported := Unsupported(api.NewCatalog(single)); len(unsupported) != 0 {
				t.Fatalf("a single node on %s is refused: %v", platform, unsupported)
			}
			media, _, _ := onlyRequests(t, api.NewCatalog(single))
			if got := encoded(t, media.InstallConfig["platform"]); got != `{"none":{}}` {
				t.Fatalf("single-node platform = %s", got)
			}
		})
	}
}

// A multi-node cluster that declares the none platform, or no platform at all,
// is accepted and installs on none, so the refusal above reaches only the
// platforms the projection has no arm for.
func TestAMultiNodeClusterOnNoPlatformInstallsOnNone(t *testing.T) {
	for name, declared := range map[string][]api.FieldValue{
		"none":       {field("platform", api.MapValue(text("type", "none")))},
		"undeclared": nil,
	} {
		t.Run(name, func(t *testing.T) {
			objects := append(base(),
				guest("ocp-01", "198.51.100.31/24"), guest("ocp-02", "198.51.100.32/24"), guest("ocp-03", "198.51.100.33/24"))
			objects = append(objects, cluster("ocp",
				installSelection(append([]api.FieldValue{
					endpoints("198.51.100.10", "198.51.100.10", "198.51.100.11", "external"),
				}, declared...)...),
				node("master-0", "master", "ocp-01", "master-0.ocp.lab.example.test"),
				node("master-1", "master", "ocp-02", "master-1.ocp.lab.example.test"),
				node("master-2", "master", "ocp-03", "master-2.ocp.lab.example.test")))
			catalog := api.NewCatalog(objects)
			if unsupported := Unsupported(catalog); len(unsupported) != 0 {
				t.Fatalf("unsupported = %v", unsupported)
			}
			media, _, _ := onlyRequests(t, catalog)
			if got := encoded(t, media.InstallConfig["platform"]); got != `{"none":{}}` {
				t.Fatalf("platform = %s", got)
			}
		})
	}
}

// hypervisor is a second libvirt host, hv-01, with the provider far-libvirt it
// hosts and the artifact server far-artifacts placed on it, so a node and the
// server its boot image is fetched from can be placed on different Machines.
func hypervisor() []api.Object {
	host := api.NewObject(api.Machine, "hv-01", api.Value{}, api.MapValue(
		field("capabilities", api.StringList("libvirt")),
		field("os", api.MapValue(field("provided", api.BoolValue(true)))),
		field("network", api.MapValue(field("addresses", api.ListValue(
			api.MapValue(text("name", "ip"), text("address", "192.0.2.2")),
		)))),
		field("access", api.MapValue(field("ssh", api.MapValue(
			text("addressRef", "ip"), text("knownHostsRef", "hv-01-host-key"),
			field("auth", api.MapValue(text("privateKeyRef", "hv-01-key"))),
		)))),
	))
	provider := api.NewObject(api.InfraProvider, "far-libvirt", api.Value{}, libvirtProvider().Spec().
		WithPath(api.StringValue("hv-01"), "libvirt", "machineRef").
		WithPath(api.StringValue("192.0.2.2"), "libvirt", "bmcEmulationDefaults", "bindAddress"))
	farServer := api.NewObject(api.ArtifactServer, "far-artifacts", api.Value{}, artifactServer().Spec().
		WithPath(api.StringValue("hv-01"), "machineRef").
		WithPath(api.StringValue("192.0.2.2"), "bindAddress"))
	return []api.Object{host, provider, farServer}
}

// servedBy selects another artifact server for the cluster's boot image.
func servedBy(server string, declared api.Object) api.Object {
	return declared.WithSpec(declared.Spec().WithPath(api.StringValue(server),
		"install", "agent", "redfishVirtualMedia", "artifactServerEndpoint", "serverRef"))
}

// onRemote places a guest on the provider hv-01 hosts.
func onRemote(machine api.Object) api.Object {
	return machine.WithSpec(machine.Spec().WithPath(api.StringValue("far-libvirt"), "substrate", "providerRef"))
}

// An emulated controller fetches the private boot image without verifying the
// server, so the image is safe only while that fetch never leaves the provider
// host the controller runs on. A node on a provider hosted anywhere but the
// artifact server's placement Machine refuses before registration, naming the
// node, its provider host, the server and the Machine it is placed on.
func TestANodeOffTheArtifactServersHostRefusesBeforeRegistration(t *testing.T) {
	for name, test := range map[string]struct {
		objects     []api.Object
		cluster     string
		remediation string
	}{
		"single node": {
			[]api.Object{onRemote(guest("sno-01", "198.51.100.21/24")), cluster("sno",
				installSelection(endpoints("198.51.100.21", "198.51.100.21", "198.51.100.21", "node")),
				node("master-0", "master", "sno-01", "master-0.sno.lab.example.test"))},
			"ContainerCluster/sno",
			"Machine/sno-01 is booted through a controller on Machine/hv-01 and ArtifactServer/lab-artifacts is placed on Machine/controller; " +
				"place InfraProvider/far-libvirt and ArtifactServer/lab-artifacts on the same Machine",
		},
		"one node of three": {
			[]api.Object{
				guest("ocp-01", "198.51.100.31/24"), guest("ocp-02", "198.51.100.32/24"), onRemote(guest("ocp-03", "198.51.100.33/24")),
				cluster("ocp",
					installSelection(
						endpoints("198.51.100.10", "198.51.100.10", "198.51.100.11", "openshift"),
						field("platform", api.MapValue(text("type", "baremetal"))),
					),
					node("master-0", "master", "ocp-01", "master-0.ocp.lab.example.test"),
					node("master-1", "master", "ocp-02", "master-1.ocp.lab.example.test"),
					node("master-2", "master", "ocp-03", "master-2.ocp.lab.example.test")),
			},
			"ContainerCluster/ocp",
			"Machine/ocp-03 is booted through a controller on Machine/hv-01 and ArtifactServer/lab-artifacts is placed on Machine/controller; " +
				"place InfraProvider/far-libvirt and ArtifactServer/lab-artifacts on the same Machine",
		},
		"server off the provider host": {
			[]api.Object{guest("sno-01", "198.51.100.21/24"), servedBy("far-artifacts", cluster("sno",
				installSelection(endpoints("198.51.100.21", "198.51.100.21", "198.51.100.21", "node")),
				node("master-0", "master", "sno-01", "master-0.sno.lab.example.test")))},
			"ContainerCluster/sno",
			"Machine/sno-01 is booted through a controller on Machine/controller and ArtifactServer/far-artifacts is placed on Machine/hv-01; " +
				"place InfraProvider/lab-libvirt and ArtifactServer/far-artifacts on the same Machine",
		},
	} {
		t.Run(name, func(t *testing.T) {
			catalog := api.NewCatalog(append(append(base(), hypervisor()...), test.objects...))
			if unsupported := Unsupported(catalog); len(unsupported) != 1 || unsupported[0] != test.cluster {
				t.Fatalf("unsupported = %v", unsupported)
			}
			_, _, _, err := Requests(catalog, "controller", testContext)
			reported := diagnostics.Of(err)
			const reason = "an emulated controller fetches the boot image without verifying its server, " +
				"so the server is placed on the provider host that controller runs on"
			if len(reported) != 1 || reported[0].Code != "lifecycle.unsupported" || reported[0].Message != reason {
				t.Fatalf("refusal = %#v", reported)
			}
			if reported[0].Remediation != test.remediation {
				t.Fatalf("remediation = %q", reported[0].Remediation)
			}
		})
	}
}

// The rule is that the node's provider host is the server's placement Machine,
// not that both are the controller: a node on hv-01 booted from a server on
// hv-01 is not refused by it. Derivation still refuses that server, because the
// image is built where the installer is, on the controller.
func TestANodeOnTheArtifactServersHostIsNotRefusedForItsFetch(t *testing.T) {
	objects := append(append(base(), hypervisor()...), onRemote(guest("sno-01", "198.51.100.21/24")), servedBy("far-artifacts",
		cluster("sno",
			installSelection(endpoints("198.51.100.21", "198.51.100.21", "198.51.100.21", "node")),
			node("master-0", "master", "sno-01", "master-0.sno.lab.example.test"))))
	catalog := api.NewCatalog(objects)
	if unsupported := Unsupported(catalog); len(unsupported) != 0 {
		t.Fatalf("unsupported = %v", unsupported)
	}
	_, _, _, err := Requests(catalog, "controller", testContext)
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "lifecycle.state" ||
		reported[0].Message != "a cluster's boot image is built on the controller, so the server it is published through is placed there" {
		t.Fatalf("refusal = %#v", reported)
	}
}

// A selection that resolves no managed server leaves the fetch with no server
// to compare, so derivation refuses the selection itself rather than the
// provider host being refused against an empty placement.
func TestAnUnresolvedServerIsRefusedAsTheSelection(t *testing.T) {
	external := api.NewObject(api.ArtifactServer, "lab-artifacts", api.Value{}, api.MapValue(
		text("management", "external"),
		field("endpoints", api.ListValue(
			api.MapValue(text("name", "ip-https"), text("url", "https://artifacts.lab.example.test:8443")),
		)),
	))
	var objects []api.Object
	for _, object := range base() {
		if object.Kind() != api.ArtifactServer {
			objects = append(objects, object)
		}
	}
	objects = append(objects, external, guest("sno-01", "198.51.100.21/24"), cluster("sno",
		installSelection(endpoints("198.51.100.21", "198.51.100.21", "198.51.100.21", "node")),
		node("master-0", "master", "sno-01", "master-0.sno.lab.example.test")))
	catalog := api.NewCatalog(objects)
	if unsupported := Unsupported(catalog); len(unsupported) != 0 {
		t.Fatalf("unsupported = %v", unsupported)
	}
	_, _, _, err := Requests(catalog, "controller", testContext)
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "lifecycle.state" ||
		reported[0].Message != "a consumer publishes only into a managed artifact server" ||
		reported[0].Remediation != "select a managed server on ContainerCluster/sno" {
		t.Fatalf("refusal = %#v", reported)
	}
}

// A supported cluster is named by nothing, so an operation registers.
func TestASupportedClusterIsNotRefused(t *testing.T) {
	for name, catalog := range map[string]api.Catalog{
		"single node": singleNodeCatalog(), "compact": compactCatalog(),
	} {
		t.Run(name, func(t *testing.T) {
			if unsupported := Unsupported(catalog); len(unsupported) != 0 {
				t.Fatalf("unsupported = %v", unsupported)
			}
		})
	}
}

func refusedWith(t *testing.T, err error, reason, remediation string) {
	t.Helper()
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "lifecycle.unsupported" || reported[0].Message != reason {
		t.Fatalf("refusal = %#v", reported)
	}
	if reported[0].Remediation != remediation {
		t.Fatalf("remediation = %q", reported[0].Remediation)
	}
}

func hostHints(t *testing.T, media MediaRequest) map[string]any {
	t.Helper()
	hosts, _ := media.AgentConfig["hosts"].([]any)
	found := map[string]any{}
	for _, entry := range hosts {
		host, _ := entry.(map[string]any)
		name, _ := host["hostname"].(string)
		found[name] = host["rootDeviceHints"]
	}
	return found
}

// Every root-device hint a node declares reaches the agent configuration under
// its admitted name and with its declared type, a size of zero and a disk that
// is not rotational included. Only a bare-metal Machine is admitted with every
// hint, and selection refuses a physical cluster today, so its configuration is
// projected directly beneath that refusal. A node that selects its disk by wwn
// alone keeps that selection rather than rendering none.
func TestEveryDeclaredRootDeviceHintReachesTheAgentConfig(t *testing.T) {
	catalog := physicalHintsCatalog()
	declared, _ := catalog.Find(api.ContainerCluster, "metal")
	nodes, err := nodeProjections(catalog, declared, testContext, "controller", &Requirements{})
	if err != nil {
		t.Fatalf("projecting: %v", diagnostics.Of(err))
	}
	config, err := agentConfig(declared, nodes, nil)
	if err != nil {
		t.Fatalf("configuring: %v", diagnostics.Of(err))
	}
	want := map[string]any{
		"master-0": map[string]any{
			"deviceName": "/dev/disk/by-path/pci-0000:00:04.0", "hctl": "1:0:0:0", "model": "1e3", "vendor": "0o17",
			"serialNumber": "0987654321", "wwn": "0x5000c500a1b2c3d4", "minSizeGigabytes": int64(0), "rotational": false,
		},
		"master-1": map[string]any{"wwn": "0x5000c500a1b2c3d5"},
		"master-2": map[string]any{"deviceName": "/dev/sda"},
	}
	if got := hostHints(t, MediaRequest{AgentConfig: config}); !reflect.DeepEqual(got, want) {
		t.Fatalf("hints =\n%#v\nwant\n%#v", got, want)
	}
}

// Every hint a virtual node is admitted with reaches the agent configuration
// through selection, and a node that selects its disk by size alone keeps that
// selection rather than rendering none.
func TestEveryHintAVirtualNodeDeclaresReachesTheAgentConfig(t *testing.T) {
	media, _, _ := onlyRequests(t, hintsCatalog())
	want := map[string]any{
		"master-0": map[string]any{
			"deviceName": "/dev/disk/by-path/pci-0000:00:04.0", "model": "1e3", "vendor": "0o17",
			"minSizeGigabytes": int64(0), "rotational": false,
		},
		"master-1": map[string]any{"minSizeGigabytes": int64(100)},
		"master-2": map[string]any{"deviceName": "/dev/vda"},
	}
	if got := hostHints(t, media); !reflect.DeepEqual(got, want) {
		t.Fatalf("hints =\n%#v\nwant\n%#v", got, want)
	}
}

// The agent installer names a root device only as a name directly beneath
// /dev/ or beneath /dev/disk/by-path/, and the frozen request carries a size
// exactly only up to 2^53-1. A node declaring anything else refuses before
// registration, naming the bound Machine, and the projection itself refuses it
// the same way, so no caller derives an input that selection refuses.
func TestARootDeviceTheAgentInstallerCannotCarryRefuses(t *testing.T) {
	const (
		unnamed   = "the agent installer names a root device only as /dev/<name> or /dev/disk/by-path/<name>"
		rename    = "set spec.os.install.rootDeviceHints.deviceName on Machine/sno-01 to such a path"
		oversized = "a node's minSizeGigabytes is larger than the frozen installer input carries exactly"
		shrink    = "declare spec.os.install.rootDeviceHints.minSizeGigabytes on Machine/sno-01 as at most 9007199254740991"
	)
	for name, test := range map[string]struct {
		hints       []api.FieldValue
		reason      string
		remediation string
	}{
		"a by-id link":   {[]api.FieldValue{text("deviceName", "/dev/disk/by-id/wwn-0x5000c500a1b2c3d4")}, unnamed, rename},
		"a mapper":       {[]api.FieldValue{text("deviceName", "/dev/mapper/root")}, unnamed, rename},
		"a size past it": {[]api.FieldValue{text("deviceName", "/dev/vda"), number("minSizeGigabytes", "9007199254740992")}, oversized, shrink},
		"a size past every integer type": {
			[]api.FieldValue{text("deviceName", "/dev/vda"), number("minSizeGigabytes", "100000000000000000000000")}, oversized, shrink,
		},
	} {
		t.Run(name, func(t *testing.T) {
			catalog := singleNodeWith(test.hints...)
			if unsupported := Unsupported(catalog); !slices.Equal(unsupported, []string{"ContainerCluster/sno"}) {
				t.Fatalf("unsupported = %v", unsupported)
			}
			// The refusal plan and apply report before registration carries
			// the cluster, the reason and the remedy naming the Machine.
			want := []lifecycle.Refusal{{Kind: "ContainerCluster", Name: "sno", Reason: test.reason, Remediation: test.remediation}}
			if refused := Refusals(catalog); !slices.Equal(refused, want) {
				t.Fatalf("refusals = %+v, want %+v", refused, want)
			}
			_, _, _, err := Requests(catalog, "controller", testContext)
			refusedWith(t, err, test.reason, test.remediation)
			declared, _ := catalog.Find(api.ContainerCluster, "sno")
			_, err = nodeProjections(catalog, declared, testContext, "controller", &Requirements{})
			refusedWith(t, err, test.reason, test.remediation)
		})
	}
}

// A device directly beneath /dev/ or beneath /dev/disk/by-path/ is one the
// agent installer names, so it reaches the agent configuration unrefused.
func TestARootDeviceTheAgentInstallerCanNameIsNotRefused(t *testing.T) {
	for _, device := range []string{"/dev/sda", "/dev/disk/by-path/pci-0000:00:1f.2-ata-1"} {
		t.Run(device, func(t *testing.T) {
			catalog := singleNodeWith(text("deviceName", device))
			if unsupported := Unsupported(catalog); len(unsupported) != 0 {
				t.Fatalf("unsupported = %v", unsupported)
			}
			media, _, _ := onlyRequests(t, catalog)
			if got := hostHints(t, media); !reflect.DeepEqual(got, map[string]any{"master-0": map[string]any{"deviceName": device}}) {
				t.Fatalf("hints = %#v", got)
			}
		})
	}
}

// The frozen media request is read back through float64, so the largest size
// it carries exactly is 2^53-1. One past it would freeze at plan and then be
// refused as not canonical when execution reads it, which is why selection
// refuses such a size instead.
func TestARootDeviceSizeTheFrozenInputCannotCarryRefuses(t *testing.T) {
	media, _, _ := onlyRequests(t, singleNodeWith(text("deviceName", "/dev/vda"), number("minSizeGigabytes", "9007199254740991")))
	frozen, err := media.Canonical()
	if err != nil {
		t.Fatalf("freezing the largest size: %v", diagnostics.Of(err))
	}
	if _, err := DecodeMediaRequest(frozen); err != nil {
		t.Fatalf("decoding the largest size: %v", diagnostics.Of(err))
	}
	past := MediaRequest{Version: mediaRequestVersion, AgentConfig: map[string]any{"minSizeGigabytes": int64(9007199254740993)}}
	frozen, err = past.Canonical()
	if err != nil {
		t.Fatalf("freezing a size past the bound: %v", diagnostics.Of(err))
	}
	_, err = DecodeMediaRequest(frozen)
	if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Message != "the frozen cluster media request is not canonical" {
		t.Fatalf("decoding a size past the bound = %#v", reported)
	}
}

// Only the agent configuration carries the hints: the install request boots
// the same nodes whatever disk each one selects.
func TestTheInstallRequestCarriesNoRootDeviceHint(t *testing.T) {
	_, hinted, _ := onlyRequests(t, hintsCatalog())
	_, compact, _ := onlyRequests(t, compactCatalog())
	hintedBytes, err := hinted.Canonical()
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	compactBytes, err := compact.Canonical()
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if !bytes.Equal(hintedBytes, compactBytes) {
		t.Fatalf("the install request changed with the hints:\n%s\n%s", hintedBytes, compactBytes)
	}
}
