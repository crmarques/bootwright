package ceph

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/ansible"
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const auxiliaryRepository = PinnedRegistry + "/cp/ibm-ceph/"

var (
	haproxyChild    = "sha256:" + strings.Repeat("a", 64)
	keepalivedChild = "sha256:" + strings.Repeat("b", 64)
)

func syntheticAuxiliary() []AuxiliaryImage {
	return []AuxiliaryImage{
		{Name: "haproxy", Base: auxiliaryRepository + "haproxy-synthetic", Tag: "synthetic-2", Image: auxiliaryRepository + "haproxy-synthetic:synthetic-2"},
		{Name: "keepalived", Base: auxiliaryRepository + "keepalived-synthetic", Image: auxiliaryRepository + "keepalived-synthetic@" + keepalivedChild},
	}
}

func authoredReferences(images []AuxiliaryImage) []AuxiliaryReference {
	var out []AuxiliaryReference
	for _, a := range images {
		out = append(out, AuxiliaryReference{Name: a.Name, Image: a.Image})
	}
	return out
}

func auxiliaryImageRequest(authored string, images []AuxiliaryImage) ImageRequest {
	r := ImageRequest{Version: ImageVersion, Cluster: "example", Controller: "controller", Release: PinnedRelease, Base: PinnedRepository, Tag: PinnedTag, Image: authored,
		Registry: PinnedRegistry, RegistryRef: "registry", Auxiliary: images}
	if strings.Contains(authored, "@") {
		r.Tag = ""
	}
	return r
}

func auxiliaryBlock(t *testing.T, r ImageRequest) reconciliation.Block {
	t.Helper()
	canonical, err := r.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	definition := reconciliation.BlockDefinition{ID: ImageBlockID(r.Cluster), Kind: string(api.CephCluster), Object: r.Cluster, Implementation: ImageImplementation, ContentDigest: ImageContentDigest(), Request: canonical}
	digest, err := reconciliation.RequestDigest(definition)
	if err != nil {
		t.Fatal(err)
	}
	return reconciliation.Block{BlockDefinition: definition, RequestDigest: digest}
}

func auxiliaryEvidence(t *testing.T, block reconciliation.Block, r ImageRequest, children []AuxiliaryEvidence) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(ImageEvidence{Postcondition: true, Release: r.Release, Base: r.Base, Tag: r.Tag, Digest: resolvedDigest, Platform: PinnedPlatform, Request: block.RequestDigest, Auxiliary: children})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func resolvedChildren(images []AuxiliaryImage, digests ...string) []AuxiliaryEvidence {
	var out []AuxiliaryEvidence
	for i, a := range images {
		out = append(out, AuxiliaryEvidence{Name: a.Name, Base: a.Base, Tag: a.Tag, Digest: digests[i], Platform: PinnedPlatform})
	}
	return out
}

func auxiliaryProof(t *testing.T, authored string, images []AuxiliaryImage, digests ...string) lifecycle.BlockEvidence {
	t.Helper()
	r := auxiliaryImageRequest(authored, images)
	block := auxiliaryBlock(t, r)
	return lifecycle.BlockEvidence{Kind: block.Kind, Object: block.Object, Implementation: block.Implementation, Verb: reconciliation.Apply, State: reconciliation.BlockDone,
		Evidence: auxiliaryEvidence(t, block, r, resolvedChildren(images, digests...)), Block: block}
}

func pythonTuple(t *testing.T, path, name string) []string {
	t.Helper()
	source := string(ansible.Assets()[path])
	_, tuple, found := strings.Cut(source, "\n"+name+" = (\n")
	tuple, _, closed := strings.Cut(tuple, "\n)\n")
	if !found || !closed {
		t.Fatalf("%s declares no %s tuple", path, name)
	}
	var out []string
	for _, match := range regexp.MustCompile(`"([^"]*)"`).FindAllStringSubmatch(tuple, -1) {
		out = append(out, match[1])
	}
	return out
}

