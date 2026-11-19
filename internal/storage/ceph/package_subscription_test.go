package ceph

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

func subscribedCatalog(machines ...string) api.Catalog {
	objects := packageCatalog().Objects()
	for i, object := range objects {
		if object.Kind() == api.Machine && slices.Contains(machines, object.Name()) {
			objects[i] = object.WithSpec(object.Spec().WithPath(api.StringValue("subscribed"), "os", "install", "profileRef"))
		}
	}
	return api.NewCatalog(append(objects,
		api.NewObject(api.MachineInstallProfile, "subscribed", api.Value{}, requestMap(
			"os", requestMap("family", "rhel", "version", "9.8", "architecture", "x86_64"),
			"installer", requestMap("anaconda", requestMap("packageSource", requestMap("subscription", api.MapValue()))),
			"subscription", requestMap("entitlementRef", "satellite"),
		)),
		api.NewObject(api.Entitlement, "satellite", api.Value{}, requestMap("rhsm", requestMap(
			"management", "managed", "organizationRef", "rhsm-organization", "activationKeyRef", "rhsm-activation-key",
			"satellite", requestMap("hostname", "satellite.example.test", "contentBaseURL", "https://satellite.example.test/pulp/content", "trustBundleRef", "satellite-ca"),
		))),
	))
}

func subscribedRequest(t *testing.T, catalog api.Catalog, edit func(*Request)) (Request, error) {
	t.Helper()
	cluster, _ := catalog.Find(api.CephCluster, "example")
	r, err := baseRequest(catalog, cluster, "synthetic")
	if err != nil {
		t.Fatal(err)
	}
	r.Controller = "controller"
	if edit != nil {
		edit(&r)
	}
	r.Packages, err = packageSources(catalog, r, "synthetic")
	return r, err
}

func TestSubscriptionHostsFreezeTheirSatelliteAndTreeHostsKeepTheirBytes(t *testing.T) {
	r, err := subscribedRequest(t, subscribedCatalog("node-2"), nil)
	if err != nil {
		t.Fatal(err)
	}
	tree := packageRequest(t)
	for i, source := range r.Packages.Sources {
		if source.Host != "node-2" {
			if source != tree.Packages.Sources[i] {
				t.Fatalf("a hosted-tree host's source changed in a mixed cluster: %+v", source)
			}
			continue
		}
		want := PackageSource{Host: "node-2", Profile: "subscribed", CertificateRef: "satellite-ca",
			Subscription: &SubscriptionSource{Hostname: "satellite.example.test", ContentBaseURL: "https://satellite.example.test/pulp/content"}}
		if source.Subscription == nil || *source.Subscription != *want.Subscription || source.Media != "" || source.PublicationPath != "" || source.URL != "" ||
			source.Host != want.Host || source.Profile != want.Profile || source.CertificateRef != want.CertificateRef {
			t.Fatalf("wrong subscription source: %+v", source)
		}
	}
	for i := range r.Packages.Sources {
		if r.Packages.Sources[i].Subscription == nil {
			r.Packages.Sources[i].MediaSHA256, r.Packages.Sources[i].MediaSize = strings.Repeat("a", 64), 123456
		}
	}
	data, err := r.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "request-packages-subscription", data, false)
	if bytes.Count(data, []byte(`"media":`)) != 2 || bytes.Count(data, []byte(`"subscription":`)) != 1 {
		t.Fatalf("a subscription source carries a DVD member: %s", data)
	}
	decoded, err := DecodeRequest(data)
	if err != nil {
		t.Fatal(err)
	}
	if roundtrip, err := decoded.Canonical(); err != nil || !bytes.Equal(data, roundtrip) {
		t.Fatal("the subscription source lost frozen fields")
	}
	if !slices.Contains(scopeOf(r).secretReferences(), "satellite-ca") || !slices.Contains(scopeOf(r).secretReferences(), "serving-tls") {
		t.Fatalf("secret references %v omit a package anchor", scopeOf(r).secretReferences())
	}
}

func TestAProvidedHostTakesTheBootstrapMachinesSatelliteOrHostedDVD(t *testing.T) {
	provided := func(r *Request) { r.Hosts[1].Provided = true }
	r, err := subscribedRequest(t, subscribedCatalog("node-1", "node-2", "node-3"), provided)
	if err != nil {
		t.Fatalf("a provided host under a subscription-sourced bootstrap refused: %+v", diagnostics.Of(err))
	}
	source := r.Packages.Sources[1]
	want := PackageSource{Host: r.Hosts[1].Name, Profile: "subscribed", CertificateRef: "satellite-ca",
		Subscription: &SubscriptionSource{Hostname: "satellite.example.test", ContentBaseURL: "https://satellite.example.test/pulp/content"}}
	if source.Subscription == nil || *source.Subscription != *want.Subscription || source.Media != "" || source.PublicationPath != "" || source.URL != "" ||
		source.Host != want.Host || source.Profile != want.Profile || source.CertificateRef != want.CertificateRef {
		t.Fatalf("the provided host did not take the bootstrap Machine's Satellite: %+v", source)
	}
	r, err = subscribedRequest(t, subscribedCatalog("node-2"), provided)
	if err != nil {
		t.Fatal(err)
	}
	if tree := packageRequest(t).Packages.Sources[1]; r.Packages.Sources[1] != tree {
		t.Fatalf("a provided host left the bootstrap Machine's hosted DVD for its own profile: %+v, want %+v", r.Packages.Sources[1], tree)
	}
}

