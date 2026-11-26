package ceph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/storage/cephcluster"
)

const (
	SignOnImplementation = "storage-ceph-signon-v1"
	SignOnVersion        = "ceph-signon-v1"
	SignOnUnitType       = api.SignOnServiceType
	signOnImage          = "oauth2_proxy"
	signOnCABundle       = "/custom_configs/0/content"
	SignOnAccessRule     = "oidc-claim-allowlist-verified-argv"
	signOnClaimArg       = "--oidc-groups-claim="
	signOnGroupArg       = "--allowed-group="
	signOnNoBearer       = "--skip-jwt-bearer-tokens=false"
)

func SignOnContentDigest() string {
	d := sha256.Sum256([]byte(SignOnImplementation + "\x00" + SignOnVersion + "\x00" + EvidenceVersion + "\x00" + SignOnSuspensionVersion + "\x00" + PhysicalRetirementImplementation + "\x00" + SignOnAccessRule))
	return hex.EncodeToString(d[:])
}

func SignOnBlockID(cluster string) string {
	return reconciliation.BlockIdentity("ceph-signon-", cluster)
}

type SignOnProvider struct {
	IssuerURL   string `json:"issuerURL"`
	Name        string `json:"name"`
	TrustBundle bool   `json:"trustBundle"`
}

type SignOnGateway struct {
	Count     int64    `json:"count"`
	Endpoint  string   `json:"endpoint,omitempty"`
	Grafana   bool     `json:"grafana,omitempty"`
	Hosts     []string `json:"hosts"`
	Port      int64    `json:"port"`
	VirtualIP string   `json:"virtualIP,omitempty"`
}

type SignOnTarget struct {
	CertificateSHA256 string   `json:"certificateSHA256"`
	Count             int64    `json:"count"`
	Endpoint          string   `json:"endpoint,omitempty"`
	Grafana           bool     `json:"grafana,omitempty"`
	Hosts             []string `json:"hosts"`
	Port              int64    `json:"port"`
	VirtualIP         string   `json:"virtualIP,omitempty"`
}

type SignOnRequest struct {
	Bindings       []ServiceBinding `json:"bindings"`
	Cluster        string           `json:"cluster"`
	Controller     string           `json:"controller"`
	Document       json.RawMessage  `json:"document"`
	Gateway        SignOnGateway    `json:"gateway"`
	HostSecretRefs []string         `json:"hostSecretRefs"`
	Image          string           `json:"image"`
	Machines       []string         `json:"machines"`
	Object         string           `json:"object"`
	Provider       SignOnProvider   `json:"provider"`
	Version        string           `json:"version"`
}

type signOnArgument struct {
	Argument string `json:"argument"`
	Split    *bool  `json:"split"`
}

func signOnArguments(access api.Value) []signOnArgument {
	groups := access.Get("allowedGroups").Strings()
	if len(groups) == 0 {
		return nil
	}
	slices.Sort(groups)
	claim := access.Get("groupsClaim").Text()
	if claim == "" {
		claim = api.DefaultSignOnGroupsClaim
	}
	split := false
	args := []signOnArgument{{Argument: signOnNoBearer, Split: &split}, {Argument: signOnClaimArg + claim, Split: &split}}
	for _, group := range groups {
		args = append(args, signOnArgument{Argument: signOnGroupArg + group, Split: &split})
	}
	return args
}

func validSignOnArguments(args []signOnArgument) bool {
	if args == nil {
		return true
	}
	if len(args) < 3 || len(args) > 2+cephcluster.SignOnMaxGroups || args[0].Argument != signOnNoBearer ||
		slices.ContainsFunc(args, func(arg signOnArgument) bool { return arg.Split == nil || *arg.Split }) {
		return false
	}
	claim, ok := strings.CutPrefix(args[1].Argument, signOnClaimArg)
	if !ok || !cephcluster.ValidSignOnClaim(claim) {
		return false
	}
	last := ""
	for _, arg := range args[2:] {
		group, ok := strings.CutPrefix(arg.Argument, signOnGroupArg)
		if !ok || !cephcluster.ValidSignOnGroup(group) || group <= last {
			return false
		}
		last = group
	}
	return true
}

