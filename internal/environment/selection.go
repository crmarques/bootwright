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
	closure := &retention{retained: map[string]bool{}, fullStorage: map[string]bool{}}
	result.Problems = append(result.Problems, closure.selectRoots(catalog, env)...)
	closure.expand(catalog, attachments)
	objects := []api.Object{}
	for _, object := range catalog.Objects() {
		if closure.retained[object.Identity()] {
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
			result.Problems = append(result.Problems, SelectionIssue{object, api.Issue{Code: "api.selection", Field: "$.metadata.name", Message: "cluster root is excluded by the Environment selection", Remediation: "include the cluster in the matching Environment root selection"}})
		}
	}
	result.Catalog = CanonicalCatalog(objects)
	slices.Sort(result.ExcludedContainerClusters)
	slices.Sort(result.ExcludedStorageClusters)
	return result
}

type retention struct {
	retained    map[string]bool
	fullStorage map[string]bool
}

func (r *retention) keep(kind api.Kind, name string) {
	if name != "" {
		r.retained[string(kind)+"/"+name] = true
	}
}

func (r *retention) selectRoots(catalog api.Catalog, env api.Object) []SelectionIssue {
	var problems []SelectionIssue
	for _, kind := range []api.Kind{api.Environment, api.Entitlement, api.MachineImage, api.MachineInstallProfile, api.NetworkConfig, api.Proxy, api.DNSServer, api.NTPServer, api.ArtifactServer, api.Registry, api.LoadBalancer, api.CustomPlaybook, api.Secret} {
		for _, object := range catalog.OfKind(kind) {
			r.keep(kind, object.Name())
		}
	}
	r.keep(api.Machine, env.Spec().Get("controller", "machineRef").Text())
	for _, entry := range []struct {
		field string
		kind  api.Kind
	}{{"containerClusters", api.ContainerCluster}, {"storageClusters", api.StorageCluster}} {
		values := env.Spec().Get(entry.field)
		if !values.Present() {
			for _, o := range catalog.OfKind(entry.kind) {
				r.keep(o.Kind(), o.Name())
				if o.Kind() == api.StorageCluster {
					r.fullStorage[o.Name()] = true
				}
			}
			continue
		}
		if values.Len() == 0 {
			problems = append(problems, SelectionIssue{env, api.Issue{Code: "api.value", Field: "$.spec." + entry.field, Message: "cluster root selection must not be empty"}})
		}
		for _, value := range values.Items() {
			name := value.Text()
			if _, ok := catalog.Find(entry.kind, name); !ok {
				problems = append(problems, SelectionIssue{env, api.Issue{Code: "api.reference", Field: "$.spec." + entry.field, Message: "cluster selection must resolve to one root of its selected kind"}})
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
			r.keep(entry.kind, name)
			if entry.kind == api.StorageCluster {
				r.fullStorage[name] = true
			}
		}
	}
	return problems
}

func (r *retention) expand(catalog api.Catalog, attachments []Attachment) {
	// Each pass adds identities only; at most the finite catalog can be added.
	for {
		before := len(r.retained)
		for _, object := range catalog.Objects() {
			r.retainDependencies(catalog, object)
		}
		for _, attachment := range attachments {
			if r.retained[string(api.ContainerCluster)+"/"+attachment.ClusterRef] {
				r.keep(api.StorageExport, attachment.ExportRef)
			}
			if export, ok := catalog.Find(api.StorageExport, attachment.ExportRef); ok && r.fullStorage[export.Spec().Get("clusterRef").Text()] {
				r.keep(api.ContainerCluster, attachment.ClusterRef)
			}
		}
		if before == len(r.retained) {
			break
		}
	}
}

func (r *retention) retainDependencies(catalog api.Catalog, object api.Object) {
	spec := object.Spec()
	kind := object.Kind()
	if kind == api.ClusterAddonBinding && r.retained[string(api.ContainerCluster)+"/"+spec.Get("clusterRef").Text()] {
		r.keep(kind, object.Name())
	}
	if storageChild(kind) && r.fullStorage[spec.Get("clusterRef").Text()] {
		r.keep(kind, object.Name())
	}
	if !r.retained[object.Identity()] {
		return
	}
	switch kind {
	case api.Proxy, api.DNSServer, api.NTPServer, api.ArtifactServer, api.Registry, api.LoadBalancer:
		if spec.Get("management").Text() == "managed" {
			r.keep(api.Machine, spec.Get("machineRef").Text())
		}
	case api.Machine:
		r.keep(api.InfraProvider, spec.Get("substrate", "providerRef").Text())
	case api.InfraProvider:
		r.keep(api.Machine, spec.Get("libvirt", "machineRef").Text())
	case api.ContainerCluster:
		for _, node := range spec.Get("nodes").Items() {
			r.keep(api.Machine, node.Get("machineRef").Text())
		}
	case api.StorageCluster:
		for _, node := range spec.Get("ceph", "topology", "nodes").Items() {
			r.keep(api.Machine, node.Get("machineRef").Text())
		}
	case api.ClusterAddonBinding, api.ClusterAddonProfile:
		for _, name := range spec.Get("profileRefs").Strings() {
			r.keep(api.ClusterAddonProfile, name)
		}
		for _, name := range spec.Get("addonRefs").Strings() {
			r.keep(api.ClusterAddon, name)
		}
	case api.StorageExport, api.StoragePool, api.StorageFilesystem, api.StorageObjectGateway, api.StorageNFSExport, api.StoragePlacementPolicy:
		r.keep(api.StorageCluster, spec.Get("clusterRef").Text())
		retainReferences(spec, api.Schema(kind), r.keep)
		if kind == api.StorageExport {
			for _, nfs := range catalog.OfKind(api.StorageNFSExport) {
				if nfs.Spec().Get("clusterRef").Text() == spec.Get("clusterRef").Text() {
					r.keep(api.StorageNFSExport, nfs.Name())
				}
			}
		}
	}
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