func TestAuxiliaryNameTableMatchesTheCephadmImageKeys(t *testing.T) {
	want := []string{"alertmanager", "alloy", "grafana", "haproxy", "keepalived", "loki", "nginx", "node_exporter", "nvmeof", "oauth2_proxy", "prometheus", "promtail", "samba", "samba_metrics", "snmp_gateway"}
	if !slices.Equal(AuxiliaryNames, want) || !slices.IsSorted(AuxiliaryNames) || slices.Contains(AuxiliaryNames, "base") {
		t.Fatalf("auxiliary names %v, want %v", AuxiliaryNames, want)
	}
	name := regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	union := map[string]bool{}
	for _, names := range auxiliaryFamilies {
		for _, n := range names {
			union[n] = true
		}
	}
	if got := slices.Sorted(maps.Keys(union)); !slices.Equal(got, AuxiliaryNames) {
		t.Fatalf("the families need %v, the table holds %v", got, AuxiliaryNames)
	}
	for _, n := range AuxiliaryNames {
		if !name.MatchString(n) {
			t.Fatalf("auxiliary name %q is not a cephadm container_image_<name> suffix", n)
		}
	}
	families := map[string][]string{
		"alertmanager": {"alertmanager"}, "alloy": {"alloy"}, "grafana": {"grafana"}, "ingress": {"haproxy", "keepalived"}, "loki": {"loki"},
		"mgmt-gateway": {"nginx"}, "node-exporter": {"node_exporter"}, "nvmeof": {"nvmeof"}, "oauth2-proxy": {"oauth2_proxy"},
		"prometheus": {"prometheus"}, "promtail": {"promtail"}, "smb": {"samba", "samba_metrics"}, "snmp-gateway": {"snmp_gateway"},
	}
	if !reflect.DeepEqual(auxiliaryFamilies, families) {
		t.Fatalf("auxiliary families %v, want %v", auxiliaryFamilies, families)
	}
	for _, path := range []string{"ceph_registry_discovery.py", "ceph_native.py"} {
		full := "collections/ansible_collections/bootwright/core/plugins/module_utils/" + path
		if got := pythonTuple(t, full, "AUXILIARY_NAMES"); !slices.Equal(got, AuxiliaryNames) {
			t.Fatalf("%s admits %v, the plan %v", path, got, AuxiliaryNames)
		}
	}
}