type oauth2ProxySpec struct {
	AllowlistDomains    []string `json:"allowlist_domains,omitempty"`
	ClientID            string   `json:"client_id"`
	HTTPSAddress        string   `json:"https_address,omitempty"`
	OIDCIssuerURL       string   `json:"oidc_issuer_url"`
	ProviderDisplayName string   `json:"provider_display_name"`
	RedirectURL         string   `json:"redirect_url,omitempty"`
}

var signOnLeaves = map[string]monitoringLeaf{
	SignOnUnitType + " " + cephcluster.SignOnClientSecret: {Part: secrets.ValuePart, Variable: "oauth2ProxyClientSecret", File: "oauth2-proxy-client-secret"},
	SignOnUnitType + " " + cephcluster.SignOnCookieSecret: {Part: secrets.ValuePart, Variable: "oauth2ProxyCookieSecret", File: "oauth2-proxy-cookie-secret"},
	SignOnUnitType + " " + cephcluster.SignOnCertificate:  {Part: secrets.CertificatePart, Variable: "oauth2ProxyCertificate", File: "oauth2-proxy-certificate"},
	SignOnUnitType + " " + cephcluster.SignOnKey:          {Part: secrets.PrivateKeyPart, Variable: "oauth2ProxyKey", File: "oauth2-proxy-key"},
	SignOnUnitType + " " + signOnCABundle:                 {Part: secrets.CertificatePart, Variable: "oauth2ProxyCABundle", File: "oauth2-proxy-ca-bundle"},
}

func (r SignOnRequest) Canonical() ([]byte, error) { return reconciliation.Freeze(r, "Ceph sign-on") }

func (r SignOnRequest) SecretReferences() []string {
	references := slices.Clone(r.HostSecretRefs)
	for _, binding := range r.Bindings {
		references = append(references, binding.SecretRef)
	}
	return lifecycle.SecretNames(references...)
}

func DecodeSignOnRequest(data []byte) (SignOnRequest, error) {
	r, err := reconciliation.ThawVersion[SignOnRequest](data, "Ceph sign-on", SignOnVersion)
	if err != nil {
		return SignOnRequest{}, err
	}
	if !r.consistent() {
		return SignOnRequest{}, cephRequestInvalid.Fail("the frozen Ceph sign-on request is inconsistent", "")
	}
	return r, nil
}

func (r SignOnRequest) document() (unitDocument, oauth2ProxySpec, bool) {
	var spec oauth2ProxySpec
	document, err := reconciliation.DecodeEvidence[unitDocument](r.Document, 60*1024, "Ceph sign-on document")
	if err != nil {
		return unitDocument{}, spec, false
	}
	raw, err := json.Marshal(document.Spec)
	if err != nil {
		return unitDocument{}, spec, false
	}
	if spec, err = reconciliation.Thaw[oauth2ProxySpec](raw, "Ceph sign-on spec"); err != nil {
		return unitDocument{}, spec, false
	}
	return document, spec, true
}

func (r SignOnRequest) consistent() bool {
	if r.Cluster == "" || r.Controller == "" || r.Image == "" || !reconciliation.ValidSegment(r.Object) || !strictlySorted(r.Machines) || !strictlySorted(r.HostSecretRefs) ||
		r.Provider.Name == "" || !strings.HasPrefix(r.Provider.IssuerURL, "https://") || !strictlySorted(r.Gateway.Hosts) || r.Gateway.Port < 1 || r.Gateway.Port > 65535 || r.Gateway.Count < 1 || r.Gateway.Count > int64(len(r.Gateway.Hosts)) {
		return false
	}
	document, spec, ok := r.document()
	if !ok || !validSignOnArguments(document.Arguments) || document.ServiceType != SignOnUnitType || document.ServiceID != "" || !strictlySorted(document.Placement.Hosts) || document.Placement.CountPerHost != nil ||
		spec.OIDCIssuerURL != r.Provider.IssuerURL || spec.ClientID == "" || spec.ProviderDisplayName == "" {
		return false
	}
	mounted := len(document.CustomConfigs) == 1 && document.CustomConfigs[0].MountPath == cephcluster.OAuth2ProxyCABundlePath
	if mounted != r.Provider.TrustBundle || !mounted && len(document.CustomConfigs) != 0 {
		return false
	}
	return r.bindingsConsistent()
}

