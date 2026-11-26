package ceph

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/storage/cephcluster"
)

const signOnIssuer = "https://idp.example.test/realms/ceph"

func signOnProvider(bundle bool) api.Object {
	spec := requestMap("management", "external", "issuerURL", signOnIssuer)
	if bundle {
		spec = spec.With("trustBundleRef", api.StringValue("sso-ca"))
	}
	return api.NewObject(api.OIDCProvider, "corp-sso", api.Value{}, spec)
}

func signOnObject(pairs ...any) api.Object {
	secrets := api.ListValue(requestMap("path", "/spec/client_secret", "secretRef", "sso-client"), requestMap("path", "/spec/cookie_secret", "secretRef", "sso-cookie"),
		requestMap("path", "/spec/ssl_cert", "secretRef", "sso-tls"), requestMap("path", "/spec/ssl_key", "secretRef", "sso-tls"))
	if len(pairs) == 2 && pairs[0] == "secrets" {
		secrets, pairs = pairs[1].(api.Value), nil
	}
	native := requestMap("service_type", "oauth2-proxy", "placement", requestMap("label", "mon", "count", 1),
		"spec", requestMap("client_id", "ceph", "provider_display_name", "SSO"))
	spec := requestMap(append([]any{"clusterRef", "example", "oidcProviderRef", "corp-sso", "secrets", secrets, "native", native}, pairs...)...)
	return api.NewObject(api.CephSignOn, "ceph-sso", api.Value{}, spec)
}

func signOnServices() []api.Value {
	return []api.Value{mgmtGateway(requestMap("port", 9443, "enable_auth", true, "virtual_ip", "192.0.2.60")), mgmtIngress("192.0.2.60")}
}

func signOnCatalog(t *testing.T, bundle bool, objects ...api.Object) api.Catalog {
	t.Helper()
	if len(objects) == 0 {
		objects = []api.Object{signOnObject()}
	}
	return withAuthoredImages(delegatedCatalog(signOnServices(), nil, append([]api.Object{signOnProvider(bundle)}, objects...)...), "keepalived", "nginx", "oauth2_proxy")
}

func signOnInput(catalog api.Catalog) lifecycle.PlanInput {
	return lifecycle.PlanInput{State: compilation.NewState(catalog, catalog, nil), Verb: reconciliation.Apply, Controller: "controller", Context: lifecycle.ContextIdentity{Name: "synthetic"}}
}

func signOnDefinition(t *testing.T, catalog api.Catalog) reconciliation.BlockDefinition {
	t.Helper()
	plan, err := NewSignOn(nil).Plan(context.Background(), signOnInput(catalog))
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if len(plan.Definitions) != 1 {
		t.Fatalf("the sign-on plans %d blocks", len(plan.Definitions))
	}
	return plan.Definitions[0]
}