func TestAuxiliaryReferencesStayInsideTheEntitlementNamespace(t *testing.T) {
	const registryURL = "cp.icr.io/cp"
	tagged, err := auxiliaryImage("haproxy", "cp.icr.io/cp/ibm-ceph/haproxy-synthetic", "synthetic-2", registryURL)
	if err != nil || tagged != (AuxiliaryImage{Name: "haproxy", Base: "cp.icr.io/cp/ibm-ceph/haproxy-synthetic", Tag: "synthetic-2", Image: "cp.icr.io/cp/ibm-ceph/haproxy-synthetic:synthetic-2"}) {
		t.Fatalf("tagged auxiliary image %+v %v", tagged, diagnostics.Of(err))
	}
	digested, err := auxiliaryImage("haproxy", "cp.icr.io/cp/ibm-ceph/haproxy-synthetic", haproxyChild, registryURL)
	if err != nil || digested != (AuxiliaryImage{Name: "haproxy", Base: "cp.icr.io/cp/ibm-ceph/haproxy-synthetic", Image: "cp.icr.io/cp/ibm-ceph/haproxy-synthetic@" + haproxyChild}) {
		t.Fatalf("digest auxiliary image %+v %v", digested, diagnostics.Of(err))
	}
	for _, base := range []string{"quay.io/ceph/haproxy", "cp.icr.io/cpx/haproxy"} {
		_, err := auxiliaryImage("haproxy", base, "synthetic-2", registryURL)
		refusedWith(t, err, "lifecycle.unsupported", "the auxiliary image haproxy is not beneath the Entitlement registry cp.icr.io/cp", "set spec.images.haproxy.base to a repository beneath the Entitlement registry.url")
	}
	_, err = auxiliaryImage("haproxy", "cp.icr.io/cp/ibm-ceph/haproxy-synthetic", "", registryURL)
	refusedWith(t, err, "lifecycle.unsupported", "the auxiliary image haproxy names neither a tag nor a sha256 digest", "set spec.images.haproxy.version to a tag or a sha256 digest")

	valid := auxiliaryImageRequest(PinnedRepository+":"+PinnedTag, syntheticAuxiliary())
	data, err := valid.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if decoded, err := DecodeImageRequest(data); err != nil || !reflect.DeepEqual(decoded, valid) {
		t.Fatalf("a valid auxiliary set refused: %+v %v", decoded, diagnostics.Of(err))
	}
	for name, damage := range map[string]func([]AuxiliaryImage) []AuxiliaryImage{
		"another registry": func(a []AuxiliaryImage) []AuxiliaryImage {
			a[0].Base, a[0].Image = "quay.io/ceph/haproxy", "quay.io/ceph/haproxy:synthetic-2"
			return a
		},
		"registry name prefix": func(a []AuxiliaryImage) []AuxiliaryImage {
			a[0].Base, a[0].Image = PinnedRegistry+"x/haproxy", PinnedRegistry+"x/haproxy:synthetic-2"
			return a
		},
		"unknown name":     func(a []AuxiliaryImage) []AuxiliaryImage { a[0].Name = "base"; return a },
		"unsorted names":   func(a []AuxiliaryImage) []AuxiliaryImage { return []AuxiliaryImage{a[1], a[0]} },
		"duplicate names":  func(a []AuxiliaryImage) []AuxiliaryImage { return []AuxiliaryImage{a[0], a[0]} },
		"tag mismatch":     func(a []AuxiliaryImage) []AuxiliaryImage { a[0].Tag = "synthetic-3"; return a },
		"digest with tag":  func(a []AuxiliaryImage) []AuxiliaryImage { a[1].Tag = "synthetic-2"; return a },
		"malformed digest": func(a []AuxiliaryImage) []AuxiliaryImage { a[1].Image = a[1].Base + "@sha256:x"; return a },
	} {
		t.Run(name, func(t *testing.T) {
			r := auxiliaryImageRequest(PinnedRepository+":"+PinnedTag, damage(syntheticAuxiliary()))
			data, err := r.Canonical()
			if err != nil {
				t.Fatal(err)
			}
			_, err = DecodeImageRequest(data)
			refusedWith(t, err, "lifecycle.state", "the frozen Ceph image request has inconsistent coordinates", "")
		})
	}
}

func auxiliaryService(serviceType, id string, spec api.Value) api.Value {
	pairs := []any{"service_type", serviceType, "placement", requestMap("label", "mon", "count", 1)}
	if id != "" {
		pairs = append(pairs, "service_id", id)
	}
	if spec.Present() {
		pairs = append(pairs, "spec", spec)
	}
	return requestMap(pairs...)
}

func auxiliaryGraph(t *testing.T, services []api.Value, children ...api.Object) *compilation.State {
	t.Helper()
	catalog := pinnedCatalog(t, func(spec api.Value) api.Value {
		return spec.WithPath(api.ListValue(append(spec.Get("services").Items(), services...)...), "services")
	})
	catalog = api.NewCatalog(append(catalog.Objects(), children...))
	return compilation.NewState(catalog, catalog, nil)
}