func (r SignOnRequest) bindingsConsistent() bool {
	paths := []string{}
	for i, binding := range r.Bindings {
		if binding.ServiceRef != SignOnUnitType || binding.SecretRef == "" || i > 0 && r.Bindings[i-1].Path >= binding.Path {
			return false
		}
		if _, known := signOnLeaves[SignOnUnitType+" "+binding.Path]; !known {
			return false
		}
		paths = append(paths, binding.Path)
	}
	certificate, key := slices.Index(paths, cephcluster.SignOnCertificate), slices.Index(paths, cephcluster.SignOnKey)
	return slices.Contains(paths, cephcluster.SignOnClientSecret) && slices.Contains(paths, cephcluster.SignOnCookieSecret) &&
		(certificate < 0) == (key < 0) && (certificate < 0 || r.Bindings[certificate].SecretRef == r.Bindings[key].SecretRef) &&
		slices.Contains(paths, signOnCABundle) == r.Provider.TrustBundle
}

func signOnDocument(cluster, object api.Object, issuer string, trustBundle bool) (json.RawMessage, []string, error) {
	native := object.Spec().Get("native")
	hosts, err := serviceHosts(cluster, native)
	if err != nil {
		return nil, nil, err
	}
	slices.Sort(hosts)
	spec := native.Get("spec")
	document := unitDocument{Arguments: signOnArguments(object.Spec().Get("access")), Networks: native.Get("networks").Strings(), Placement: nativePlacement{Count: integerOf(native.Get("placement", "count")), Hosts: hosts}, ServiceType: SignOnUnitType,
		Spec: oauth2ProxySpec{AllowlistDomains: spec.Get("allowlist_domains").Strings(), ClientID: spec.Get("client_id").Text(), HTTPSAddress: spec.Get("https_address").Text(),
			OIDCIssuerURL: issuer, ProviderDisplayName: spec.Get("provider_display_name").Text(), RedirectURL: spec.Get("redirect_url").Text()}}
	if trustBundle {
		document.CustomConfigs = []customConfig{{MountPath: cephcluster.OAuth2ProxyCABundlePath}}
	}
	raw, err := json.Marshal(document)
	return raw, hosts, err
}

func signOnGateway(cluster api.Object) (SignOnGateway, error) {
	for _, svc := range cluster.Spec().Get("services").Items() {
		if svc.Get("service_type").Text() != "mgmt-gateway" {
			continue
		}
		hosts, err := serviceHosts(cluster, svc)
		if err != nil {
			return SignOnGateway{}, err
		}
		slices.Sort(hosts)
		port, _ := svc.Get("spec", "port").Int64()
		count, counted := svc.Get("placement", "count").Int64()
		if !counted {
			count = int64(len(hosts))
		}
		gateway := SignOnGateway{Count: count, Endpoint: cluster.Spec().Get("managementGateway", "fqdn").Text(), Grafana: slices.Contains(gatewayMonitoring(cluster), "grafana"), Hosts: hosts, Port: port}
		if vip := svc.Get("spec", "virtual_ip").Text(); vip != "" {
			gateway.VirtualIP = canonicalVIP(vip)
		}
		return gateway, nil
	}
	return SignOnGateway{}, cephRequestInvalid.Fail("CephCluster "+cluster.Name()+" declares no mgmt-gateway service for its CephSignOn", "declare a mgmt-gateway service")
}