func TestSignOnPlansItsOwnBlockAfterTheGateway(t *testing.T) {
	definition := signOnDefinition(t, signOnCatalog(t, true))
	wantDependencies := []string{ClusterBlockID("example"), ImageBlockID("example"), ServiceUnitBlockID("example", ServiceUnit{Type: "mgmt-gateway"}), SettingsBlockID("example")}
	slices.Sort(wantDependencies)
	if definition.ID != "ceph-signon-example" || definition.Kind != string(api.CephSignOn) || definition.Object != "ceph-sso" || definition.Implementation != SignOnImplementation ||
		definition.ContentDigest != SignOnContentDigest() || definition.Stage != reconciliation.StageClusters || !slices.Equal(definition.Dependencies, wantDependencies) || len(definition.Exclusive) != 0 {
		t.Fatalf("the sign-on block is %+v", definition)
	}
	want := []string{"realize Ceph sign-on ceph-sso of example",
		"the management gateway signs users in through oauth2-proxy and OIDCProvider corp-sso", signOnEnableImpact, signOnDelegateImpact, signOnLoginsImpact, signOnAudienceImpact, signOnPortImpact}
	if !slices.Equal(definition.Impacts, want) || slices.ContainsFunc(definition.Impacts, func(impact string) bool { return len(impact) > 200 }) {
		t.Fatalf("the sign-on impacts are %q", definition.Impacts)
	}
	r, err := DecodeSignOnRequest(definition.Request)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	matchesGolden(t, "signon-request", definition.Request, false)
	if r.Provider != (SignOnProvider{IssuerURL: signOnIssuer, Name: "corp-sso", TrustBundle: true}) || !strings.Contains(string(r.Document), `"oidc_issuer_url":"`+signOnIssuer+`"`) ||
		!strings.Contains(string(r.Document), `"custom_configs":[{"mount_path":"`+cephcluster.OAuth2ProxyCABundlePath+`"}]`) {
		t.Fatalf("the sign-on freezes %+v %s", r.Provider, r.Document)
	}
	if paths := bindingPaths(r.Bindings); !slices.Equal(paths, []string{"/custom_configs/0/content", "/spec/client_secret", "/spec/cookie_secret", "/spec/ssl_cert", "/spec/ssl_key"}) ||
		r.Bindings[0].SecretRef != "sso-ca" {
		t.Fatalf("the sign-on binds %+v", r.Bindings)
	}
	if r.Gateway.Port != 9443 || r.Gateway.VirtualIP != "192.0.2.60" || r.Gateway.Count != 2 || len(r.Gateway.Hosts) == 0 {
		t.Fatalf("the sign-on freezes the gateway %+v", r.Gateway)
	}
	plain, err := DecodeSignOnRequest(signOnDefinition(t, signOnCatalog(t, false, signOnObject("secrets", api.ListValue(
		requestMap("path", "/spec/client_secret", "secretRef", "sso-client"), requestMap("path", "/spec/cookie_secret", "secretRef", "sso-cookie"))))).Request)
	if err != nil || plain.Provider.TrustBundle || strings.Contains(string(plain.Document), "custom_configs") || len(plain.Bindings) != 2 {
		t.Fatalf("a sign-on without a trust bundle freezes %+v (%v)", plain, err)
	}
}

func bindingPaths(bindings []ServiceBinding) []string {
	paths := []string{}
	for _, b := range bindings {
		paths = append(paths, b.Path)
	}
	return paths
}

func TestSignOnImpactsNameTheProxyCertificateAndTrust(t *testing.T) {
	definition := signOnDefinition(t, signOnCatalog(t, false, signOnObject("secrets", api.ListValue(
		requestMap("path", "/spec/client_secret", "secretRef", "sso-client"), requestMap("path", "/spec/cookie_secret", "secretRef", "sso-cookie")))))
	tail := definition.Impacts[len(definition.Impacts)-2:]
	if !slices.Equal(tail, []string{"cephadm signs oauth2-proxy's TLS certificate", "oauth2-proxy verifies the issuer of OIDCProvider corp-sso with its image's system trust store"}) {
		t.Fatalf("the sign-on impacts end %q", tail)
	}
}

func TestTheClusterDelegatesTheSignOnsProxyAndTheGatewayCarriesNone(t *testing.T) {
	catalog := signOnCatalog(t, true)
	core, err := planned(t, catalog, api.CephCluster, "example")
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if at := slices.IndexFunc(core.Delegated, func(d DelegatedService) bool { return d.Name == "oauth2-proxy" }); at < 0 || len(core.Delegated[at].Hosts) == 0 {
		t.Fatalf("the core request delegates %+v", core.Delegated)
	}
	unit := gatewayUnitOf(t, catalog)
	for _, document := range documentsOf(unit) {
		if strings.Contains(document, "oauth2-proxy") || strings.Contains(document, "custom_configs") {
			t.Fatalf("the gateway unit carries %s", document)
		}
	}
	if impacts := gatewayImpacts(api.Object{}, unit); !slices.Equal(impacts, []string{gatewayUnbound}) {
		t.Fatalf("the gateway impacts are %q", impacts)
	}
	r := frozenGoldenRequest()
	r.Action, r.Service = "service", &unit
	if got, err := cephDeadline(r, "apply"); err != nil || got != 3600*time.Second {
		t.Fatalf("the gateway apply deadline is %v (%v)", got, err)
	}
}

func TestAFrozenGatewayWithAnOAuth2ProxyRefusesAsAnotherBuild(t *testing.T) {
	r := mgmtGatewayRequest(t)
	unit := *r.Service
	unit.Documents = append(slices.Clone(unit.Documents), json.RawMessage(`{"placement":{"hosts":["node-1"]},"service_type":"oauth2-proxy"}`))
	r.Service = &unit
	for _, operation := range []string{"apply", "observe", "destroy"} {
		_, err := cephDeadline(r, operation)
		refusedStop(t, err, cephOtherBuild, "the management gateway of CephCluster example froze an oauth2-proxy, which this build realizes as a CephSignOn",
			"destroy it with the build that applied it, then apply with this build")
	}
}