func withImages(t *testing.T, state *compilation.State, images ...string) *compilation.State {
	t.Helper()
	catalog := state.Effective()
	cluster, _ := catalog.Find(api.CephCluster, "example")
	spec := cluster.Spec()
	for _, name := range images {
		spec = spec.WithPath(requestMap("base", PinnedRegistry+"/synthetic/"+name, "version", "synthetic-1"), "images", name)
	}
	objects := catalog.Objects()
	for i := range objects {
		if objects[i].Identity() == cluster.Identity() {
			objects[i] = cluster.WithSpec(spec)
		}
	}
	catalog = api.NewCatalog(objects)
	return compilation.NewState(catalog, catalog, nil)
}

func refusedEverywhere(t *testing.T, state *compilation.State, reason, remedy string) {
	t.Helper()
	for kind, name := range map[api.Kind]string{api.CephCluster: "example", api.CephPool: "volumes", api.CephClient: "consumer"} {
		want := []lifecycle.Refusal{{Kind: string(kind), Name: name, Reason: reason, Remediation: remedy}}
		if got := New(kind, nil).Unsupported(state); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s refuses %+v, want %+v", kind, got, want)
		}
	}
}

func unauthoredRefusal(t *testing.T, state *compilation.State, family, service, name string) {
	t.Helper()
	refusedEverywhere(t, state, "the "+family+" service "+service+" needs cephadm's container_image_"+name+", and spec.images."+name+" is not set; Bootwright keeps no default",
		"set spec.images."+name+" of CephCluster example to an image beneath its Entitlement registry, or remove "+service+" from its spec.services, together with its spec.serviceBindings entries")
}

func unneededRefusal(t *testing.T, state *compilation.State, name string) {
	t.Helper()
	refusedEverywhere(t, state, "spec.images."+name+" is set, but no declared service or CephSMBResource of CephCluster example needs cephadm's container_image_"+name,
		"remove spec.images."+name+" from CephCluster example")
}

func notRefused(t *testing.T, state *compilation.State, what string) {
	t.Helper()
	for _, kind := range []api.Kind{api.CephCluster, api.CephPool, api.CephClient} {
		if got := New(kind, nil).Unsupported(state); len(got) != 0 {
			t.Fatalf("%s: %s refuses %+v", what, kind, got)
		}
	}
}

func objectsGateway(t *testing.T, ingress api.Value) *compilation.State {
	t.Helper()
	return auxiliaryGraph(t, []api.Value{rgwService("objects"), ingress}, childOf(api.CephObjectGateway, "objects", "serviceRef", "rgw.objects"))
}

func TestDeclaredFamilyWithoutItsAuxiliaryImageRefusesNamingTheField(t *testing.T) {
	notRefused(t, auxiliaryGraph(t, nil), "the plain graph")
	for _, c := range []struct{ family, id, service, name string }{
		{"alertmanager", "", "alertmanager", "alertmanager"}, {"alloy", "", "alloy", "alloy"}, {"grafana", "", "grafana", "grafana"},
		{"loki", "", "loki", "loki"}, {"mgmt-gateway", "", "mgmt-gateway", "nginx"}, {"node-exporter", "", "node-exporter", "node_exporter"},
		{"nvmeof", "rbd.gateways", "nvmeof.rbd.gateways", "nvmeof"}, {"oauth2-proxy", "", "oauth2-proxy", "oauth2_proxy"},
		{"prometheus", "", "prometheus", "prometheus"}, {"promtail", "", "promtail", "promtail"}, {"smb", "shares", "smb.shares", "samba"},
		{"snmp-gateway", "", "snmp-gateway", "snmp_gateway"},
	} {
		t.Run(c.family, func(t *testing.T) {
			unauthoredRefusal(t, auxiliaryGraph(t, []api.Value{auxiliaryService(c.family, c.id, api.Value{})}), c.family, c.service, c.name)
		})
	}
	smb := auxiliaryGraph(t, []api.Value{auxiliaryService("smb", "shares", api.Value{})})
	unauthoredRefusal(t, withImages(t, smb, "samba"), "smb", "smb.shares", "samba_metrics")
	two := auxiliaryGraph(t, []api.Value{auxiliaryService("prometheus", "", api.Value{}), auxiliaryService("alertmanager", "", api.Value{})})
	unauthoredRefusal(t, two, "alertmanager", "alertmanager", "alertmanager")
	unauthoredRefusal(t, withImages(t, two, "alertmanager"), "prometheus", "prometheus", "prometheus")
	gateway := objectsGateway(t, ingressService("rgw.objects"))
	unauthoredRefusal(t, gateway, "ingress", "ingress.rgw.objects", "haproxy")
	unauthoredRefusal(t, withImages(t, gateway, "haproxy"), "ingress", "ingress.rgw.objects", "keepalived")
	unauthoredRefusal(t, withImages(t, gateway, "keepalived"), "ingress", "ingress.rgw.objects", "haproxy")
	keepaliveOnly := objectsGateway(t, ingressService("rgw.objects").WithPath(api.BoolValue(true), "spec", "keepalive_only"))
	unauthoredRefusal(t, keepaliveOnly, "ingress", "ingress.rgw.objects", "keepalived")
	notRefused(t, withImages(t, keepaliveOnly, "keepalived"), "a keepalive-only ingress with its keepalived image")
	notRefused(t, withImages(t, gateway, "haproxy", "keepalived"), "an ingress with both images")
}

