package artifactserver

import (
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

const testContext = "lab"

func field(name string, value api.Value) api.FieldValue {
	return api.FieldValue{Name: name, Value: value}
}

func text(name, value string) api.FieldValue { return field(name, api.StringValue(value)) }

func number(name string, value string) api.FieldValue { return field(name, api.IntegerValue(value)) }

func controller() api.Object {
	return api.NewObject(api.Machine, "controller", api.Value{}, api.MapValue(
		field("capabilities", api.StringList("container-runtime")),
		field("os", api.MapValue(field("provided", api.BoolValue(true)))),
		field("proxy", api.MapValue(field("direct", api.MapValue()))),
		field("network", api.MapValue(field("addresses", api.ListValue(
			api.MapValue(text("name", "fqdn"), text("address", "controller.lab.example.test")),
			api.MapValue(text("name", "ip"), text("address", "192.0.2.1")),
		)))),
		field("access", api.MapValue(field("local", api.BoolValue(true)))),
	))
}

func artifactServer(fields ...api.FieldValue) api.Object {
	spec := api.MapValue(
		text("management", "managed"),
		text("machineRef", "controller"),
		text("bindAddress", "192.0.2.1"),
		text("retention", "persistent"),
		field("tls", api.MapValue(text("secretRef", "artifact-server-tls"), text("minVersion", "TLSv1.2"))),
		field("listeners", api.ListValue(
			api.MapValue(text("name", "https"), text("protocol", "https"), number("port", "8443")),
			api.MapValue(text("name", "http"), text("protocol", "http"), number("port", "8080")),
		)),
		field("endpoints", api.ListValue(
			api.MapValue(text("name", "ip-https"), text("listenerRef", "https"), text("addressRef", "ip")),
			api.MapValue(text("name", "ip-http"), text("listenerRef", "http"), text("addressRef", "ip")),
		)),
	)
	for _, extra := range fields {
		spec = spec.With(extra.Name, extra.Value)
	}
	return api.NewObject(api.ArtifactServer, "lab-artifacts", api.Value{}, spec)
}

func catalogOf(objects ...api.Object) api.Catalog { return api.NewCatalog(objects) }

func TestRequestsDeriveTheFrozenLocalPlacement(t *testing.T) {
	requests, err := Requests(catalogOf(controller(), artifactServer()), "controller", testContext)
	if err != nil || len(requests) != 1 {
		t.Fatalf("requests = %d (%v)", len(requests), err)
	}
	request := requests[0]
	if request.Placement.Connection != connectionLocal || request.Placement.Machine != "controller" {
		t.Fatalf("placement = %+v", request.Placement)
	}
	if request.Placement.Address != "" || request.Placement.User != "" || request.Placement.Port != 0 {
		t.Fatal("local placement carries SSH target fields")
	}
	if request.Image != defaultImage {
		t.Fatalf("image = %q", request.Image)
	}
	if request.ContentRoot != contentRootPrefix+"/"+testContext+"/artifact-server/lab-artifacts" {
		t.Fatalf("content root = %q", request.ContentRoot)
	}
	if strings.HasPrefix(request.ContentRoot, "/var/lib/bootwright/") {
		t.Fatal("the content root is inside the Bootwright state root")
	}
	if request.TLS == nil || request.TLS.Secret != "artifact-server-tls" || request.TLS.MinVersion != "TLSv1.2" {
		t.Fatalf("tls = %+v", request.TLS)
	}
	if len(request.Endpoints) != 2 || request.Endpoints[0].Name != "ip-http" || request.Endpoints[0].Address != "192.0.2.1" {
		t.Fatalf("endpoints = %+v", request.Endpoints)
	}
	if _, err := request.Canonical(); err != nil {
		t.Fatal("the derived request is not canonical:", err)
	}
}

func TestRequestCanonicalFormIsStableAndOrdered(t *testing.T) {
	requests, err := Requests(catalogOf(controller(), artifactServer()), "controller", testContext)
	if err != nil {
		t.Fatal(err)
	}
	data, err := requests[0].Canonical()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeRequest(data)
	if err != nil {
		t.Fatal(err)
	}
	again, err := decoded.Canonical()
	if err != nil || string(again) != string(data) {
		t.Fatal("the request does not round trip byte for byte")
	}
	for _, forbidden := range []string{"artifact-server-tls\",\"fingerprint", "BEGIN", "PRIVATE"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("the request carries %q", forbidden)
		}
	}
	if _, err := DecodeRequest(append(slices.Clone(data), '{')); err == nil {
		t.Fatal("trailing data decoded")
	}
	if _, err := DecodeRequest([]byte(strings.Replace(string(data), requestVersion, "artifact-server-v9", 1))); err == nil {
		t.Fatal("an unsupported request version decoded")
	}
}