func signOnRequest(c api.Catalog, object api.Object, base Request, controller string) (SignOnRequest, error) {
	cluster, _ := c.Find(api.CephCluster, base.Cluster)
	provider, found := c.Find(api.OIDCProvider, object.Spec().Get("oidcProviderRef").Text())
	if !found {
		return SignOnRequest{}, cephRequestInvalid.Fail("CephSignOn "+object.Name()+" names no declared OIDCProvider", "declare the OIDCProvider that spec.oidcProviderRef names")
	}
	at := slices.IndexFunc(base.Auxiliary, func(a AuxiliaryReference) bool { return a.Name == signOnImage })
	if at < 0 {
		return SignOnRequest{}, cephRecordsMissing.Fail("CephSignOn "+object.Name()+" needs spec.images.oauth2_proxy of CephCluster "+base.Cluster, "set spec.images.oauth2_proxy")
	}
	bundle := provider.Spec().Get("trustBundleRef").Text()
	r := SignOnRequest{Version: SignOnVersion, Object: object.Name(), Cluster: base.Cluster, Controller: controller, Image: base.Auxiliary[at].Image,
		HostSecretRefs: hostSecretRefs(base.Hosts), Machines: requestMachines(base), Bindings: []ServiceBinding{},
		Provider: SignOnProvider{IssuerURL: provider.Spec().Get("issuerURL").Text(), Name: provider.Name(), TrustBundle: bundle != ""}}
	document, _, err := signOnDocument(cluster, object, r.Provider.IssuerURL, r.Provider.TrustBundle)
	if err != nil {
		return SignOnRequest{}, err
	}
	r.Document = document
	if r.Gateway, err = signOnGateway(cluster); err != nil {
		return SignOnRequest{}, err
	}
	for _, binding := range object.Spec().Get("secrets").Items() {
		r.Bindings = append(r.Bindings, ServiceBinding{Path: binding.Get("path").Text(), SecretRef: binding.Get("secretRef").Text(), ServiceRef: SignOnUnitType})
	}
	if bundle != "" {
		r.Bindings = append(r.Bindings, ServiceBinding{Path: signOnCABundle, SecretRef: bundle, ServiceRef: SignOnUnitType})
	}
	slices.SortFunc(r.Bindings, func(a, b ServiceBinding) int { return strings.Compare(a.Path, b.Path) })
	return r, nil
}

const (
	signOnEnableImpact     = "Bootwright enables the dashboard's OAuth2 single sign-on; local sign-in returns only after bootwright cluster sign-on suspend"
	signOnPortImpact       = "while sign-on is on, any client that reaches port 8443 of a manager host acts as the dashboard admin without signing in (an upstream defect); keep it reachable only from the management gateway hosts"
	signOnDelegateImpact   = "cephadm reconfigures the management gateway, Grafana, Prometheus and Alertmanager for single sign-on; their blocks do not change"
	signOnLoginsImpact     = "while sign-on is on, cephadm turns off Grafana's login form and Prometheus and Alertmanager basic authentication: their local logins gate nothing until bootwright cluster sign-on suspend"
	signOnRestrictedImpact = "the management gateway requires an allowed value in the OIDC claim for every signed-in user and does not accept JWT bearer sign-in"
	signOnAudienceImpact   = "every user the identity provider signs in for this client reaches Prometheus and Alertmanager, silences included: restrict the client to Ceph operators in the provider"
)

func signOnImpacts(r SignOnRequest) []string {
	audience := signOnAudienceImpact
	if document, _, ok := r.document(); ok && len(document.Arguments) != 0 {
		audience = signOnRestrictedImpact
	}
	impacts := []string{"realize Ceph sign-on " + r.Object + " of " + r.Cluster, "the management gateway signs users in through oauth2-proxy and OIDCProvider " + r.Provider.Name,
		signOnEnableImpact, signOnDelegateImpact, signOnLoginsImpact, audience, signOnPortImpact}
	if !slices.ContainsFunc(r.Bindings, func(b ServiceBinding) bool { return b.Path == cephcluster.SignOnCertificate }) {
		impacts = append(impacts, "cephadm signs oauth2-proxy's TLS certificate")
	}
	if !r.Provider.TrustBundle {
		impacts = append(impacts, "oauth2-proxy verifies the issuer of OIDCProvider "+r.Provider.Name+" with its image's system trust store")
	}
	return impacts
}

func signOnRemoval(verb reconciliation.Verb, r SignOnRequest) lifecycle.Removal {
	if verb == reconciliation.Destroy {
		return lifecycle.Removal{Description: "remove Ceph sign-on " + r.Object + " of " + r.Cluster, Impacts: []string{"remove Ceph sign-on " + r.Object + " of " + r.Cluster}, Consumes: []string{reconciliation.AuthorizationDataLoss}}
	}
	impacts := signOnImpacts(r)
	return lifecycle.Removal{Description: impacts[0], Impacts: impacts, Consumes: []string{}}
}

func signOnDependencies(r SignOnRequest, base Request, cluster api.Object) []string {
	dependencies := []string{ClusterBlockID(r.Cluster), ImageBlockID(r.Cluster), ServiceUnitBlockID(r.Cluster, ServiceUnit{Type: "mgmt-gateway"})}
	if _, needed := settingsRequest(api.Catalog{}, base, cluster); needed {
		dependencies = append(dependencies, SettingsBlockID(r.Cluster))
	}
	slices.Sort(dependencies)
	return dependencies
}

