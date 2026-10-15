package ceph

import (
	"bytes"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

func packageCatalog() api.Catalog {
	objects := poolRequestCatalog(3, 3).Objects()
	objects = append(objects,
		api.NewObject(api.MachineInstallProfile, "rhel", api.Value{}, requestMap(
			"os", requestMap("family", "rhel", "version", "9.8", "architecture", "x86_64"),
			"installer", requestMap("anaconda", requestMap("packageSource", requestMap("hostedTree", requestMap("fromMedia", "local-media:rhel-dvd.iso", "artifactServerEndpoint", requestMap("serverRef", "artifacts", "endpointRef", "http"))))),
		)),
		api.NewObject(api.Machine, "controller", api.Value{}, requestMap("network", requestMap("addresses", api.ListValue(requestMap("name", "ip", "address", "192.0.2.10"))))),
		api.NewObject(api.ArtifactServer, "artifacts", api.Value{}, requestMap(
			"management", "managed", "machineRef", "controller", "tls", requestMap("secretRef", "serving-tls"),
			"listeners", api.ListValue(requestMap("name", "http", "protocol", "http", "port", 8080), requestMap("name", "https", "protocol", "https", "port", 8443)),
			"endpoints", api.ListValue(requestMap("name", "http", "listenerRef", "http", "addressRef", "ip"), requestMap("name", "https", "listenerRef", "https", "addressRef", "ip")),
		)),
	)
	return api.NewCatalog(objects)
}

func packageRequest(t *testing.T) Request {
	t.Helper()
	catalog := packageCatalog()
	cluster, _ := catalog.Find(api.StorageCluster, "example")
	r, err := baseRequest(catalog, cluster, "synthetic")
	if err != nil {
		t.Fatal(err)
	}
	r.Controller = "controller"
	r.Packages, err = packageSources(catalog, r, "synthetic")
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestPackageSourcesUseOwnedDVDAndDeclaredVerifiedListener(t *testing.T) {
	r := packageRequest(t)
	for _, source := range r.Packages.Sources {
		if source.URL != "https://192.0.2.10:8443/os/rhel/tree" || source.Media != "rhel-dvd.iso" || source.Profile != "rhel" || source.CertificateRef != "serving-tls" || source.MediaSHA256 != "" {
			t.Fatalf("wrong frozen source: %+v", source)
		}
	}
	catalog := packageCatalog()
	profile, _ := catalog.Find(api.MachineInstallProfile, "rhel")
	server, _ := catalog.Find(api.ArtifactServer, "artifacts")
	entitlement, _ := catalog.Find(api.Entitlement, "ibm")
	for _, replacement := range []api.Object{
		entitlement.WithSpec(entitlement.Spec().WithPath(api.BoolValue(false), "license", "accept")),
		profile.WithSpec(profile.Spec().WithPath(api.StringValue("https://other.example.test/dvd.iso"), "installer", "anaconda", "packageSource", "hostedTree", "fromMedia")),
		server.WithSpec(server.Spec().WithPath(api.ListValue(requestMap("name", "http", "listenerRef", "http", "addressRef", "ip")), "endpoints")),
		server.WithSpec(server.Spec().WithPath(api.StringValue(""), "tls", "secretRef")),
	} {
		objects := catalog.Objects()
		for i, object := range objects {
			if object.Identity() == replacement.Identity() {
				objects[i] = replacement
			}
		}
		if _, err := packageSources(api.NewCatalog(objects), r, "synthetic"); err == nil {
			t.Fatalf("accepted incompatible/unauthenticated source %s", replacement.Identity())
		}
	}
}

type packageProofs struct {
	dependencyProofs
	tree PackageTree
}

func (p packageProofs) PackageTree([]lifecycle.BlockEvidence, string) (PackageTree, error) {
	return p.tree, nil
}

func TestPackageSourceBindingUsesOriginalApplyPinWithoutAliasing(t *testing.T) {
	r := packageRequest(t)
	original := r.Packages
	source := r.Packages.Sources[0]
	proof := packageProofs{tree: PackageTree{Context: "synthetic", Media: source.Media, MediaSHA256: strings.Repeat("a", 64), MediaSize: 123456, Path: source.PublicationPath}}
	bound, err := bindPackageSources(lifecycle.Execution{Context: "renamed"}, r.Packages, proof)
	if err != nil {
		t.Fatal(err)
	}
	if original.Sources[0].MediaSHA256 != "" || bound.Sources[0].MediaSHA256 != proof.tree.MediaSHA256 {
		t.Fatal("binding aliased the frozen plan or lost its original pin")
	}
	proof.tree.Path += "/another"
	if _, err := bindPackageSources(lifecycle.Execution{}, bound, proof); err == nil {
		t.Fatal("accepted another DVD publication")
	}
	if _, err := bindPackageSources(lifecycle.Execution{}, bound, nil); err == nil {
		t.Fatal("accepted absent evidence owner")
	}
	if _, err := bindPackageSources(lifecycle.Execution{}, nil, proof); err == nil {
		t.Fatal("accepted absent package source")
	}
}

func TestPackageMaterialLendsOnlyServingCertificate(t *testing.T) {
	cluster := packageRequest(t)
	for i := range cluster.Hosts {
		cluster.Hosts[i].HostKeyRef = "host-key"
	}
	e := withMaterial(executionFor(t, cluster), cluster)
	e.Material["serving-tls"] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.CertificatePart: []byte("public-certificate"), secrets.PrivateKeyPart: []byte("private-tls-key")})
	if _, files, cleanup, err := boundMaterial(e, cluster, "apply", dependencyProofs{}); err != nil {
		t.Fatal(err)
	} else {
		cleanup()
		for _, file := range files {
			if file.Secret == "serving-tls" {
				t.Fatal("the cluster block still lends package certificates")
			}
		}
	}
	for _, host := range cluster.Hosts {
		r := HostRequest{Version: HostVersion, Cluster: scopeOf(cluster), Host: host.Name}
		_, files, cleanup, err := hostMaterial(e, r, "apply", dependencyProofs{})
		if err != nil {
			t.Fatal(err)
		}
		cleanup()
		index, _ := r.source()
		found := 0
		for _, file := range files {
			if file.Secret == "serving-tls" {
				found++
				if file.Part != secrets.CertificatePart || file.Variable != packageCertificateVariable(index) {
					t.Fatal("lent private serving key or another host's certificate slot")
				}
			}
		}
		if found != 1 {
			t.Fatalf("%s: package certificate missing", host.Name)
		}
	}
}