func TestAuthoredImageOfAnUndeclaredFamilyRefuses(t *testing.T) {
	plain := auxiliaryGraph(t, nil)
	unneededRefusal(t, withImages(t, plain, "haproxy"), "haproxy")
	unneededRefusal(t, withImages(t, plain, "snmp_gateway", "alertmanager"), "alertmanager")
	keepaliveOnly := objectsGateway(t, ingressService("rgw.objects").WithPath(api.BoolValue(true), "spec", "keepalive_only"))
	unneededRefusal(t, withImages(t, keepaliveOnly, "haproxy", "keepalived"), "haproxy")
	unneededRefusal(t, withImages(t, plain, "samba", "samba_metrics"), "samba")
	withSMB := api.NewCatalog(append(plain.Effective().Objects(), smbJoin()))
	notRefused(t, withImages(t, compilation.NewState(withSMB, withSMB, nil), "samba", "samba_metrics"), "samba images a CephSMBResource needs")
	unneededRefusal(t, withImages(t, compilation.NewState(withSMB, withSMB, nil), "samba", "samba_metrics", "loki"), "loki")
}

func TestUndeclaredFamilyNeedsNoAuxiliaryImage(t *testing.T) {
	state := auxiliaryGraph(t, nil)
	notRefused(t, state, "the plain graph")
	plan := plannedAuxiliary(t, state)
	for _, id := range []string{ImageBlockID("example"), ClusterBlockID("example")} {
		if bytes.Contains(plan[id].Request, []byte("auxiliary")) {
			t.Fatalf("%s carries an auxiliary member: %s", id, plan[id].Request)
		}
	}
	if _, planned := plan[SettingsBlockID("example")]; planned {
		t.Fatal("a cluster without auxiliary images plans a settings block")
	}
}

func plannedAuxiliary(t *testing.T, state *compilation.State) map[string]reconciliation.BlockDefinition {
	t.Helper()
	out := map[string]reconciliation.BlockDefinition{}
	input := lifecycle.PlanInput{State: state, Verb: reconciliation.Apply, Controller: "controller", Context: lifecycle.ContextIdentity{Name: "synthetic"}}
	image, err := NewImage(nil).Plan(context.Background(), input)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	core, err := New(api.CephCluster, nil).Plan(context.Background(), input)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	host, err := NewHost(nil).Plan(context.Background(), input)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	for _, d := range slices.Concat(image.Definitions, core.Definitions, host.Definitions) {
		out[d.ID] = d
	}
	return out
}