type SignOnCapability struct {
	runner Runner
	proofs DependencyProofs
}

func NewSignOn(runner Runner) SignOnCapability { return SignOnCapability{runner: runner} }

func (c SignOnCapability) WithProofs(proofs DependencyProofs) SignOnCapability {
	c.proofs = proofs
	return c
}

func (c SignOnCapability) Plan(ctx context.Context, input lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
	plan := lifecycle.CapabilityPlan{Definitions: []reconciliation.BlockDefinition{}}
	if err := ctx.Err(); err != nil {
		return plan, err
	}
	if input.State == nil {
		return plan, cephRequestInvalid.Fail("Ceph planning requires compiled state", "")
	}
	catalog := input.State.Effective()
	for _, object := range catalog.OfKind(api.CephSignOn) {
		cluster, _ := catalog.Find(api.CephCluster, object.Spec().Get("clusterRef").Text())
		base, err := planDerivation(input).base(catalog, cluster)
		if err != nil {
			return plan, err
		}
		r, err := signOnRequest(catalog, object, base, input.Controller)
		if err != nil {
			return plan, err
		}
		canonical, err := r.Canonical()
		if err != nil {
			return plan, err
		}
		if _, err := DecodeSignOnRequest(canonical); err != nil {
			return plan, err
		}
		removal := signOnRemoval(input.Verb, r)
		plan.Definitions = append(plan.Definitions, reconciliation.BlockDefinition{ID: SignOnBlockID(r.Cluster), Description: removal.Description, Stage: reconciliation.StageClusters,
			Requires: []reconciliation.ObjectRef{}, Dependencies: signOnDependencies(r, base, cluster), Exclusive: exclusiveFor("signon", Request{Cluster: r.Cluster}), Throttle: ThrottleKey(r.Cluster),
			Impacts: removal.Impacts, Consumes: removal.Consumes, Kind: string(api.CephSignOn), Object: object.Name(), Implementation: SignOnImplementation, ContentDigest: SignOnContentDigest(), Request: canonical})
		plan.Secrets = append(plan.Secrets, r.SecretReferences()...)
	}
	slices.Sort(plan.Secrets)
	plan.Secrets = slices.Compact(plan.Secrets)
	return plan, nil
}

func (SignOnCapability) Removal(ctx context.Context, block reconciliation.Block) (lifecycle.Removal, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.Removal{}, err
	}
	r, err := DecodeSignOnRequest(block.Request)
	if err != nil {
		return lifecycle.Removal{}, err
	}
	return signOnRemoval(reconciliation.Destroy, r), nil
}

func (SignOnCapability) RequiresOnlineRemoval(reconciliation.Block) bool { return true }

func (SignOnCapability) identity(ctx context.Context, e lifecycle.Execution) (SignOnRequest, error) {
	if err := ctx.Err(); err != nil {
		return SignOnRequest{}, err
	}
	r, err := DecodeSignOnRequest(e.Block.Request)
	if err != nil {
		return SignOnRequest{}, err
	}
	if e.Block.Kind != string(api.CephSignOn) || e.Block.Object != r.Object || e.Block.ID != SignOnBlockID(r.Cluster) {
		return SignOnRequest{}, cephRequestInvalid.Fail("Ceph sign-on request does not match its frozen resource identity", "")
	}
	if e.ApplyOperation == "" {
		return SignOnRequest{}, cephRecordsMissing.Fail("Ceph sign-on requires the immutable original apply identity", "")
	}
	return r, nil
}

func SignOnOwner(proofs []lifecycle.BlockEvidence, cluster string) (string, error) {
	proof, err := provedClusterProof(proofs, cluster)
	if err != nil {
		return "", err
	}
	if proof.Operation == "" {
		return "", cephRecordsMissing.Fail("the creation proof of CephCluster "+cluster+" names no operation", "restore the original apply evidence")
	}
	return proof.Operation, nil
}