func TestSignOnWithoutTheOAuth2ProxyImageRefusesNamingTheField(t *testing.T) {
	catalog := withAuthoredImages(delegatedCatalog(signOnServices(), nil, signOnProvider(true), signOnObject()), "keepalived", "nginx")
	state := compilation.NewState(catalog, catalog, nil)
	signOnRefused(t, state, "CephSignOn ceph-sso needs cephadm's container_image_oauth2_proxy, and spec.images.oauth2_proxy of CephCluster example is not set; Bootwright keeps no default",
		"set spec.images.oauth2_proxy of CephCluster example to an image beneath its Entitlement registry; on an installed cluster that is an input change: take back what this context owns with bootwright destroy --context <context>, then apply")
}

func TestTheOAuth2ProxyImageIsAdmittedWithAGatewayAndNoSignOn(t *testing.T) {
	catalog := withAuthoredImages(delegatedCatalog(signOnServices(), nil), "keepalived", "nginx", "oauth2_proxy")
	for _, refusal := range New(api.CephCluster, nil).Unsupported(compilation.NewState(catalog, catalog, nil)) {
		if strings.Contains(refusal.Reason, "oauth2_proxy") {
			t.Fatalf("a gateway with the oauth2-proxy image and no sign-on refuses %+v", refusal)
		}
	}
	bare := withAuthoredImages(delegatedCatalog(nil, nil), "oauth2_proxy")
	signOnRefused(t, compilation.NewState(bare, bare, nil), "spec.images.oauth2_proxy is set, but no declared service or CephSMBResource of CephCluster example needs cephadm's container_image_oauth2_proxy",
		"remove spec.images.oauth2_proxy from CephCluster example")
}

func signOnRefused(t *testing.T, state *compilation.State, reason, remedy string) {
	t.Helper()
	for _, refusal := range New(api.CephCluster, nil).Unsupported(state) {
		if refusal.Reason == reason && refusal.Remediation == remedy {
			return
		}
	}
	t.Fatalf("CephCluster does not refuse %q: %+v", reason, New(api.CephCluster, nil).Unsupported(state))
}

func TestSignOnDeadline(t *testing.T) {
	for operation, want := range map[string]time.Duration{"apply": 5580 * time.Second, "observe": 1380 * time.Second} {
		if got, err := signOnDeadline(operation); err != nil || got != want {
			t.Fatalf("the sign-on %s deadline is %v (%v), want %v", operation, got, err, want)
		}
	}
	if _, err := signOnDeadline("destroy"); err == nil {
		t.Fatal("the sign-on runs a destroy operation")
	}
}

const signOnPin = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

func signOnCore() Request {
	r := coreRequest()
	r.Auxiliary = authoredReferences(signOnAuxiliary())
	r.Delegated = []DelegatedService{{Name: "oauth2-proxy", Hosts: []string{"node-1"}}}
	return r
}

func signOnAuxiliary() []AuxiliaryImage {
	images := []AuxiliaryImage{}
	for _, name := range []string{"keepalived", "nginx", "oauth2_proxy"} {
		base := PinnedRegistry + "/synthetic/" + name
		images = append(images, AuxiliaryImage{Name: name, Base: base, Tag: "synthetic-1", Image: base + ":synthetic-1"})
	}
	return images
}

func signOnGatewayProof(t *testing.T, owner string, reachable bool) lifecycle.BlockEvidence {
	t.Helper()
	r := mgmtGatewayRequest(t)
	data, err := r.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	block := reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: ServiceUnitBlockID("example", ServiceUnit{Type: "mgmt-gateway"}), Kind: string(api.CephCluster), Object: "example", Implementation: Implementation, Request: data}, RequestDigest: "digest"}
	runtime := r
	runtime.Owner, runtime.FSID = owner, FSIDFor(owner, r.Cluster)
	e := readyEvidence(runtime)
	e.Endpoint = EndpointEvidence{CertificateSHA256: signOnPin, Reachable: reachable}
	return lifecycle.BlockEvidence{Operation: owner, Block: block, Kind: block.Kind, Object: block.Object, Implementation: Implementation, Verb: reconciliation.Apply, State: reconciliation.BlockDone, Evidence: evidenceBytes(t, e)}
}

