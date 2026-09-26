package agentinstall

import (
	"encoding/json"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
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
	objects := append(base(),
		guest("ocp-01", "198.51.100.31/24"), guest("ocp-02", "198.51.100.32/24"), guest("ocp-03", "198.51.100.33/24"))
	objects = append(objects, cluster("ocp",
		installSelection(
			endpoints("198.51.100.20", "198.51.100.20", "198.51.100.21", "external"),
			field("platform", api.MapValue(text("type", "baremetal"))),
		),
		node("master-0", "master", "ocp-01", "master-0.ocp.lab.example.test"),
		node("master-1", "master", "ocp-02", "master-1.ocp.lab.example.test"),
		node("master-2", "master", "ocp-03", "master-2.ocp.lab.example.test")))
	media, _, _ := onlyRequests(t, api.NewCatalog(objects))
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
			} else if code := refusalCode(t, err); code != "lifecycle.state" {
				t.Fatalf("refusal = %s", code)
			}
		})
	}
}

// A cluster with a physical node refuses, because booting that node erases
// what it holds and nothing proves the node is the declared machine, powered
// off, before it is booted. A node Bootwright also installs an operating
// system on refuses too, because two installations would write its one disk.
// Either refusal names the bound Machine, because that is what an operator
// changes.
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
			"physical cluster nodes are not supported until the installer proves each node before booting it",
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
			if len(reported) != 1 || reported[0].Code != "lifecycle.state" || reported[0].Message != test.reason {
				t.Fatalf("refusal = %#v", reported)
			}
			if reported[0].Remediation != test.remediation {
				t.Fatalf("remediation = %q", reported[0].Remediation)
			}
		})
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