func gatewayCertificate(proofs []lifecycle.BlockEvidence, cluster string) (string, error) {
	id := ServiceUnitBlockID(cluster, ServiceUnit{Type: "mgmt-gateway"})
	for _, proof := range proofs {
		if proof.Block.ID != id || !completedApply(proof, string(api.CephCluster), cluster, Implementation) {
			continue
		}
		e, err := decodeEvidence(proof.Evidence)
		if err != nil || !e.Ready || !e.Endpoint.Reachable || !ValidImageDigest("sha256:"+e.Endpoint.CertificateSHA256) {
			return "", cephProofMismatch.Fail("the apply proof of CephCluster "+cluster+"'s management gateway does not prove the certificate it serves", "restore the original apply evidence")
		}
		return e.Endpoint.CertificateSHA256, nil
	}
	return "", cephRecordsMissing.Fail("CephCluster "+cluster+" lacks the apply proof of its management gateway", "apply the management gateway first; a sign-on proves it enforces sign-on only against the certificate that proof reports")
}

func (c SignOnCapability) prepare(ctx context.Context, e lifecycle.Execution) (SignOnRequest, Request, error) {
	r, err := c.identity(ctx, e)
	if err != nil {
		return SignOnRequest{}, Request{}, err
	}
	proofs := provedFor(e)
	owner, err := SignOnOwner(proofs, r.Cluster)
	if err != nil {
		return SignOnRequest{}, Request{}, err
	}
	scope, err := ProvedCluster(proofs, r.Cluster, owner)
	if err != nil {
		return SignOnRequest{}, Request{}, err
	}
	if !slices.Equal(r.Machines, scope.Machines) || !slices.Equal(r.HostSecretRefs, hostSecretRefs(scope.Scope.Hosts)) {
		return SignOnRequest{}, Request{}, cephProofMismatch.Fail("CephSignOn "+r.Object+" froze another Machine set or host keys than the creation proof of CephCluster "+r.Cluster, "restore the original apply evidence")
	}
	authored := slices.IndexFunc(scope.Authored, func(a AuxiliaryReference) bool { return a.Name == signOnImage })
	completed := slices.IndexFunc(scope.Scope.Auxiliary, func(a AuxiliaryReference) bool { return a.Name == signOnImage })
	if authored < 0 || completed < 0 || scope.Authored[authored].Image != r.Image {
		return SignOnRequest{}, Request{}, cephProofMismatch.Fail("CephSignOn "+r.Object+" froze an oauth2-proxy image its CephCluster's settings did not freeze", "restore the original apply evidence")
	}
	if err := bindPhysicalHosts(scope.Scope.Hosts, e, c.proofs); err != nil {
		return SignOnRequest{}, Request{}, err
	}
	certificate, err := gatewayCertificate(proofs, r.Cluster)
	if err != nil {
		return SignOnRequest{}, Request{}, err
	}
	proof, _ := provedClusterProof(proofs, r.Cluster)
	unit, err := DecodeRequest(proof.Block.Request)
	if err != nil {
		return SignOnRequest{}, Request{}, err
	}
	unit.Action, unit.Owner, unit.FSID, unit.Image, unit.Auxiliary = "service", owner, FSIDFor(owner, r.Cluster), scope.Scope.Image, scope.Scope.Auxiliary
	unit.Service = &ServiceUnit{Bindings: r.Bindings, Documents: []json.RawMessage{r.Document}, Type: SignOnUnitType,
		SignOn: &SignOnTarget{CertificateSHA256: certificate, Count: r.Gateway.Count, Endpoint: r.Gateway.Endpoint, Grafana: r.Gateway.Grafana, Hosts: r.Gateway.Hosts, Port: r.Gateway.Port, VirtualIP: r.Gateway.VirtualIP}}
	return r, unit, nil
}

func signOnDeadline(operation string) (time.Duration, error) {
	calls, waits := 0, 0
	switch operation {
	case "apply", "resume":
		calls, waits = 7, 1
	case "observe":
		calls = 2
	case "suspend_prepare":
		calls = 2
	case "suspend":
		calls, waits = 4, 1
	default:
		return 0, cephRequestInvalid.Fail("the Ceph sign-on adapter runs no "+operation+" operation", "")
	}
	return time.Duration(calls*nativeCall+waits*readinessWait+60) * time.Second, nil
}

func signOnMaterial(e lifecycle.Execution, unit Request, operation string, readers DependencyProofs) (lifecycle.Execution, []lifecycle.MaterialFile, func(), error) {
	cleanup := func() {}
	if len(unit.Hosts) == 0 {
		return e, nil, cleanup, cephRequestInvalid.Fail("Ceph has no frozen hosts", "")
	}
	leaves, err := unitMaterial(e, unit, operation)
	if err != nil {
		return e, nil, cleanup, err
	}
	bound, knownFile, clearKnown, err := bindKnownHosts(e, unit.Hosts, unit.Security, readers)
	if err != nil {
		return e, nil, cleanup, err
	}
	return bound, append([]lifecycle.MaterialFile{fleetKeyFile(unit.Hosts[0].SSHKeyRef), knownFile}, leaves...), clearKnown, nil
}