func TestSSHPlacementRequiresKeyAndHostKey(t *testing.T) {
	host := func(ssh api.Value) api.Object {
		spec := controller().Spec().Without("access").With("access", api.MapValue(field("ssh", ssh)))
		return api.NewObject(api.Machine, "services", api.Value{}, spec)
	}
	complete := api.MapValue(
		text("addressRef", "ip"), number("port", "2222"), text("user", "root"),
		field("auth", api.MapValue(text("privateKeyRef", "services-key"))),
		text("knownHostsRef", "services-host-key"),
	)
	server := artifactServer(text("machineRef", "services"))
	requests, err := Requests(catalogOf(host(complete), server), "controller", testContext)
	if err != nil || len(requests) != 1 {
		t.Fatalf("ssh placement = %v", err)
	}
	placement := requests[0].Placement
	if placement.Connection != connectionSSH || placement.Address != "192.0.2.1" || placement.Port != 2222 || placement.User != "root" {
		t.Fatalf("placement = %+v", placement)
	}
	for name, auth := range map[string]api.Value{
		"operator identity": complete.With("auth", api.MapValue(field("operatorIdentity", api.MapValue()))),
		"password":          complete.With("auth", api.MapValue(text("passwordRef", "services-password"))),
		"no key":            complete.With("auth", api.MapValue()),
		"no host key":       complete.Without("knownHostsRef"),
		"non-root user":     complete.With("user", api.StringValue("operator")),
		"escalation secret": complete.With("sudoPasswordRef", api.StringValue("services-sudo")),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Requests(catalogOf(host(auth), server), "controller", testContext)
			reported := diagnostics.Of(err)
			if err == nil || len(reported) != 1 || reported[0].Code != "lifecycle.state" || !strings.Contains(reported[0].Message, "lifecycle placement") {
				t.Fatalf("an unsupported SSH posture was not refused as a lifecycle placement: %+v", reported)
			}
		})
	}
	bare := api.NewObject(api.Machine, "services", api.Value{}, controller().Spec().Without("access"))
	if _, err := Requests(catalogOf(bare, server), "controller", testContext); err == nil {
		t.Fatal("a host with neither controller nor SSH access produced a request")
	}
}

// A request frozen before placements stopped escalating still names its
// escalation Secret; the operation binds the placement's own list, without it.
func TestAPlacementNeverBindsAnEscalationSecret(t *testing.T) {
	request := Request{
		TLS: &TLS{Secret: "artifact-server-tls"},
		Placement: Placement{
			Address: "192.0.2.2", Connection: connectionSSH, KnownHostsRef: "services-host-key", Machine: "services",
			Port: 22, PrivateKeyRef: "services-key", SudoPasswordRef: "services-sudo", User: "root",
		},
	}
	if got := request.secretReferences(); !slices.Equal(got, []string{"artifact-server-tls", "services-host-key", "services-key"}) {
		t.Fatalf("secret references = %v", got)
	}
}

func TestImageSelectionRequiresADigestPin(t *testing.T) {
	digest := "registry.example.test/nginx@sha256:" + strings.Repeat("a", 64)
	for name, image := range map[string]api.Value{
		"local wins": api.MapValue(text("local", digest), text("public", "registry.example.test/other@sha256:"+strings.Repeat("b", 64))),
		"public":     api.MapValue(text("public", digest)),
	} {
		t.Run(name, func(t *testing.T) {
			requests, err := Requests(catalogOf(controller(), artifactServer(field("image", image))), "controller", testContext)
			if err != nil || requests[0].Image != digest {
				t.Fatalf("image = %v (%v)", requests, err)
			}
		})
	}
	for name, reference := range map[string]string{
		"tag":          "registry.example.test/nginx:1.24",
		"latest":       "registry.example.test/nginx:latest",
		"short digest": "registry.example.test/nginx@sha256:abc",
		"uppercase":    "registry.example.test/nginx@sha256:" + strings.Repeat("A", 64),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Requests(catalogOf(controller(), artifactServer(field("image", api.MapValue(text("public", reference))))), "controller", testContext)
			reported := diagnostics.Of(err)
			if err == nil || len(reported) != 1 || !strings.Contains(reported[0].Remediation, "@sha256:") {
				t.Fatalf("an unpinned image was accepted: %+v", reported)
			}
		})
	}
}