func TestSubscriptionPackageSourceRefusals(t *testing.T) {
	refused := func(name string, err error, reason string) {
		t.Helper()
		var failure *diagnostics.Failure
		if !errors.As(err, &failure) || len(failure.Diagnostics) != 1 || failure.Diagnostics[0].Code != cephPackageSourceUnsupported.Code || !strings.Contains(failure.Diagnostics[0].Message, reason) {
			t.Fatalf("%s: refusal %v does not say %q", name, err, reason)
		}
	}
	if _, err := subscribedRequest(t, subscribedCatalog("node-1", "node-2", "node-3"), nil); err != nil {
		t.Fatalf("an all-subscription cluster refused: %v", err)
	}
	for name, edit := range map[string]func(api.Value) api.Value{
		"no satellite": func(spec api.Value) api.Value { return spec.WithPath(spec.Get("rhsm").Without("satellite"), "rhsm") },
		"no bundle": func(spec api.Value) api.Value {
			return spec.WithPath(spec.Get("rhsm", "satellite").Without("trustBundleRef"), "rhsm", "satellite")
		},
		"external": func(spec api.Value) api.Value {
			return spec.WithPath(api.StringValue("external"), "rhsm", "management")
		},
	} {
		objects := subscribedCatalog("node-1").Objects()
		for i, object := range objects {
			if object.Kind() == api.Entitlement && object.Name() == "satellite" {
				objects[i] = object.WithSpec(edit(object.Spec()))
			}
		}
		_, err := subscribedRequest(t, api.NewCatalog(objects), nil)
		refused(name, err, "come from the Satellite of a managed registration")
	}
	for _, root := range []string{"https://satellite.example.test/pulp/./content", "https://satellite.example.test/pulp/%2e%2e/content", `https://satellite.example.test/pulp/a%5Cb`} {
		objects := subscribedCatalog("node-1").Objects()
		for i, object := range objects {
			if object.Kind() == api.Entitlement && object.Name() == "satellite" {
				objects[i] = object.WithSpec(object.Spec().WithPath(api.StringValue(root), "rhsm", "satellite", "contentBaseURL"))
			}
		}
		_, err := subscribedRequest(t, api.NewCatalog(objects), nil)
		refused(root, err, "holds a '.', '..' or backslash path segment")
	}
	objects := packageCatalog().Objects()
	for i, object := range objects {
		if object.Kind() == api.MachineInstallProfile {
			objects[i] = object.WithSpec(object.Spec().WithPath(requestMap("url", requestMap("url", "https://mirror.example.test/rhel")), "installer", "anaconda", "packageSource"))
		}
	}
	_, err := subscribedRequest(t, api.NewCatalog(objects), nil)
	refused("another source", err, "hosted DVD or its Satellite subscription")
}

func updatedTreeCatalog(policy, management string, satellite bool) api.Catalog {
	objects := subscribedCatalog().Objects()
	for i, object := range objects {
		switch {
		case object.Kind() == api.MachineInstallProfile && object.Name() == "rhel":
			spec := object.Spec().WithPath(api.StringValue("satellite"), "subscription", "entitlementRef")
			if policy != "" {
				spec = spec.WithPath(api.StringValue(policy), "updates", "policy")
			}
			objects[i] = object.WithSpec(spec)
		case object.Kind() == api.Entitlement && object.Name() == "satellite":
			spec := object.Spec().WithPath(api.StringValue("redhatRhel"), "type").WithPath(api.StringValue(management), "rhsm", "management")
			if !satellite {
				spec = spec.WithPath(spec.Get("rhsm").Without("satellite"), "rhsm")
			}
			objects[i] = object.WithSpec(spec)
		}
	}
	return api.NewCatalog(objects)
}

func TestATreeHostItsSatelliteUpdatesTakesItsDependenciesFromThatSatellite(t *testing.T) {
	tree := packageRequest(t)
	for name, test := range map[string]struct {
		catalog   api.Catalog
		provided  bool
		satellite bool
	}{
		"updated by default":     {updatedTreeCatalog("", "managed", true), false, true},
		"updated within release": {updatedTreeCatalog("withinRelease", "managed", true), false, true},
		"not updated":            {updatedTreeCatalog("none", "managed", true), false, false},
		"registered to the CDN":  {updatedTreeCatalog("withinRelease", "managed", false), false, false},
		"external registration":  {updatedTreeCatalog("withinRelease", "external", true), false, false},
		"provided host":          {updatedTreeCatalog("withinRelease", "managed", true), true, true},
	} {
		r, err := subscribedRequest(t, test.catalog, func(r *Request) { r.Hosts[1].Provided = test.provided })
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for i, source := range r.Packages.Sources {
			if !test.satellite || test.provided && i == 1 {
				if source != tree.Packages.Sources[i] {
					t.Fatalf("%s: host %s left its hosted DVD: %+v", name, source.Host, source)
				}
				continue
			}
			if source.Subscription == nil || source.Subscription.Hostname != "satellite.example.test" || source.CertificateRef != "satellite-ca" || source.Media != "" || source.URL != "" || source.Profile != "rhel" {
				t.Fatalf("%s: host %s does not take its dependencies from the Satellite that updated it: %+v", name, source.Host, source)
			}
		}
	}
}

