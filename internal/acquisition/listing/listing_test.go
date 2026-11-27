package listing

import (
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/acquisition"
)

func TestAShortImageNameIsClassedByTheRegistryItResolvesTo(t *testing.T) {
	t.Parallel()
	row := imageAcquisition("service image", "ubuntu/squid@sha256:"+strings.Repeat("a", 64), []string{"controller"}, "Proxy/egress")
	if row.Artifact != "docker.io/ubuntu/squid" || row.Class != acquisition.ClassPublic {
		t.Fatalf("a short name: %+v", row)
	}
	if row := imageAcquisition("service image", "registry.example.test:5000/squid@sha256:"+strings.Repeat("a", 64), nil, ""); row.Class != acquisition.ClassDeclared {
		t.Fatalf("an enterprise registry: %+v", row)
	}
}

func TestOnlyARegistryTheListKnowsGetsItsTokenAndDeliveryHosts(t *testing.T) {
	t.Parallel()
	rows := withRegistryHosts([]Row{
		{Kind: "service image", Artifact: "docker.io/ubuntu/squid", Origin: "docker.io/ubuntu/squid", Machines: []string{"b"}, Consumer: "Proxy/two"},
		{Kind: "service image", Artifact: "docker.io/4km3/dnsmasq", Origin: "docker.io/4km3/dnsmasq", Machines: []string{"a"}, Consumer: "DNSServer/one"},
		{Kind: "service image", Artifact: "registry.example.test/squid", Origin: "registry.example.test/squid", Machines: []string{"c"}, Consumer: "Proxy/three"},
		{Kind: "client", Artifact: "helm", Origin: "https://quay.io/helm", Machines: []string{"d"}, Consumer: "Environment/lab"},
	})
	got := map[string]Row{}
	for _, row := range rows[4:] {
		got[row.Artifact] = row
	}
	if len(got) != 3 || len(rows) != 7 {
		t.Fatalf("only the Docker Hub image names companion hosts: %+v", rows[4:])
	}
	if row := got["auth.docker.io"]; row.Kind != "registry token service" || row.Class != acquisition.ClassPublic || row.Consumer != "DNSServer/one, Proxy/two" || !slices.Equal(row.Machines, []string{"b", "a"}) {
		t.Errorf("the token service merges its consumers: %+v", row)
	}
	if got["registry-1.docker.io"].Kind != "registry endpoint" || got["production.cloudfront.docker.com"].Kind != "registry blob host" {
		t.Errorf("the endpoint and blob host: %+v", got)
	}
}

func TestAnImageNamedByTagKeepsTheTagAsItsVersion(t *testing.T) {
	t.Parallel()
	row := imageAcquisition("add-on catalog image", "registry.example.test:5000/team/catalog:v1.2", nil, "")
	if row.Artifact != "registry.example.test:5000/team/catalog" || row.Version != "v1.2" || row.SHA256 != "" {
		t.Fatalf("a tagged reference: %+v", row)
	}
	if row := imageAcquisition("add-on catalog image", "registry.example.test:5000/team/catalog", nil, ""); row.Artifact != "registry.example.test:5000/team/catalog" || row.Version != "" {
		t.Fatalf("a port is not a tag: %+v", row)
	}
}

func TestARepositoryKeyURLIsListedWithoutItsCredentialOrQuery(t *testing.T) {
	t.Parallel()
	if got := redactedLocation("https://user:secret@keys.example.test/RPM-GPG-KEY?token=abc#frag"); got != "https://keys.example.test/RPM-GPG-KEY" {
		t.Fatalf("a credentialed key URL is listed as %s", got)
	}
}

// A shared CDN remains public for public registry pulls; only a vendor
// registry's delivery edge is an approved fetch-only exception.
func TestSharedCDNRowsKeepVendorDeliverySeparateFromPublicPulls(t *testing.T) {
	t.Parallel()
	rows := withRegistryHosts([]Row{
		{Kind: "default catalog image", Origin: "registry.redhat.io/redhat/catalog", Machines: []string{"node"}, Consumer: "OpenShiftCluster/vendor"},
		{Kind: "mirror release", Origin: "quay.io/openshift-release-dev/ocp-release", Machines: []string{"mirror"}, Consumer: "Registry/mirror"},
	})
	vendor, public := 0, 0
	for _, row := range rows[2:] {
		if row.Kind != "registry blob host" {
			t.Fatalf("unexpected companion: %+v", row)
		}
		switch row.Class {
		case "vendor delivery":
			vendor++
			if !slices.Equal(row.Machines, []string{"node"}) || row.Consumer != "OpenShiftCluster/vendor" {
				t.Errorf("the vendor edge merged a public pull: %+v", row)
			}
		case acquisition.ClassPublic:
			public++
			if !slices.Equal(row.Machines, []string{"mirror"}) || row.Consumer != "Registry/mirror" {
				t.Errorf("the public edge merged a vendor pull: %+v", row)
			}
		default:
			t.Errorf("unexpected shared-host class: %+v", row)
		}
		if acquisition.OriginClass(row.Origin) != acquisition.ClassPublic {
			t.Errorf("the exception reclassified shared host %s globally", row.Origin)
		}
	}
	if vendor != len(quayDeliveryHosts) || public != len(quayDeliveryHosts) {
		t.Fatalf("got %d vendor and %d public delivery rows, want %d of each", vendor, public, len(quayDeliveryHosts))
	}
}

func TestCatalogSelectionReplacesOnlyTheHelmFillOrigin(t *testing.T) {
	field := func(name string, content api.Value) api.FieldValue { return api.FieldValue{Name: name, Value: content} }
	helm := api.MapValue(field("catalogArtifact", api.StringValue("approved-helm")))
	artifacts := api.MapValue(field("toolSources", api.MapValue(field("helm", helm))))
	spec := api.MapValue(field("mirror", api.MapValue(field("artifacts", artifacts))))
	catalog := api.NewCatalog([]api.Object{api.NewObject(api.Environment, "lab", api.Value{}, spec)})
	clients := []MirroredClient{
		{Tool: "helm 4.3.1", Location: "https://mirror.example.test/helm", Source: "https://publisher.example.test/helm", SHA256: "helm-pin"},
		{Tool: "govc 1.0.0", Source: "https://publisher.example.test/govc"},
	}
	other := clients[1]
	applyCatalogSources(catalog, clients)
	if client := clients[0]; client.CatalogArtifact != "approved-helm" || client.Source != "" ||
		client.Location != "https://mirror.example.test/helm" || client.SHA256 != "helm-pin" {
		t.Fatalf("catalog selection produced %+v", client)
	}
	if clients[1] != other {
		t.Fatalf("catalog selection changed another tool: %+v", clients[1])
	}
}