func TestPackageRequestGolden(t *testing.T) {
	r := packageRequest(t)
	for i := range r.Packages.Sources {
		r.Packages.Sources[i].MediaSHA256 = strings.Repeat("a", 64)
		r.Packages.Sources[i].MediaSize = 123456
	}
	data, err := r.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "request-packages", data, false)
	decoded, err := DecodeRequest(data)
	if err != nil {
		t.Fatal(err)
	}
	roundtrip, err := decoded.Canonical()
	if err != nil || !bytes.Equal(data, roundtrip) {
		t.Fatal("package request lost frozen fields")
	}
}

func TestEveryCapabilityDeadlineCoversItsFrozenBudgets(t *testing.T) {
	r := packageRequest(t)
	hosts := len(r.Hosts)
	const native = 660 * time.Second
	const convergence = 900 * time.Second
	const margin = 60 * time.Second
	for _, action := range []string{"cluster", "pool", "client"} {
		r.Action = action
		for _, operation := range []string{"apply", "observe", "destroy"} {
			minimum := time.Duration(1+hosts)*native + margin
			if operation == "destroy" {
				minimum += native
			}
			if operation == "apply" {
				minimum = time.Duration(hosts+2)*native + convergence + margin
				if action == "cluster" {
					minimum += time.Duration(4*hosts+2)*native + time.Duration(2*hosts)*native
					verified := r
					verified.ClusterSSHTransport = "verified-tunnel"
					verifiedDeadline, err := cephDeadline(verified, operation)
					if err != nil || verifiedDeadline < minimum+2*native || verifiedDeadline > lifecycle.MaxDeadline {
						t.Fatalf("verified tunnel deadline %s does not cover %s: %v", verifiedDeadline, minimum+2*native, err)
					}
				}
			}
			deadline, err := cephDeadline(r, operation)
			if err != nil || deadline < minimum || deadline > lifecycle.MaxDeadline {
				t.Fatalf("%s %s deadline %s does not cover %s within %s: %v", action, operation, deadline, minimum, lifecycle.MaxDeadline, err)
			}
		}
	}
	r.Action = "cluster"
	r.Hosts = append(r.Hosts, r.Hosts[0])
	if _, err := cephDeadline(r, "apply"); err == nil {
		t.Fatal("silently clamped a budget beyond the run ceiling")
	}
}