func TestEgressFollowsThePlacementMachineProxyChoice(t *testing.T) {
	external := api.NewObject(api.Proxy, "egress", api.Value{}, api.MapValue(
		text("management", "external"),
		field("connection", api.MapValue(text("httpsProxy", "http://proxy.example.test:3128"))),
	))
	host := api.NewObject(api.Machine, "controller", api.Value{}, controller().Spec().With("proxy", api.MapValue(
		text("proxyRef", "egress"), field("noProxy", api.StringList(".lab.example.test")),
	)))
	requests, err := Requests(catalogOf(host, external, artifactServer()), "controller", testContext)
	if err != nil {
		t.Fatal(err)
	}
	egress := requests[0].Egress
	if egress.HTTPSProxy != "http://proxy.example.test:3128" || !slices.Equal(egress.NoProxy, []string{".lab.example.test"}) {
		t.Fatalf("egress = %+v", egress)
	}
	managed := api.NewObject(api.Proxy, "egress", api.Value{}, api.MapValue(
		text("management", "managed"), text("machineRef", "controller"), text("implementation", "squid"),
	))
	if _, err := Requests(catalogOf(host, managed, artifactServer()), "controller", testContext); err == nil {
		t.Fatal("a managed Proxy was accepted as service egress")
	}
	authenticated := api.NewObject(api.Proxy, "egress", api.Value{}, api.MapValue(
		text("management", "external"),
		field("connection", api.MapValue(
			text("httpsProxy", "http://proxy.example.test:3128"),
			field("auth", api.MapValue(text("proxyAuthRef", "proxy-credentials"))),
		)),
	))
	if _, err := Requests(catalogOf(host, authenticated, artifactServer()), "controller", testContext); err == nil {
		t.Fatal("an authenticated proxy was accepted for image acquisition")
	}
}

// This capability reports only the artifact servers it cannot realize. Objects
// of kinds no capability claims are the engine's refusal, not this one's.
func TestUnsupportedArtifactServersAreListedInCanonicalOrder(t *testing.T) {
	cluster := api.NewObject(api.ContainerCluster, "sno", api.Value{}, api.MapValue())
	managedProxy := api.NewObject(api.Proxy, "lab-proxy", api.Value{}, api.MapValue(text("management", "managed")))
	external := api.NewObject(api.ArtifactServer, "published", api.Value{}, api.MapValue(
		text("management", "external"), text("retention", "install-only"),
	))
	installOnly := api.NewObject(api.ArtifactServer, "temp", api.Value{}, api.MapValue(
		text("management", "managed"), text("retention", "install-only"),
	))
	found := Unsupported(catalogOf(controller(), cluster, managedProxy, external, installOnly, artifactServer()))
	if !slices.Equal(found, []string{"ArtifactServer/temp"}) {
		t.Fatalf("unsupported = %v", found)
	}
	if len(Unsupported(catalogOf(controller(), artifactServer()))) != 0 {
		t.Fatal("a supported graph reported unsupported objects")
	}
}

func TestReservationKeysCoverEverySocketUnitAndPath(t *testing.T) {
	requests, err := Requests(catalogOf(controller(), artifactServer()), "controller", testContext)
	if err != nil {
		t.Fatal(err)
	}
	keys := requests[0].reservationKeys()
	for _, want := range []string{
		"socket:192.0.2.1:8080", "socket:192.0.2.1:8443",
		"unit:" + unitPrefix + "-" + testContext + "-artifacts-lab-artifacts",
		"path:" + contentRootPrefix + "/" + testContext + "/artifact-server/lab-artifacts",
	} {
		if !slices.Contains(keys, want) {
			t.Fatalf("keys = %v, missing %q", keys, want)
		}
	}
	if !slices.IsSorted(keys) {
		t.Fatalf("keys are unordered: %v", keys)
	}
	wildcard := artifactServer(text("bindAddress", "0.0.0.0"))
	requests, err = Requests(catalogOf(controller(), wildcard), "controller", testContext)
	if err != nil {
		t.Fatal(err)
	}
	keys = requests[0].reservationKeys()
	for _, want := range []string{"socket:0.0.0.0:8443", "socket:192.0.2.1:8443", "socket:192.0.2.1:8080"} {
		if !slices.Contains(keys, want) {
			t.Fatalf("wildcard keys = %v, missing %q", keys, want)
		}
	}
}

func TestProbeTargetsFollowTheEffectiveBind(t *testing.T) {
	requests, err := Requests(catalogOf(controller(), artifactServer()), "controller", testContext)
	if err != nil {
		t.Fatal(err)
	}
	targets := requests[0].ProbeTargets()
	if len(targets) != 2 || targets[0].Name != "http" || targets[0].Address != "192.0.2.1" || targets[1].Protocol != "https" {
		t.Fatalf("targets = %+v", targets)
	}
	requests, err = Requests(catalogOf(controller(), artifactServer(text("bindAddress", "0.0.0.0"))), "controller", testContext)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range requests[0].ProbeTargets() {
		if target.Address == "0.0.0.0" {
			t.Fatal("a wildcard bind is probed on the wildcard address instead of its endpoints")
		}
	}
}

func TestRequestsRefuseAnInvalidContextName(t *testing.T) {
	for _, name := range []string{"", "Lab", "-lab", "lab-", "lab/other", strings.Repeat("a", 64)} {
		if _, err := Requests(catalogOf(controller(), artifactServer()), "controller", name); err == nil {
			t.Fatalf("context name %q produced requests", name)
		}
	}
}