func TestAuthoredAuxiliaryImageReachesTheFrozenImageRequest(t *testing.T) {
	haproxy, keepalived := PinnedRegistry+"/synthetic/haproxy", PinnedRegistry+"/synthetic/keepalived"
	state := objectsGateway(t, ingressService("rgw.objects"))
	cluster, _ := state.Effective().Find(api.CephCluster, "example")
	spec := cluster.Spec().WithPath(requestMap("base", keepalived, "version", keepalivedChild), "images", "keepalived").
		WithPath(requestMap("base", haproxy, "version", "synthetic-1"), "images", "haproxy")
	objects := state.Effective().Objects()
	for i := range objects {
		if objects[i].Identity() == cluster.Identity() {
			objects[i] = cluster.WithSpec(spec)
		}
	}
	catalog := api.NewCatalog(objects)
	state = compilation.NewState(catalog, catalog, nil)
	notRefused(t, state, "an ingress with its authored images")
	want := []AuxiliaryImage{{Name: "haproxy", Base: haproxy, Image: haproxy + ":synthetic-1", Tag: "synthetic-1"}, {Name: "keepalived", Base: keepalived, Image: keepalived + "@" + keepalivedChild}}
	plan := plannedAuxiliary(t, state)
	image, err := DecodeImageRequest(plan[ImageBlockID("example")].Request)
	if err != nil || !reflect.DeepEqual(image.Auxiliary, want) {
		t.Fatalf("image request auxiliary %+v %v, want %+v", image.Auxiliary, diagnostics.Of(err), want)
	}
	core, err := DecodeRequest(plan[ClusterBlockID("example")].Request)
	if err != nil || !reflect.DeepEqual(core.Auxiliary, authoredReferences(want)) {
		t.Fatalf("core request auxiliary %+v %v", core.Auxiliary, err)
	}
	hosts := 0
	for id, d := range plan {
		if !strings.HasPrefix(id, "ceph-host-") {
			continue
		}
		hosts++
		host, err := DecodeHostRequest(d.Request)
		if err != nil || !reflect.DeepEqual(host.Cluster.Auxiliary, core.Auxiliary) {
			t.Fatalf("%s auxiliary %+v %v", id, host.Cluster.Auxiliary, err)
		}
	}
	if _, planned := plan[SettingsBlockID("example")]; !planned || hosts == 0 {
		t.Fatalf("settings planned %v, %d host blocks", planned, hosts)
	}
	authored, err := authoredAuxiliary(catalog, cluster.WithSpec(spec))
	if err != nil || !reflect.DeepEqual(authored, want) {
		t.Fatalf("authored %+v %v", authored, diagnostics.Of(err))
	}
}

func TestAuxiliarySetFeedsImageAndHostContentDigests(t *testing.T) {
	plainImage := auxiliaryImageRequest(PinnedRepository+":"+PinnedTag, nil)
	withImage := auxiliaryImageRequest(PinnedRepository+":"+PinnedTag, syntheticAuxiliary()[:1])
	before, errBefore := plainImage.Canonical()
	after, errAfter := withImage.Canonical()
	if errBefore != nil || errAfter != nil || bytes.Equal(before, after) || bytes.Contains(before, []byte("auxiliary")) {
		t.Fatalf("image request bytes do not follow the auxiliary set: %s", before)
	}
	if decoded, err := DecodeImageRequest(after); err != nil || !reflect.DeepEqual(decoded, withImage) {
		t.Fatalf("image request round trip %+v %v", decoded, diagnostics.Of(err))
	}
	plain := coreRequest()
	with := coreRequest()
	with.Auxiliary = []AuxiliaryReference{{Name: "haproxy", Image: auxiliaryRepository + "haproxy-synthetic@" + haproxyChild}}
	before, errBefore = plain.Canonical()
	after, errAfter = with.Canonical()
	if errBefore != nil || errAfter != nil || bytes.Equal(before, after) || bytes.Contains(before, []byte("auxiliary")) {
		t.Fatalf("core request bytes do not follow the auxiliary set: %s", before)
	}
	if decoded, err := DecodeRequest(after); err != nil || !reflect.DeepEqual(decoded.Auxiliary, with.Auxiliary) {
		t.Fatalf("core request round trip %+v %v", decoded.Auxiliary, err)
	}
	if !reflect.DeepEqual(scopeOf(with).Auxiliary, with.Auxiliary) {
		t.Fatal("the host scope lost the auxiliary set")
	}
	before, errBefore = HostRequest{Version: HostVersion, Cluster: scopeOf(plain), Host: "node-1"}.canonical()
	after, errAfter = HostRequest{Version: HostVersion, Cluster: scopeOf(with), Host: "node-1"}.canonical()
	if errBefore != nil || errAfter != nil || bytes.Equal(before, after) || bytes.Contains(before, []byte("auxiliary")) {
		t.Fatalf("host request bytes do not follow the auxiliary set: %s", before)
	}
}