func signOnRun(t *testing.T, applying string) (SignOnRequest, lifecycle.Execution) {
	t.Helper()
	catalog := signOnCatalog(t, true)
	cluster, _ := catalog.Find(api.CephCluster, "example")
	base, err := baseRequest(catalog, cluster, "synthetic")
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	core := signOnCore()
	base.Auxiliary, base.Hosts = core.Auxiliary, core.Hosts
	object, _ := catalog.Find(api.CephSignOn, "ceph-sso")
	r, err := signOnRequest(catalog, object, base, "controller")
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	canonical, err := r.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	definition := reconciliation.BlockDefinition{ID: SignOnBlockID("example"), Kind: string(api.CephSignOn), Object: "ceph-sso", Implementation: SignOnImplementation, ContentDigest: SignOnContentDigest(), Request: canonical}
	digest, err := reconciliation.RequestDigest(definition)
	if err != nil {
		t.Fatal(err)
	}
	document, _, _ := r.document()
	core.Delegated = []DelegatedService{{Name: "oauth2-proxy", Hosts: document.Placement.Hosts}}
	clusterProof := coreClusterProof(t, core, nil)
	clusterProof.Operation = "apply-original"
	proofs := []lifecycle.BlockEvidence{clusterProof, auxiliaryProof(t, core.Image, signOnAuxiliary(), sambaDigests()...), signOnGatewayProof(t, "apply-original", true)}
	e := lifecycle.Execution{ApplyOperation: applying, Operation: applying, Context: "example", Block: reconciliation.Block{BlockDefinition: definition, RequestDigest: digest}, ApplyProved: proofs}
	e = withMaterial(e, core)
	for _, b := range r.Bindings {
		e.Material[b.SecretRef] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("value"), secrets.CertificatePart: []byte(gatewayCertificatePEM), secrets.PrivateKeyPart: []byte(gatewayKeyPEM)})
	}
	return r, e
}

func signOnReady(t *testing.T, e lifecycle.Execution, unit Request, change func(*Evidence)) json.RawMessage {
	t.Helper()
	evidence := readyEvidence(unit)
	evidence.Request = e.Block.RequestDigest
	evidence.Daemons = []UnitDaemon{{Host: "node-1", Name: "oauth2-proxy.node-1", Service: "oauth2-proxy"}}
	evidence.Endpoint = EndpointEvidence{CertificateSHA256: signOnPin, Reachable: true}
	evidence.SSO = "oauth2"
	if change != nil {
		change(&evidence)
	}
	return evidenceBytes(t, evidence)
}

func TestSignOnAccessFreezesOnlyVerifiedProxyArguments(t *testing.T) {
	access := requestMap("allowedGroups", api.StringList("Ceph Operators", "/storage/ceph"))
	definition := signOnDefinition(t, signOnCatalog(t, true, signOnObject("access", access)))
	r, err := DecodeSignOnRequest(definition.Request)
	if err != nil {
		t.Fatal(err)
	}
	document, _, ok := r.document()
	want := []string{"--skip-jwt-bearer-tokens=false", "--oidc-groups-claim=groups", "--allowed-group=/storage/ceph", "--allowed-group=Ceph Operators"}
	if !ok || len(document.Arguments) != len(want) || !slices.Contains(definition.Impacts, signOnRestrictedImpact) ||
		slices.Contains(definition.Impacts, signOnAudienceImpact) {
		t.Fatalf("unexpected frozen policy and impacts: %s, %v", r.Document, definition.Impacts)
	}
	for i, arg := range document.Arguments {
		if arg.Argument != want[i] || arg.Split == nil || *arg.Split {
			t.Fatalf("argument %d is %+v", i, arg)
		}
	}
	badArgs := [][]signOnArgument{document.Arguments[:2], append(slices.Clone(document.Arguments), document.Arguments[2]),
		{{Argument: "--skip-auth-route=^/"}}, {{Argument: want[0]}, document.Arguments[1], document.Arguments[2]}}
	for _, args := range badArgs {
		bad := r
		document.Arguments = args
		bad.Document, err = json.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		data, err := bad.Canonical()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeSignOnRequest(data); err == nil {
			t.Fatal("inconsistent access policy decoded")
		}
	}
}

