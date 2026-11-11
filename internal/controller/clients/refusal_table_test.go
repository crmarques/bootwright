package clients

import (
	"maps"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// refusalSpec owns this capability's refusal table, read from this package's
// directory.
const refusalSpec = "../../../specs/api/environment.md"

// refusalCase is a selected graph that one row of the refusal table refuses,
// and the value of each placeholder the row's reason and remedy write in angle
// brackets. Every refusal names the Environment the capability plans for.
type refusalCase struct {
	objects []api.Object
	values  map[string]string
}

// proxied is the controller Machine selecting the Proxy egress, with bypass
// entries when given.
func proxied(bypass ...string) api.Object {
	choice := api.MapValue(text("proxyRef", "egress"))
	if len(bypass) != 0 {
		choice = choice.With("noProxy", api.StringList(bypass...))
	}
	controller := machine()
	return controller.WithSpec(controller.Spec().With("proxy", choice))
}

// externalProxy is the external Proxy egress with the given connection.
func externalProxy(connection ...api.FieldValue) api.Object {
	return api.NewObject(api.Proxy, "egress", api.Value{}, api.MapValue(text("management", "external"), field("connection", api.MapValue(connection...))))
}

// TestControllerRefusalTableMatchesUnsupported holds the controller stage
// capability's Unsupported to the Environment refusal table: every row refuses
// its graph with exactly its reason and remedy, and a row added or removed on
// either side fails here.
func TestControllerRefusalTableMatchesUnsupported(t *testing.T) {
	endpoint := text("httpsProxy", "http://proxy.example.test:3128")
	names := map[string]string{"<machine>": "Machine/controller", "<proxy>": "Proxy/egress", "<environment>": "Environment/lab"}
	bypass := make([]string, 129)
	for index := range bypass {
		bypass[index] = "host-" + strconv.Itoa(index) + ".example.test"
	}
	cases := map[string]refusalCase{
		"Too many bypass entries": {[]api.Object{proxied(bypass...), externalProxy(endpoint)}, names},
		"Managed Proxy": {[]api.Object{proxied(), api.NewObject(api.Proxy, "egress", api.Value{}, api.MapValue(text("management", "managed")))},
			names},
		"Proxy authentication": {[]api.Object{proxied(), externalProxy(endpoint, field("auth", api.MapValue(text("proxyAuthRef", "proxy-credentials"))))},
			names},
		"Private trust":       {[]api.Object{proxied(), externalProxy(endpoint, text("trustBundleRef", "corporate-ca"))}, names},
		"HTTP proxy alone":    {[]api.Object{proxied(), externalProxy(text("httpProxy", "http://proxy.example.test:3128"))}, names},
		"Libvirt release pin": {append([]api.Object{environment(field("dependencyVersions", api.MapValue(text("libvirt", "10.0.0"))))}, libvirtGraph()[1:]...), names},
		"Client release below the minimum": {[]api.Object{environment(field("dependencyVersions", api.MapValue(text("helm", "1.0.0")))), machine(), cluster()},
			clientRelease(t, names, "1.0.0")},
		"Client release without MAJOR.MINOR.PATCH": {[]api.Object{environment(field("dependencyVersions", api.MapValue(text("helm", "4.3")))), machine(), cluster()},
			clientRelease(t, names, "4.3")},
		"Per-client mirror beside the artifact mirror": {[]api.Object{environment(artifactMirror(), field("downloads", api.MapValue(text("helmMirror", "https://other.example.test")))), machine(), cluster()},
			withValues(names, "<field>", "helmMirror")},
		"Unpinned client behind the artifact mirror": {[]api.Object{environment(artifactMirror(), field("dependencyVersions", api.MapValue(text("helm", "99.0.0")))), machine(), cluster()},
			withValues(names, "<tool>", "helm", "<release>", "99.0.0")},
		"OKD clients behind the artifact mirror": {[]api.Object{environment(artifactMirror()), machine(), okdCluster()},
			withValues(names, "<tool>", "openshift-clients", "<release>", "4.19.3")},
		"Managed artifact mirror on the controller": {append([]api.Object{environment(managedArtifactMirror("ip")), machine(), cluster()}, mirrorServer("controller")...),
			withValues(names, "<server>", "ArtifactServer/mirror", "<host>", "Machine/controller")},
		"Managed artifact mirror on a provider host": {append([]api.Object{environment(managedArtifactMirror("ip")), machine(), cluster(), mirrorMachine(),
			api.NewObject(api.InfraProvider, "lab-libvirt", api.Value{}, api.MapValue(field("libvirt", api.MapValue(text("machineRef", "mirror")))))}, mirrorServer("mirror")...),
			withValues(names, "<server>", "ArtifactServer/mirror", "<host>", "Machine/mirror", "<provider>", "InfraProvider/lab-libvirt")},
		"Managed artifact mirror behind a managed Proxy": {append([]api.Object{environment(managedArtifactMirror("ip")), machine(), cluster(), mirrorMachine(text("proxyRef", "egress")),
			api.NewObject(api.Proxy, "egress", api.Value{}, api.MapValue(text("management", "managed")))}, mirrorServer("mirror")...),
			withValues(names, "<host>", "Machine/mirror", "<proxy>", "Proxy/egress")},
		"Managed artifact mirror without an HTTPS URL": {append([]api.Object{environment(managedArtifactMirror("missing")), machine(), cluster(), mirrorMachine()}, mirrorServer("mirror")...),
			withValues(names, "<server>", "ArtifactServer/mirror")},
		"Managed image mirror": {[]api.Object{environment(field("mirror", api.MapValue(field("registry", api.MapValue(text("registryRef", "mirror")))))), machine(), cluster(),
			api.NewObject(api.Registry, "mirror", api.Value{}, api.MapValue(text("management", "managed"), text("machineRef", "controller")))},
			withValues(names, "<registry>", "Registry/mirror")},
	}
	for _, row := range refusalTable(t) {
		test, found := cases[row.name]
		if !found {
			t.Errorf("the table row %q has no graph here that it refuses", row.name)
			continue
		}
		delete(cases, row.name)
		t.Run(row.name, func(t *testing.T) {
			want := []lifecycle.Refusal{{Kind: "Environment", Name: "lab", Reason: expand(row.reason, test.values), Remediation: expand(row.remedy, test.values)}}
			if got := (Capability{}).Unsupported(stateOf(withEnvironment(test.objects)...)); !reflect.DeepEqual(got, want) {
				t.Fatalf("the capability refuses %+v, the table %+v", got, want)
			}
		})
	}
	for _, name := range slices.Sorted(maps.Keys(cases)) {
		t.Errorf("the table has no row %q", name)
	}
	if got := (Capability{}).Unsupported(stateOf(environment(), proxied(".example.test"), externalProxy(endpoint))); len(got) != 0 {
		t.Fatalf("a supported controller was refused: %+v", got)
	}
}

// clientRelease adds a refused helm release and the build's minimum to the
// placeholders every row names.
func clientRelease(t *testing.T, names map[string]string, release string) map[string]string {
	t.Helper()
	pin, _, err := controller.PinOf("helm")
	if err != nil {
		t.Fatal(err)
	}
	values := maps.Clone(names)
	values["<tool>"], values["<minimum>"], values["<release>"] = "helm", pin.Minimum, release
	return values
}

// withEnvironment adds the plain Environment unless the graph declares its own.
func withEnvironment(objects []api.Object) []api.Object {
	if slices.ContainsFunc(objects, func(object api.Object) bool { return object.Kind() == api.Environment }) {
		return objects
	}
	return append([]api.Object{environment()}, objects...)
}

// refusalRow is one row of a refusal table: its name, and its reason and remedy
// read out of their code spans. Its path is for the reader alone.
type refusalRow struct {
	name, reason, remedy string
}

// refusalTable reads the one table under the refusal-table heading of the
// spec. It fails when the heading is missing or repeated, or when the header,
// the separator or a row does not match.
func refusalTable(t *testing.T) []refusalRow {
	t.Helper()
	data, err := os.ReadFile(refusalSpec)
	if err != nil {
		t.Fatal(err)
	}
	const heading = "### Refusal table"
	lines := strings.Split(string(data), "\n")
	start := slices.Index(lines, heading)
	if start < 0 || slices.Contains(lines[start+1:], heading) {
		t.Fatalf("%s must hold exactly one %q heading", refusalSpec, heading)
	}
	var table [][]string
	for _, line := range lines[start+1:] {
		if strings.HasPrefix(line, "|") {
			table = append(table, strings.Split(strings.TrimSuffix(strings.TrimPrefix(line, "| "), " |"), " | "))
			continue
		}
		if len(table) != 0 || strings.HasPrefix(line, "#") {
			break
		}
	}
	if len(table) < 3 || !slices.Equal(table[0], []string{"Refusal", "Path", "Reason", "Remedy"}) ||
		!slices.Equal(table[1], []string{"---", "---", "---", "---"}) {
		t.Fatalf("%q holds no refusal table", heading)
	}
	var rows []refusalRow
	for _, cells := range table[2:] {
		if len(cells) != 4 {
			t.Fatalf("%q has a malformed row %q", heading, cells)
		}
		rows = append(rows, refusalRow{name: cells[0], reason: codeSpan(t, cells[2]), remedy: codeSpan(t, cells[3])})
	}
	return rows
}

// codeSpan reads a cell that must be exactly one code span.
func codeSpan(t *testing.T, cell string) string {
	t.Helper()
	value, opened := strings.CutPrefix(cell, "`")
	value, closed := strings.CutSuffix(value, "`")
	if !opened || !closed || value == "" || strings.Contains(value, "`") {
		t.Fatalf("cell %q is not one code span", cell)
	}
	return value
}

// expand writes each placeholder's value into a row's text.
func expand(text string, values map[string]string) string {
	for placeholder, value := range values {
		text = strings.ReplaceAll(text, placeholder, value)
	}
	return text
}

func artifactMirror() api.FieldValue {
	return field("mirror", api.MapValue(field("artifacts", api.MapValue(text("url", "https://mirror.example.test/tools")))))
}

func managedArtifactMirror(endpoint string) api.FieldValue {
	return field("mirror", api.MapValue(field("artifacts", api.MapValue(text("serverRef", "mirror"), text("endpointRef", endpoint)))))
}

func mirrorServer(host string) []api.Object {
	return []api.Object{
		api.NewObject(api.ArtifactServer, "mirror", api.Value{}, api.MapValue(text("management", "managed"), text("machineRef", host),
			field("tls", api.MapValue(text("secretRef", "mirror-tls"))),
			field("listeners", api.ListValue(api.MapValue(text("name", "https"), text("protocol", "https"), field("port", api.IntegerValue("443"))))),
			field("endpoints", api.ListValue(api.MapValue(text("name", "ip"), text("listenerRef", "https"), text("addressRef", "ip")))))),
		api.NewObject(api.Secret, "mirror-ca", api.Value{}, api.MapValue(text("type", "caBundle"))),
		api.NewObject(api.Secret, "mirror-tls", api.Value{}, api.MapValue(text("type", "tlsCertificate"), field("source", api.MapValue(field("generated", api.MapValue(text("issuerRef", "mirror-ca"))))))),
	}
}

func mirrorMachine(proxy ...api.FieldValue) api.Object {
	choice := api.MapValue(field("direct", api.MapValue()))
	if len(proxy) != 0 {
		choice = api.MapValue(proxy...)
	}
	return api.NewObject(api.Machine, "mirror", api.Value{}, api.MapValue(field("proxy", choice),
		field("network", api.MapValue(field("addresses", api.ListValue(api.MapValue(text("name", "ip"), text("address", "203.0.113.20"))))))))
}

func okdCluster() api.Object {
	return api.NewObject(api.OpenShiftCluster, "hub", api.Value{}, api.MapValue(
		field("distribution", api.MapValue(text("type", "okd"), field("release", api.MapValue(text("version", "4.19.3"))))),
	))
}

func withValues(names map[string]string, pairs ...string) map[string]string {
	values := maps.Clone(names)
	for index := 0; index+1 < len(pairs); index += 2 {
		values[pairs[index]] = pairs[index+1]
	}
	return values
}

// A second name for the controller host, a Machine reached locally or over
// SSH port 22 at one of the controller's own addresses, is the controller
// host too, so a managed artifact mirror placed on it refuses as on the
// controller rather than reaching the engine's ordering refusal.
func TestAManagedArtifactMirrorOnASecondNameForTheControllerHostRefuses(t *testing.T) {
	controller := machine()
	controller = controller.WithSpec(controller.Spec().With("network", api.MapValue(field("addresses", api.ListValue(api.MapValue(text("name", "ip"), text("address", "192.0.2.1")))))))
	local := mirrorMachine()
	local = local.WithSpec(local.Spec().With("access", api.MapValue(field("local", api.BoolValue(true)))))
	ssh := mirrorMachine()
	ssh = ssh.WithSpec(ssh.Spec().With("network", api.MapValue(field("addresses", api.ListValue(api.MapValue(text("name", "ip"), text("address", "192.0.2.1")))))).
		With("access", api.MapValue(field("ssh", api.MapValue(text("addressRef", "ip"), text("knownHostsRef", "known"), field("auth", api.MapValue(text("privateKeyRef", "key"))))))))
	for name, host := range map[string]api.Object{"local": local, "ssh": ssh} {
		got := (Capability{}).Unsupported(stateOf(append([]api.Object{environment(managedArtifactMirror("ip")), controller, cluster(), host}, mirrorServer("mirror")...)...))
		if len(got) != 1 || !strings.Contains(got[0].Reason, "is placed on Machine/mirror, which is the controller host") {
			t.Errorf("%s: refusals %+v", name, got)
		}
	}
}