func TestPrepareFillsTheAuxiliaryDigestsFromTheOriginalResolution(t *testing.T) {
	images := syntheticAuxiliary()
	want := []AuxiliaryReference{{Name: "haproxy", Image: images[0].Base + "@" + haproxyChild}, {Name: "keepalived", Image: images[1].Base + "@" + keepalivedChild}}
	r := coreRequest()
	r.Auxiliary = authoredReferences(images)
	e := executionFor(t, r)
	e.ApplyProved = []lifecycle.BlockEvidence{auxiliaryProof(t, r.Image, images, haproxyChild, keepalivedChild)}
	frozen := bytes.Clone(e.Block.Request)
	prepared, err := New(api.CephCluster, nil).prepare(context.Background(), e)
	if err != nil || prepared.Image != PinnedRepository+"@"+resolvedDigest || !reflect.DeepEqual(prepared.Auxiliary, want) || !bytes.Equal(e.Block.Request, frozen) {
		t.Fatalf("prepared %q %+v %v", prepared.Image, prepared.Auxiliary, diagnostics.Of(err))
	}

	he, hr, _ := hostExecution(t, "node-2")
	hr.Cluster.Auxiliary = authoredReferences(images)
	he.Block.Request, err = hr.canonical()
	if err != nil {
		t.Fatal(err)
	}
	he.ApplyProved[0] = auxiliaryProof(t, hr.Cluster.Image, images, haproxyChild, keepalivedChild)
	host, err := NewHost(nil).prepare(context.Background(), he)
	if err != nil || !reflect.DeepEqual(host.Cluster.Auxiliary, want) {
		t.Fatalf("host prepared %+v %v", host.Cluster.Auxiliary, diagnostics.Of(err))
	}

	for name, proof := range map[string]lifecycle.BlockEvidence{
		"fewer":         auxiliaryProof(t, r.Image, images[:1], haproxyChild),
		"none":          auxiliaryProof(t, r.Image, nil),
		"another image": auxiliaryProof(t, r.Image, []AuxiliaryImage{images[0], {Name: "keepalived", Base: images[1].Base, Tag: "synthetic-4", Image: images[1].Base + ":synthetic-4"}}, haproxyChild, keepalivedChild),
		"another name":  auxiliaryProof(t, r.Image, []AuxiliaryImage{images[0], {Name: "loki", Base: images[1].Base, Image: images[1].Image}}, haproxyChild, keepalivedChild),
	} {
		t.Run(name, func(t *testing.T) {
			e.ApplyProved = []lifecycle.BlockEvidence{proof}
			_, err := New(api.CephCluster, nil).prepare(context.Background(), e)
			refusedWith(t, err, "lifecycle.state", "the Ceph image resolution proof was resolved for another authored auxiliary image set", "")
		})
	}
	plain := executionFor(t, coreRequest())
	if prepared, err := New(api.CephCluster, nil).prepare(context.Background(), plain); err != nil || prepared.Auxiliary != nil {
		t.Fatalf("a cluster without an auxiliary set prepared %+v %v", prepared.Auxiliary, err)
	}
}