func TestSubscriptionSourceBindsWithoutATreeProofAndLendsItsBundle(t *testing.T) {
	r, err := subscribedRequest(t, subscribedCatalog("node-1", "node-2", "node-3"), nil)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := bindPackageSources(lifecycle.Execution{}, r.Packages, packageProofs{})
	if err != nil || !slices.EqualFunc(bound.Sources, r.Packages.Sources, func(a, b PackageSource) bool { return a.Subscription == b.Subscription && a.MediaSHA256 == "" }) {
		t.Fatalf("a subscription source needed or took a tree proof: %v", err)
	}
	for i := range r.Hosts {
		r.Hosts[i].HostKeyRef = "host-key"
	}
	e := withMaterial(executionFor(t, r), r)
	e.Material["satellite-ca"] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.CertificatePart: []byte("satellite-ca-certificate")})
	for _, host := range r.Hosts {
		request := HostRequest{Version: HostVersion, Cluster: scopeOf(r), Host: host.Name}
		_, files, cleanup, err := hostMaterial(e, request, "apply", dependencyProofs{})
		if err != nil {
			t.Fatal(err)
		}
		cleanup()
		index, _ := request.source()
		if !slices.ContainsFunc(files, func(file lifecycle.MaterialFile) bool {
			return file.Secret == "satellite-ca" && file.Part == secrets.CertificatePart && file.Variable == packageCertificateVariable(index)
		}) {
			t.Fatalf("%s: the Satellite CA bundle is not lent as its package certificate", host.Name)
		}
	}
}

func hostBlockRequirements(t *testing.T, catalog api.Catalog, realized ...string) ([]reconciliation.ObjectRef, error) {
	t.Helper()
	cluster, _ := catalog.Find(api.CephCluster, "example")
	objects := catalog.Objects()
	for i := range objects {
		if objects[i].Identity() == cluster.Identity() {
			objects[i] = cluster.WithSpec(cluster.Spec().WithPath(api.StringValue(PinnedTransport), "cephadm", "clusterSSH", "transport"))
		}
	}
	catalog = api.NewCatalog(objects)
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "synthetic"}, State: compilation.NewState(catalog, catalog, nil), Controller: "controller"}
	var definitions []reconciliation.BlockDefinition
	var requires []reconciliation.ObjectRef
	for _, capability := range []lifecycle.Capability{NewImage(&recordingRunner{}), NewHost(&recordingRunner{})} {
		plan, err := capability.Plan(context.Background(), input)
		if err != nil {
			t.Fatal(diagnostics.Of(err))
		}
		for _, definition := range plan.Definitions {
			if definition.Implementation == HostImplementation {
				requires = append(requires, definition.Requires...)
			}
		}
		definitions = append(definitions, plan.Definitions...)
	}
	for _, block := range append([]string{"Machine/node-1", "Machine/node-2", "Machine/node-3"}, realized...) {
		kind, name, _ := strings.Cut(block, "/")
		definitions = append(definitions, reconciliation.BlockDefinition{ID: strings.ToLower(kind) + "-" + name, Description: "synthetic " + name, Stage: reconciliation.StageMachines, Kind: kind, Object: name, Implementation: "synthetic", ContentDigest: strings.Repeat("f", 64), Request: json.RawMessage(`{}`)})
	}
	_, err := reconciliation.NewPlan(reconciliation.Apply, definitions)
	return requires, err
}

func TestAHostBlockWaitsOnlyForTheProfilesThatPublishATree(t *testing.T) {
	tree := reconciliation.ObjectRef{Kind: string(api.MachineInstallProfile), Object: "rhel"}
	subscribed := reconciliation.ObjectRef{Kind: string(api.MachineInstallProfile), Object: "subscribed"}
	for name, test := range map[string]struct {
		catalog  api.Catalog
		realized []string
		tree     bool
	}{
		"every host installs from a subscription": {subscribedCatalog("node-1", "node-2", "node-3"), nil, false},
		"a mixed cluster":                         {subscribedCatalog("node-2"), []string{"MachineInstallProfile/rhel"}, true},
		"a hosted tree its Satellite updates":     {updatedTreeCatalog("withinRelease", "managed", true), []string{"MachineInstallProfile/rhel"}, true},
	} {
		requires, err := hostBlockRequirements(t, test.catalog, test.realized...)
		if err != nil {
			t.Fatalf("%s: the plan refuses: %v", name, diagnostics.Of(err))
		}
		if slices.Contains(requires, subscribed) || slices.Contains(requires, tree) != test.tree {
			t.Fatalf("%s: host blocks require %+v", name, requires)
		}
	}
}