func TestSignOnTakesItsOwnerFromTheClusterProofNotTheApplyingOperation(t *testing.T) {
	_, e := signOnRun(t, "apply-additive")
	_, unit, err := NewSignOn(nil).prepare(context.Background(), e)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if unit.Owner != "apply-original" || unit.FSID != FSIDFor("apply-original", "example") || unit.Action != "service" || unit.Service.Type != SignOnUnitType ||
		unit.Service.SignOn == nil || unit.Service.SignOn.CertificateSHA256 != signOnPin || unit.Service.SignOn.Count != 2 {
		t.Fatalf("the sign-on unit is %+v %+v", unit.Owner, unit.Service)
	}
	if !slices.ContainsFunc(unit.Auxiliary, func(a AuxiliaryReference) bool {
		return a.Name == "oauth2_proxy" && strings.Contains(a.Image, "@sha256:")
	}) {
		t.Fatalf("the sign-on unit runs %+v", unit.Auxiliary)
	}
	access := requestMap("groupsClaim", "ceph_roles", "allowedGroups", api.StringList("ceph-operator"))
	r, err := DecodeSignOnRequest(signOnDefinition(t, signOnCatalog(t, true, signOnObject("access", access))).Request)
	if err != nil {
		t.Fatal(err)
	}
	unit.Service.Documents = []json.RawMessage{r.Document}
	if len(unitDocuments(unit)) != 1 {
		t.Fatal("the worker request does not decode the restricted proxy document")
	}
	canonical, err := unit.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "request-signon-unit", canonical, false)
}

func TestSignOnRefusesWithoutTheGatewaysCertificateProof(t *testing.T) {
	_, e := signOnRun(t, "apply-original")
	e.ApplyProved = e.ApplyProved[:2]
	_, _, err := NewSignOn(nil).prepare(context.Background(), e)
	refusedStop(t, err, cephRecordsMissing, "CephCluster example lacks the apply proof of its management gateway", "apply the management gateway first; a sign-on proves it enforces sign-on only against the certificate that proof reports")
	_, e = signOnRun(t, "apply-original")
	e.ApplyProved[2] = signOnGatewayProof(t, "apply-original", false)
	_, _, err = NewSignOn(nil).prepare(context.Background(), e)
	refusedStop(t, err, cephProofMismatch, "the apply proof of CephCluster example's management gateway does not prove the certificate it serves", "restore the original apply evidence")
}

func TestSignOnApplyLendsItsLeavesAndProvesSignOn(t *testing.T) {
	_, e := signOnRun(t, "apply-original")
	_, unit, err := NewSignOn(nil).prepare(context.Background(), e)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	runner := &recordingRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: signOnReady(t, e, unit, nil)}}
	got, err := NewSignOn(runner).Apply(context.Background(), e)
	if err != nil || got.Outcome != reconciliation.OutcomeChanged {
		t.Fatalf("a ready sign-on applies as %+v (%+v)", got, diagnostics.Of(err))
	}
	if runner.request.Implementation != SignOnImplementation || runner.request.Variable != "bootwright_ceph" || runner.request.Deadline != 5580*time.Second {
		t.Fatalf("the sign-on runs %s %s %v", runner.request.Implementation, runner.request.Variable, runner.request.Deadline)
	}
	variables := []string{}
	for _, file := range runner.request.Materials {
		variables = append(variables, file.Variable)
	}
	if !slices.Equal(variables, []string{"sshKey", "knownHosts", "oauth2ProxyCABundle", "oauth2ProxyClientSecret", "oauth2ProxyCookieSecret", "oauth2ProxyCertificate", "oauth2ProxyKey"}) {
		t.Fatalf("the sign-on apply lends %q", variables)
	}
	for name, change := range map[string]func(*Evidence){
		"single sign-on off":  func(e *Evidence) { e.SSO = "" },
		"not enforced":        func(e *Evidence) { e.Endpoint = EndpointEvidence{} },
		"another owner":       func(e *Evidence) { e.Owner = "apply-other" },
		"another certificate": func(e *Evidence) { e.Endpoint.CertificateSHA256 = strings.Repeat("d", 64) },
	} {
		runner := &recordingRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: signOnReady(t, e, unit, change)}}
		if _, err := NewSignOn(runner).Apply(context.Background(), e); err == nil {
			t.Fatalf("a sign-on with %s applies", name)
		}
	}
	observer := &recordingRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: signOnReady(t, e, unit, nil)}}
	observation, err := NewSignOn(observer).Observe(context.Background(), e)
	if err != nil || observation.Effect != reconciliation.EffectCompleted || len(observer.request.Materials) != 2 || observer.request.Deadline != 1380*time.Second {
		t.Fatalf("the sign-on observes %+v (%+v), lending %+v", observation, diagnostics.Of(err), observer.request.Materials)
	}
}