func TestAuxiliaryResolutionMustProveEachChild(t *testing.T) {
	images := syntheticAuxiliary()
	r := auxiliaryImageRequest(PinnedRepository+":"+PinnedTag, images)
	block := auxiliaryBlock(t, r)
	apply := func(children []AuxiliaryEvidence) (lifecycle.Result, error) {
		runner := &recordingRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: auxiliaryEvidence(t, block, r, children)}}
		return NewImage(runner).Apply(context.Background(), imageExecution(block))
	}
	if result, err := apply(resolvedChildren(images, haproxyChild, keepalivedChild)); err != nil || result.Outcome != reconciliation.OutcomeUnchanged {
		t.Fatalf("a proved auxiliary set refused: %+v %v", result, diagnostics.Of(err))
	}
	extra := append(resolvedChildren(images, haproxyChild, keepalivedChild), AuxiliaryEvidence{Name: "loki", Base: auxiliaryRepository + "loki", Digest: haproxyChild, Platform: PinnedPlatform})
	arm := resolvedChildren(images, haproxyChild, keepalivedChild)
	arm[0].Platform = "linux/arm64"
	for name, children := range map[string][]AuxiliaryEvidence{
		"missing":  resolvedChildren(images, haproxyChild, keepalivedChild)[:1],
		"extra":    extra,
		"platform": arm,
		"none":     nil,
	} {
		t.Run(name, func(t *testing.T) {
			if result, err := apply(children); err == nil || result.Outcome != reconciliation.OutcomeUnknown {
				t.Fatalf("an unproved auxiliary resolution reached %+v %v", result, err)
			}
		})
	}
	result, err := apply(resolvedChildren(images, haproxyChild, haproxyChild))
	if d := diagnostics.Of(err); result.Outcome != reconciliation.OutcomeFailed || len(d) != 1 ||
		d[0].Message != "the resolved Ceph image or an auxiliary image does not match its authored digest (image-digest-mismatch)" {
		t.Fatalf("an auxiliary child other than its authored digest reached %+v %v", result, d)
	}
	full := make([]AuxiliaryEvidence, 0, len(AuxiliaryNames))
	for _, name := range AuxiliaryNames {
		full = append(full, AuxiliaryEvidence{Name: name, Base: auxiliaryRepository + strings.Repeat("r", 200), Tag: strings.Repeat("t", 64), Digest: haproxyChild, Platform: PinnedPlatform})
	}
	data := auxiliaryEvidence(t, block, r, full)
	if _, err := decodeImageEvidence(data, block.RequestDigest); err != nil {
		t.Fatalf("a full auxiliary evidence of %d bytes is refused: %v", len(data), err)
	}
}

func TestAuxiliaryImageKeysAreTheClosedNameTable(t *testing.T) {
	ceph := api.Field{Shape: api.Schema(api.CephCluster)}
	images, found := ceph.Shape.Field("images")
	if !found || images.Required || images.Shape.Type != api.Mapping || images.Shape.Open || images.Shape.Element != nil {
		t.Fatalf("spec.images is not a closed optional record: %+v", images)
	}
	var names []string
	for _, entry := range images.Shape.Fields {
		names = append(names, entry.Name)
		shape := entry.Shape
		if entry.Required || shape.Type != api.Mapping || shape.Open || len(shape.Fields) != 2 ||
			shape.Fields[0].Name != "base" || shape.Fields[0].Required || shape.Fields[0].Shape.Rule != "registry-base" ||
			shape.Fields[1].Name != "version" || shape.Fields[1].Required || shape.Fields[1].Shape.Rule != "native-version" {
			t.Fatalf("spec.images.%s is not a closed record of base and version", entry.Name)
		}
	}
	if !slices.Equal(names, AuxiliaryNames) {
		t.Fatalf("spec.images admits %v, the plan %v", names, AuxiliaryNames)
	}
}
