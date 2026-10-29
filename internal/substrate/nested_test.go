package substrate

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func cudn(name string) api.Value {
	return m("name", name, "kubevirt", m("networkRef", m("apiGroup", "k8s.ovn.org", "kind", "ClusterUserDefinedNetwork", "name", name)))
}

func hostedOn(name, namespace string, networks ...string) api.Object {
	attachments := []api.Value{}
	for _, network := range networks {
		attachments = append(attachments, cudn(network))
	}
	return obj(api.InfraProvider, name, m("kubevirt", m("hostClusterRef", "host", "namespace", namespace), "networkAttachments", list(attachments...)))
}

func hostOf(networks ...api.Value) api.Object {
	return obj(api.OpenShiftCluster, "host", m("networking", m("secondaryNetworks", list(networks...))))
}

func selecting(name string, labels ...any) api.Value {
	return m("name", name, "namespaceSelector", m("matchLabels", m(labels...)))
}

func TestAHostedNamespaceCarriesTheUnionOfItsNetworksSelectors(t *testing.T) {
	host := hostOf(selecting("machine", "tier", "hub"), selecting("storage", "net", "storage", "tier", "hub"), selecting("edge", "tier", "edge"))
	catalog := api.NewCatalog([]api.Object{host, hostedOn("a", "guests", "machine"), hostedOn("b", "guests", "storage"), hostedOn("c", "others", "machine")})
	want := []HostedNamespace{
		{Name: "guests", Providers: []string{"a", "b"}, Labels: map[string]string{"tier": "hub", "net": "storage"}},
		{Name: "others", Providers: []string{"c"}, Labels: map[string]string{"tier": "hub"}},
	}
	if got := HostedNamespaces(catalog, "host"); !reflect.DeepEqual(got, want) {
		t.Fatalf("namespaces %+v", got)
	}
	for _, provider := range []string{"a", "b"} {
		object, _ := catalog.Find(api.InfraProvider, provider)
		if issues := Validate(object, catalog); len(issues) != 0 {
			t.Fatalf("%s: %+v", provider, issues)
		}
	}
}

func TestNetworksThatLabelOneNamespaceDifferentlyRefuseEveryProviderInIt(t *testing.T) {
	host := hostOf(selecting("machine", "tier", "hub"), selecting("edge", "tier", "edge"))
	catalog := api.NewCatalog([]api.Object{host, hostedOn("a", "guests", "machine"), hostedOn("b", "guests", "edge"), hostedOn("c", "others", "edge")})
	namespaces := HostedNamespaces(catalog, "host")
	if !strings.Contains(namespaces[0].Conflict, `label tier is "hub"`) || !strings.Contains(namespaces[0].Conflict, `"edge" for edge`) || namespaces[1].Conflict != "" {
		t.Fatalf("namespaces %+v", namespaces)
	}
	for provider, refused := range map[string]bool{"a": true, "b": true, "c": false} {
		object, _ := catalog.Find(api.InfraProvider, provider)
		issues := Validate(object, catalog)
		if refused != (len(issues) == 1 && issues[0].Field == "$.spec.kubevirt.namespace" && strings.Contains(issues[0].Message, "namespace guests")) || !refused && len(issues) != 0 {
			t.Fatalf("%s: %+v", provider, issues)
		}
	}
}

func TestAHostOwnsAtMostItsBoundOfHostedNamespaces(t *testing.T) {
	objects := []api.Object{hostOf(selecting("machine", "tier", "hub"))}
	for index := 0; index <= MaxHostedNamespaces; index++ {
		objects = append(objects, hostedOn(fmt.Sprintf("p-%02d", index), fmt.Sprintf("guests-%02d", index), "machine"))
	}
	catalog := api.NewCatalog(objects)
	issues := Validate(objects[1], catalog)
	if len(issues) != 1 || issues[0].Field != "$.spec.kubevirt.hostClusterRef" {
		t.Fatalf("issues %+v", issues)
	}
	if issues := Validate(objects[1], api.NewCatalog(objects[:len(objects)-1])); len(issues) != 0 {
		t.Fatalf("at the bound: %+v", issues)
	}
}

func TestAHostedTargetNamesItsHostInPlaceOfACredential(t *testing.T) {
	provider := obj(api.InfraProvider, "kv", m("kubevirt", m("hostClusterRef", "host", "namespace", "guests")))
	if !IsRealized(provider) || HostCluster(provider) != "host" || !NamesHostCluster(provider) {
		t.Fatal("a hosted KubeVirt provider is not realized")
	}
	if HostCluster(obj(api.InfraProvider, "kv", m("kubevirt", m("kubeconfigRef", "k", "namespace", "guests")))) != "" {
		t.Fatal("an external provider names a host cluster")
	}
}

// A consumer reads a hosted provider's facts through these accessors, never
// the endpoint grammar or the provider's arm block itself.
func TestHostedAccessorsReadTheProvidersOwnShape(t *testing.T) {
	endpoint := KubeVirtEndpoint("guests", "vm-1")
	if got := KubeVirtNamespace(endpoint); got != "guests" {
		t.Fatalf("namespace of %q = %q", endpoint, got)
	}
	if got := KubeVirtNamespace(""); got != "" {
		t.Fatalf("namespace of an empty endpoint = %q", got)
	}
	provider := hostedOn("a", "guests", "machine")
	if got := AttachmentNetworkKind(provider, "machine"); got != "ClusterUserDefinedNetwork" {
		t.Fatalf("kind of machine = %q", got)
	}
	if got := AttachmentNetworkKind(provider, "missing"); got != "" {
		t.Fatalf("kind of an undeclared attachment = %q", got)
	}
	for _, tc := range []struct {
		profile api.Value
		want    bool
	}{
		{m("name", "p"), false},
		{m("name", "p", "tpm", m()), true},
		{m("name", "p", "tpm", m("persistent", true)), true},
		{m("name", "p", "tpm", m("persistent", false)), false},
	} {
		if got := PersistentTPM(tc.profile); got != tc.want {
			t.Errorf("PersistentTPM(%v) = %t, want %t", tc.profile, got, tc.want)
		}
	}
}
