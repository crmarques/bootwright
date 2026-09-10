package environment

import (
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

type Attachment struct{ ClusterRef, ExportRef string }
type SelectionIssue struct {
	Object api.Object
	Issue  api.Issue
}
type Selection struct {
	Catalog                                            api.Catalog
	ExcludedContainerClusters, ExcludedStorageClusters []string
	Problems                                           []SelectionIssue
}

// Select computes the declarative cluster closure. Attachments come from the
// add-on semantic owner; matching arbitrary native strings grants no edge.
func Select(catalog api.Catalog, attachments []Attachment) Selection {
	result := Selection{Catalog: catalog, ExcludedContainerClusters: []string{}, ExcludedStorageClusters: []string{}, Problems: []SelectionIssue{}}
	envs := catalog.OfKind(api.Environment)
	if len(envs) != 1 {
		return result
	}
	env := envs[0]
	if !env.Spec().Has("containerClusters") && !env.Spec().Has("storageClusters") {
		return result
	}
	retained := map[string]bool{}
	fullStorage := map[string]bool{}
	keep := func(kind api.Kind, name string) {
		if name != "" {
			retained[string(kind)+"/"+name] = true
		}
	}
	for _, kind := range []api.Kind{api.Environment, api.Entitlement, api.MachineImage, api.MachineInstallProfile, api.NetworkConfig, api.Proxy, api.DNSServer, api.NTPServer, api.ArtifactServer, api.Registry, api.LoadBalancer, api.CustomPlaybook, api.Secret} {
		for _, object := range catalog.OfKind(kind) {
			keep(kind, object.Name())
		}
	}
	keep(api.Machine, env.Spec().Get("controller", "machineRef").Text())
	for _, entry := range []struct {
		field string
		kind  api.Kind
	}{{"containerClusters", api.ContainerCluster}, {"storageClusters", api.StorageCluster}} {
		values := env.Spec().Get(entry.field)
		if !values.Present() {
			for _, o := range catalog.OfKind(entry.kind) {
				keep(o.Kind(), o.Name())
				if o.Kind() == api.StorageCluster {
					fullStorage[o.Name()] = true
				}
			}
			continue
		}
		if values.Len() == 0 {
			result.Problems = append(result.Problems, SelectionIssue{env, api.Issue{Code: "api.value", Field: "$.spec." + entry.field, Message: "cluster root selection must not be empty"}})
		}
		for _, value := range values.Items() {
			name := value.Text()
			if _, ok := catalog.Find(entry.kind, name); !ok {
				result.Problems = append(result.Problems, SelectionIssue{env, api.Issue{Code: "api.reference", Field: "$.spec." + entry.field, Message: "cluster selection must resolve to one root of its selected kind"}})
				matches := 0
				for _, object := range catalog.OfKind(entry.kind) {
					if object.Name() == name {
						matches++
					}
				}
				if matches == 0 {
					continue
				}
			}
			keep(entry.kind, name)
			if entry.kind == api.StorageCluster {
				fullStorage[name] = true
			}
		}
	}
	// Each pass adds identities only; at most the finite catalog can be added.
	for {
		before := len(retained)
		for _, object := range catalog.Objects() {
			spec := object.Spec()
			kind := object.Kind()
			if kind == api.ClusterAddonBinding && retained[string(api.ContainerCluster)+"/"+spec.Get("clusterRef").Text()] {
				keep(kind, object.Name())
			}
			if storageChild(kind) && fullStorage[spec.Get("clusterRef").Text()] {
				keep(kind, object.Name())
			}
			if !retained[object.Identity()] {
				continue
			}
			switch kind {
			case api.Proxy, api.DNSServer, api.NTPServer, api.ArtifactServer, api.Registry, api.LoadBalancer:
				if spec.Get("management").Text() == "managed" {
					keep(api.Machine, spec.Get("machineRef").Text())
				}
			case api.Machine:
				keep(api.InfraProvider, spec.Get("substrate", "providerRef").Text())
			case api.InfraProvider:
				keep(api.Machine, spec.Get("libvirt", "machineRef").Text())
			case api.ContainerCluster:
				for _, node := range spec.Get("nodes").Items() {
					keep(api.Machine, node.Get("machineRef").Text())
				}
			case api.StorageCluster:
				for _, node := range spec.Get("ceph", "topology", "nodes").Items() {
					keep(api.Machine, node.Get("machineRef").Text())
				}
			case api.ClusterAddonBinding, api.ClusterAddonProfile:
				for _, name := range spec.Get("profileRefs").Strings() {
					keep(api.ClusterAddonProfile, name)
				}
				for _, name := range spec.Get("addonRefs").Strings() {
					keep(api.ClusterAddon, name)
				}
			case api.StorageExport, api.StoragePool, api.StorageFilesystem, api.StorageObjectGateway, api.StorageNFSExport, api.StoragePlacementPolicy:
				keep(api.StorageCluster, spec.Get("clusterRef").Text())
				retainReferences(spec, api.Schema(kind), keep)
				if kind == api.StorageExport {
					for _, nfs := range catalog.OfKind(api.StorageNFSExport) {
						if nfs.Spec().Get("clusterRef").Text() == spec.Get("clusterRef").Text() {
							keep(api.StorageNFSExport, nfs.Name())
						}
					}
				}
			}
		}
		for _, attachment := range attachments {
			if retained[string(api.ContainerCluster)+"/"+attachment.ClusterRef] {
				keep(api.StorageExport, attachment.ExportRef)
			}
			if export, ok := catalog.Find(api.StorageExport, attachment.ExportRef); ok && fullStorage[export.Spec().Get("clusterRef").Text()] {
				keep(api.ContainerCluster, attachment.ClusterRef)
			}
		}
		if before == len(retained) {
			break
		}
	}
	objects := []api.Object{}
	for _, object := range catalog.Objects() {
		if retained[object.Identity()] {
			objects = append(objects, object)
			continue
		}
		if object.Kind() == api.ContainerCluster {
			result.ExcludedContainerClusters = append(result.ExcludedContainerClusters, object.Name())
		}
		if object.Kind() == api.StorageCluster {
			result.ExcludedStorageClusters = append(result.ExcludedStorageClusters, object.Name())
		}
		if object.Kind() == api.ContainerCluster || object.Kind() == api.StorageCluster {
			result.Problems = append(result.Problems, SelectionIssue{object, api.Issue{Code: "api.deferred", Field: "$.metadata.name", Message: "cluster root is excluded by the Environment selection", Remediation: "include the cluster in the matching Environment root selection"}})
		}
	}
	result.Catalog = CanonicalCatalog(objects)
	slices.Sort(result.ExcludedContainerClusters)
	slices.Sort(result.ExcludedStorageClusters)
	return result
}

func storageChild(kind api.Kind) bool {
	return strings.HasPrefix(string(kind), "Storage") && kind != api.StorageCluster
}

func retainReferences(value api.Value, shape *api.Shape, keep func(api.Kind, string)) {
	if shape == nil {
		return
	}
	for _, alternative := range shape.Alternatives {
		if alternative.Type == value.Type() {
			shape = alternative
			break
		}
	}
	if value.Type() == api.String {
		for _, kind := range shape.Reference {
			if storageChild(kind) {
				keep(kind, value.Text())
			}
		}
	}
	if value.Type() == api.Mapping {
		for _, field := range value.Fields() {
			if f, ok := shape.Field(field.Name); ok {
				retainReferences(field.Value, f.Shape, keep)
			}
		}
	}
	if value.Type() == api.Sequence {
		for _, item := range value.Items() {
			retainReferences(item, shape.Element, keep)
		}
	}
}