func (c SignOnCapability) run(ctx context.Context, e lifecycle.Execution, operation string, unit Request) (lifecycle.RunResult, error) {
	return c.runLending(ctx, e, operation, operation, unit, nil)
}

func (c SignOnCapability) runLending(ctx context.Context, e lifecycle.Execution, operation, lending string, unit Request, values map[string]string) (lifecycle.RunResult, error) {
	if c.runner == nil {
		return lifecycle.RunResult{}, cephUnconfigured.Fail("the Ceph sign-on adapter is not configured", "")
	}
	canonical, err := unit.Canonical()
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	deadline, err := signOnDeadline(operation)
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	if operation == "apply" || operation == "resume" {
		documents := unitDocuments(unit)
		if len(documents) == 1 && len(documents[0].Arguments) != 0 {
			deadline += time.Duration(nativeCall+readinessWait) * time.Second
		}
	}
	execution, files, cleanup, err := signOnMaterial(e, unit, lending, c.proofs)
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	defer cleanup()
	return c.runner.Run(ctx, lifecycle.RunFor(execution, lifecycle.Invocation{Implementation: SignOnImplementation, Operation: operation, Variable: "bootwright_ceph", Canonical: canonical,
		Placement: machine.Placement{Connection: machine.ConnectionLocal, Machine: unit.Controller}, Materials: files, Values: values, Refusals: adapterRefusals(), NativeErrors: true, Deadline: deadline}))
}

func (c SignOnCapability) Apply(ctx context.Context, e lifecycle.Execution) (lifecycle.Result, error) {
	_, unit, err := c.prepare(ctx, e)
	if err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeFailed}, err
	}
	result, err := c.run(ctx, e, "apply", unit)
	if err != nil {
		return lifecycle.Result{Outcome: lifecycle.AttemptOutcome(err)}, err
	}
	effect, err := EvaluateEvidence(result.Evidence, unit, e.Block.RequestDigest, false)
	if err != nil || effect != reconciliation.EffectCompleted || len(result.Produced) != 0 {
		lifecycle.ClearProduced(result.Produced)
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}, cephUnproved.Fail("Ceph sign-on did not prove the requested postcondition", "")
	}
	if !result.Outcome.Settled() {
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}, cephResultInvalid.Fail("Ceph sign-on returned an unknown outcome", "")
	}
	return lifecycle.Result{Outcome: result.Outcome, Evidence: result.Evidence}, nil
}

func (c SignOnCapability) Observe(ctx context.Context, e lifecycle.Execution) (lifecycle.Observation, error) {
	unknown := lifecycle.Observation{Effect: reconciliation.EffectUnknown}
	_, unit, err := c.prepare(ctx, e)
	if err != nil {
		return unknown, err
	}
	if e.SignOn != "" {
		return c.observeSuspended(ctx, e, unit)
	}
	result, err := c.run(ctx, e, "observe", unit)
	if err != nil {
		return unknown, err
	}
	if len(result.Produced) != 0 {
		lifecycle.ClearProduced(result.Produced)
		return unknown, cephResultInvalid.Fail("Ceph sign-on returned a value it never produces", "")
	}
	effect, err := EvaluateEvidence(result.Evidence, unit, e.Block.RequestDigest, false)
	if err != nil {
		return unknown, err
	}
	return lifecycle.Observation{Effect: effect, Evidence: result.Evidence}, nil
}

func (c SignOnCapability) Destroy(ctx context.Context, e lifecycle.Execution) (lifecycle.Result, error) {
	if e.RetiredWithHosts {
		evidence, err := c.retirement(ctx, e)
		if err != nil {
			return lifecycle.Result{Outcome: reconciliation.OutcomeFailed}, err
		}
		return lifecycle.Result{Outcome: reconciliation.OutcomeChanged, Evidence: evidence}, nil
	}
	if _, err := c.identity(ctx, e); err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeFailed}, err
	}
	return lifecycle.Result{Outcome: reconciliation.OutcomeFailed}, cephHostsSurvive.Fail(survivingReason, survivingRemedy)
}

