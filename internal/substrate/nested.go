package substrate

import (
	"fmt"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

const MaxHostedNamespaces = 16

const HostAccount = "bootwright-kubevirt"

type HostedNamespace struct {
	Name      string
	Providers []string
	Labels    map[string]string
	Conflict  string
}

func HostedProviders(catalog api.Catalog, cluster string) []api.Object {
	var found []api.Object
	for _, provider := range ProvidersOn(catalog, ArmKubeVirt) {
		if cluster != "" && HostCluster(provider) == cluster {
			found = append(found, provider)
		}
	}
	return found
}

func HostedNamespaces(catalog api.Catalog, cluster string) []HostedNamespace {
	host, declared := catalog.Find(api.OpenShiftCluster, cluster)
	var namespaces []HostedNamespace
	for _, provider := range HostedProviders(catalog, cluster) {
		name := provider.Spec().Get(ArmKubeVirt, "namespace").Text()
		index := slices.IndexFunc(namespaces, func(namespace HostedNamespace) bool { return namespace.Name == name })
		if index < 0 {
			namespaces = append(namespaces, HostedNamespace{Name: name, Labels: map[string]string{}})
			index = len(namespaces) - 1
		}
		namespace := &namespaces[index]
		namespace.Providers = append(namespace.Providers, provider.Name())
		for _, segment := range provider.Spec().Get("segments").Items() {
			network, ok := hostNetwork(host, segment)
			if !declared || !ok {
				continue
			}
			for _, label := range network.Get("namespaceSelector", "matchLabels").Fields() {
				value := label.Value.Text()
				if held, found := namespace.Labels[label.Name]; found && held != value && namespace.Conflict == "" {
					namespace.Conflict = fmt.Sprintf("label %s is %q for one secondary network of %s and %q for %s", label.Name, held, cluster, value, network.Get("name").Text())
				}
				if _, found := namespace.Labels[label.Name]; !found {
					namespace.Labels[label.Name] = value
				}
			}
		}
	}
	slices.SortFunc(namespaces, func(x, y HostedNamespace) int { return strings.Compare(x.Name, y.Name) })
	return namespaces
}

func SegmentNetworkKind(provider api.Object, segment string) string {
	kind := ""
	for _, declared := range provider.Spec().Get("segments").Items() {
		if declared.Get("name").Text() == segment {
			kind = declared.Get(ArmKubeVirt, "networkRef", "kind").Text()
		}
	}
	return kind
}

func PersistentTPM(profile api.Value) bool {
	return profile.Has("tpm") && (!profile.Has("tpm", "persistent") || profile.Get("tpm", "persistent").Bool())
}

func HostNetworkNamed(host api.Object, segment api.Value) bool {
	_, ok := hostNetwork(host, segment)
	return ok
}

func HostClusterObject(catalog api.Catalog, provider api.Object) (api.Object, bool) {
	return catalog.Find(api.OpenShiftCluster, HostCluster(provider))
}

func HostNetworkMTU(host api.Object, segment api.Value) (string, int64, bool) {
	network, ok := hostNetwork(host, segment)
	if !ok {
		return "", 0, false
	}
	mtu, declared := network.Get("mtu").Int64()
	if !declared {
		mtu = 1500
	}
	return network.Get("name").Text(), mtu, true
}

func hostNetwork(host api.Object, segment api.Value) (api.Value, bool) {
	ref := segment.Get(ArmKubeVirt, "networkRef")
	if ref.Get("apiGroup").Text() != "k8s.ovn.org" || ref.Get("kind").Text() != "ClusterUserDefinedNetwork" {
		return api.Value{}, false
	}
	return findNamed(host.Spec().Get("networking", "secondaryNetworks"), "name", ref.Get("name").Text())
}

func validateHostedProvider(o api.Object, c api.Catalog) []api.Issue {
	cluster := HostCluster(o)
	if cluster == "" {
		return nil
	}
	host, found := c.Find(api.OpenShiftCluster, cluster)
	if !found {
		return nil
	}
	var issues []api.Issue
	namespaces := HostedNamespaces(c, cluster)
	name := o.Spec().Get(ArmKubeVirt, "namespace").Text()
	for _, namespace := range namespaces {
		if namespace.Name == name && namespace.Conflict != "" {
			issues = add(issues, issue("$.spec.kubevirt.namespace", "namespace "+name+" cannot carry the selector of every secondary network its providers attach to: "+namespace.Conflict))
		}
	}
	for index, segment := range o.Spec().Get("segments").Items() {
		if network, ok := hostNetwork(host, segment); ok && network.Get("ipam", "mode").Text() == "Enabled" {
			name := network.Get("name").Text()
			path := fmt.Sprintf("$.spec.segments[%d].kubevirt.networkRef", index)
			issues = add(issues, api.Issue{Code: "api.invariant", Field: path,
				Message:     "a VirtualMachine attaches only to a host network that leaves IP address management to the guest, and secondary network " + name + " of " + host.Identity() + " sets ipam.mode Enabled, so its OVN port security would enforce the addresses it assigns and not the static install address",
				Remediation: "set ipam.mode to Disabled on secondary network " + name + " of " + host.Identity() + ", or attach " + o.Identity() + " to a network that does"})
		}
	}
	if len(namespaces) > MaxHostedNamespaces {
		issues = add(issues, issue("$.spec.kubevirt.hostClusterRef", fmt.Sprintf("%s hosts at most %d KubeVirt provider namespaces, and its providers declare %d", host.Identity(), MaxHostedNamespaces, len(namespaces))))
	}
	return issues
}