func TestSignOnEvidenceBelongsOnlyToTheSignOnUnit(t *testing.T) {
	_, e := signOnRun(t, "apply-original")
	_, unit, err := NewSignOn(nil).prepare(context.Background(), e)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	gateway := runtimeRequest()
	plain := gatewayUnitOf(t, delegatedCatalog(vipGateway("192.0.2.60"), nil))
	gateway.Action, gateway.Service = "service", &plain
	e2 := readyEvidence(gateway)
	e2.Daemons = []UnitDaemon{{Host: "node-1", Name: "mgmt-gateway.node-1", Service: "mgmt-gateway"}, {Host: "node-1", Name: "keepalived.mgmt.node-1.abcdef", Service: "ingress.mgmt"}}
	e2.Endpoint = EndpointEvidence{CertificateSHA256: signOnPin, Reachable: true}
	e2.SSO = "oauth2"
	if effect, err := EvaluateEvidence(evidenceBytes(t, e2), gateway, "digest", false); err == nil || effect != reconciliation.EffectUnknown {
		t.Fatalf("a gateway unit reporting single sign-on is %s", effect)
	}
	if effect, err := EvaluateEvidence(signOnReady(t, e, unit, nil), unit, e.Block.RequestDigest, false); err != nil || effect != reconciliation.EffectCompleted {
		t.Fatalf("a ready sign-on is %s (%+v)", effect, diagnostics.Of(err))
	}
}

func TestSignOnRetiresWithItsHostsAndRefusesOnlineRemoval(t *testing.T) {
	_, e := signOnRun(t, "apply-additive")
	if _, err := NewSignOn(nil).Destroy(context.Background(), e); err == nil {
		t.Fatal("the sign-on removes itself while its hosts survive")
	}
	e.RetiredWithHosts = true
	got, err := NewSignOn(nil).Destroy(context.Background(), e)
	if err != nil || got.Outcome != reconciliation.OutcomeChanged || !strings.Contains(string(got.Evidence), FSIDFor("apply-original", "example")) {
		t.Fatalf("the retired sign-on is %+v (%+v)", got, diagnostics.Of(err))
	}
	machines, err := NewSignOn(nil).HostMachines(e.Block)
	if err != nil || len(machines) == 0 {
		t.Fatalf("the sign-on retires with %v (%v)", machines, err)
	}
	if !NewSignOn(nil).RequiresOnlineRemoval(e.Block) {
		t.Fatal("the sign-on needs no online removal")
	}
}

func TestSignOnRequestDecodeRefusesInconsistentRequests(t *testing.T) {
	r, _ := signOnRun(t, "apply-original")
	cases := map[string]func(*SignOnRequest){
		"another issuer": func(r *SignOnRequest) { r.Provider.IssuerURL = "https://other.example.test" },
		"no cookie binding": func(r *SignOnRequest) {
			r.Bindings = slices.DeleteFunc(slices.Clone(r.Bindings), func(b ServiceBinding) bool { return b.Path == "/spec/cookie_secret" })
		},
		"no CA binding": func(r *SignOnRequest) { r.Bindings = r.Bindings[1:] },
		"a gateway binding": func(r *SignOnRequest) {
			r.Bindings = append(slices.Clone(r.Bindings), ServiceBinding{Path: "/spec/zzz", SecretRef: "x", ServiceRef: "oauth2-proxy"})
		},
		"no gateway hosts":       func(r *SignOnRequest) { r.Gateway.Hosts = nil },
		"a count past the hosts": func(r *SignOnRequest) { r.Gateway.Count = int64(len(r.Gateway.Hosts) + 1) },
	}
	for name, change := range cases {
		bad := r
		bad.Gateway.Hosts = slices.Clone(r.Gateway.Hosts)
		change(&bad)
		data, err := json.Marshal(bad)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeSignOnRequest(data); err == nil {
			t.Fatalf("a request with %s decodes", name)
		}
	}
}