func (c SignOnCapability) ObserveRemoval(ctx context.Context, e lifecycle.Execution) (lifecycle.Observation, error) {
	unknown := lifecycle.Observation{Effect: reconciliation.EffectUnknown}
	if e.RetiredWithHosts {
		evidence, err := c.retirement(ctx, e)
		if err != nil {
			return unknown, err
		}
		return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Evidence: evidence}, nil
	}
	if _, err := c.identity(ctx, e); err != nil {
		return unknown, err
	}
	return unknown, cephHostsSurvive.Fail(survivingReason, survivingRemedy)
}

func (c SignOnCapability) retirement(ctx context.Context, e lifecycle.Execution) (json.RawMessage, error) {
	r, err := c.identity(ctx, e)
	if err != nil {
		return nil, err
	}
	owner, err := SignOnOwner(provedFor(e), r.Cluster)
	if err != nil {
		return nil, err
	}
	want := Retirement{Action: "signon", Cluster: r.Cluster, FSID: FSIDFor(owner, r.Cluster), RetiredWith: r.Machines}
	pending, err := pendingPhysicalClusters(e, []string{r.Cluster}, r.Machines)
	if err != nil {
		return nil, err
	}
	want.PendingErase = len(pending) != 0
	evidence, err := json.Marshal(want)
	if err != nil {
		return nil, cephRequestInvalid.Fail("the Ceph retirement evidence cannot be encoded", "")
	}
	if _, err := evaluateRetirement(evidence, want, true); err != nil {
		return nil, err
	}
	return evidence, nil
}

func (SignOnCapability) HostMachines(block reconciliation.Block) ([]string, error) {
	r, err := DecodeSignOnRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return slices.Clone(r.Machines), nil
}

func (SignOnCapability) Quiescent(ctx context.Context, probe lifecycle.Probe) (lifecycle.Quiescence, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.Quiescence{State: lifecycle.Unproved}, err
	}
	if probe.RetiredWithHosts {
		r, err := DecodeSignOnRequest(probe.Block.Request)
		if err != nil {
			return lifecycle.Quiescence{State: lifecycle.Unproved}, err
		}
		if len(r.Machines) != 0 {
			pending, err := pendingPhysicalClusters(probe.Execution(), []string{r.Cluster}, r.Machines)
			if err != nil {
				return lifecycle.Quiescence{State: lifecycle.Unproved}, err
			}
			if len(pending) != 0 {
				return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: pendingPhysicalReason}, nil
			}
			return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: retiredReason}, nil
		}
	}
	return lifecycle.Quiescence{State: lifecycle.Unproved, Reason: survivingReason, Remedy: survivingRemedy}, nil
}

func (SignOnCapability) Claimed(catalog api.Catalog) []string { return ofKind(catalog, api.CephSignOn) }

func (SignOnCapability) SecretReferences(block reconciliation.Block) ([]string, error) {
	r, err := DecodeSignOnRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return r.SecretReferences(), nil
}

func (SignOnCapability) SecretClasses(block reconciliation.Block) (secrets.Classes, error) {
	r, err := DecodeSignOnRequest(block.Request)
	if err != nil {
		return nil, err
	}
	classes := secrets.Classes{}.Add(secrets.EffectMaterial, r.HostSecretRefs...)
	for _, binding := range r.Bindings {
		class := secrets.EffectMaterial
		if binding.Path == signOnCABundle {
			class = secrets.TrustAnchor
		}
		classes.Add(class, binding.SecretRef)
	}
	return classes, nil
}

func unselectedSignOnImage(c api.Catalog, cluster api.Object) (string, string, bool) {
	signOns := cephcluster.ClusterSignOns(c, cluster.Name())
	if len(signOns) == 0 || cluster.Spec().Get("images").Has(signOnImage) {
		return "", "", false
	}
	return "CephSignOn " + signOns[0].Name() + " needs cephadm's container_image_oauth2_proxy, and spec.images.oauth2_proxy of CephCluster " + cluster.Name() + " is not set; Bootwright keeps no default",
		"set spec.images.oauth2_proxy of CephCluster " + cluster.Name() + " to an image beneath its Entitlement registry; on an installed cluster that is an input change: take back what this context owns with bootwright destroy --context <context>, then apply", true
}
