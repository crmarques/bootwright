package environment

import (
	"maps"
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
)

type Attachment struct{ ClusterRef, ExportRef string }
type SelectionIssue struct {
	Object api.Object
	Issue  api.Issue
	Target string
}
type Selection struct {
	Catalog                                            api.Catalog
	ExcludedContainerClusters, ExcludedStorageClusters []string
	Problems                                           []SelectionIssue
	Excluded                                           map[string]string
}

// Select computes the declarative cluster closure. Attachments come from the
// add-on semantic owner; matching arbitrary native strings grants no edge.
func Select(catalog api.Catalog, attachments []Attachment, members func(api.Object) (Membership, bool)) Selection {
	result := Selection{Catalog: catalog, ExcludedContainerClusters: []string{}, ExcludedStorageClusters: []string{}, Problems: []SelectionIssue{}, Excluded: map[string]string{}}
	envs := catalog.OfKind(api.Environment)
	if len(envs) != 1 {
		return result
	}
	env := envs[0]
	if !selectsClusters(env, "containerClusters") && !selectsClusters(env, "storageClusters") {
		return result
	}
	closure := &retention{retained: map[string]bool{}, fullStorage: map[string]bool{}, attached: map[string]string{}, members: Members(members)}
	result.Problems = append(result.Problems, closure.selectRoots(catalog, env)...)
	closure.expand(catalog, attachments)
	excludedRoots := []string{}
	for _, object := range catalog.Objects() {
		if (object.Kind() == api.ContainerCluster || object.Kind() == api.StorageCluster) && !closure.retained[object.Identity()] {
			excludedRoots = append(excludedRoots, object.Identity())
		}
	}
	accounted := closure.hostClosure(catalog, closure.nodeMachines(catalog, excludedRoots))
	fields := authoredLists(env)
	objects := []api.Object{}
	for _, object := range catalog.Objects() {
		if closure.retained[object.Identity()] {
			objects = append(objects, object)
			continue
		}
		result.Excluded[object.Identity()] = "select the cluster that retains " + object.Identity() + " in Environment " + strings.Join(fields, " or ")
		switch object.Kind() {
		case api.ContainerCluster:
			result.ExcludedContainerClusters = append(result.ExcludedContainerClusters, object.Name())
			result.Excluded[object.Identity()] = "add " + object.Name() + " to Environment spec.containerClusters"
			result.Problems = append(result.Problems, excludedRoot(object, "spec.containerClusters"))
		case api.StorageCluster:
			result.ExcludedStorageClusters = append(result.ExcludedStorageClusters, object.Name())
			result.Excluded[object.Identity()] = "add " + object.Name() + " to Environment spec.storageClusters"
			result.Problems = append(result.Problems, excludedRoot(object, "spec.storageClusters"))
		case api.Machine, api.InfraProvider:
			if !accounted[object.Identity()] {
				result.Excluded[object.Identity()] = unretainedRemedy
				result.Problems = append(result.Problems, SelectionIssue{Object: object, Issue: api.Issue{Code: "api.selection", Field: "$.metadata.name",
					Message:     object.Identity() + " is excluded: no selection field retains it and no excluded cluster root uses it",
					Remediation: unretainedRemedy}})
			}
		}
	}
	result.Problems = append(result.Problems, closure.omittedRoots(env)...)
	result.Catalog = CanonicalCatalog(objects)
	slices.Sort(result.ExcludedContainerClusters)
	slices.Sort(result.ExcludedStorageClusters)
	return result
}

const unretainedRemedy = "reference it from a selected object, make it a Bootwright-installed Machine, or remove the declaration"

func excludedRoot(object api.Object, field string) SelectionIssue {
	return SelectionIssue{Object: object, Issue: api.Issue{Code: "api.selection", Field: "$.metadata.name", Message: "cluster root is excluded by the Environment selection", Remediation: "add " + object.Name() + " to Environment " + field + " to include it"}}
}

func authoredLists(env api.Object) []string {
	fields := []string{}
	for _, field := range []string{"containerClusters", "storageClusters"} {
		if selectsClusters(env, field) {
			fields = append(fields, "spec."+field)
		}
	}
	return fields
}

type retention struct {
	retained    map[string]bool
	fullStorage map[string]bool
	attached    map[string]string
	listed      map[string]bool
	members     func(api.Object) (Membership, bool)
}

func (r *retention) keep(kind api.Kind, name string) {
	if name != "" {
		r.retained[string(kind)+"/"+name] = true
	}
}

func (r *retention) keepFullStorage(name string) {
	if name != "" {
		r.keep(api.StorageCluster, name)
		r.fullStorage[name] = true
	}
}

func (r *retention) selectRoots(catalog api.Catalog, env api.Object) []SelectionIssue {
	var problems []SelectionIssue
	r.listed = map[string]bool{}
	for _, kind := range rootKinds() {
		for _, object := range catalog.OfKind(kind) {
			r.keep(kind, object.Name())
		}
	}
	r.keep(api.Machine, env.Spec().Get("controller", "machineRef").Text())
	for _, entry := range []struct {
		field string
		kind  api.Kind
	}{{"containerClusters", api.ContainerCluster}, {"storageClusters", api.StorageCluster}} {
		objects := catalog.OfKind(entry.kind)
		if !selectsClusters(env, entry.field) {
			for _, o := range objects {
				r.listed[o.Identity()] = true
				r.keep(o.Kind(), o.Name())
				if o.Kind() == api.StorageCluster {
					r.fullStorage[o.Name()] = true
				}
			}
			continue
		}
		problems = append(problems, r.selectNamedRoots(env, entry.field, entry.kind, objects)...)
	}
	r.keepStandaloneMachines(catalog)
	return problems
}

func (r *retention) keepStandaloneMachines(catalog api.Catalog) {
	roots := []string{}
	for _, kind := range []api.Kind{api.ContainerCluster, api.StorageCluster} {
		for _, object := range catalog.OfKind(kind) {
			roots = append(roots, object.Identity())
		}
	}
	used := r.hostClosure(catalog, r.nodeMachines(catalog, roots))
	for _, machine := range catalog.OfKind(api.Machine) {
		os := machine.Spec().Get("os")
		if !os.Get("provided").Bool() && os.Get("installProfileRef").Text() != "" && !used[machine.Identity()] {
			r.keep(api.Machine, machine.Name())
		}
	}
}

func (r *retention) nodeMachines(catalog api.Catalog, roots []string) []string {
	machines := []string{}
	for _, identity := range roots {
		kind, name, _ := strings.Cut(identity, "/")
		object, found := catalog.Find(api.Kind(kind), name)
		if !found {
			continue
		}
		if membership, bound := r.members(object); bound {
			for _, node := range membership.Nodes {
				machines = append(machines, node.Machine)
			}
		}
	}
	return machines
}

func (r *retention) hostClosure(catalog api.Catalog, machines []string) map[string]bool {
	reached := map[string]bool{}
	queue := slices.Clone(machines)
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if name == "" || reached[string(api.Machine)+"/"+name] {
			continue
		}
		reached[string(api.Machine)+"/"+name] = true
		machine, found := catalog.Find(api.Machine, name)
		if !found {
			continue
		}
		provider := machine.Spec().Get("substrate", "providerRef").Text()
		if provider == "" || reached[string(api.InfraProvider)+"/"+provider] {
			continue
		}
		reached[string(api.InfraProvider)+"/"+provider] = true
		if object, found := catalog.Find(api.InfraProvider, provider); found {
			retainReferences(object.Spec(), api.Schema(api.InfraProvider), isMachine, func(_ api.Kind, host string) { queue = append(queue, host) })
		}
	}
	return reached
}

func isMachine(kind api.Kind) bool { return kind == api.Machine }

func rootKinds() []api.Kind {
	return []api.Kind{api.Environment, api.Entitlement, api.MachineImage, api.MachineInstallProfile, api.NetworkConfig, api.Proxy, api.DNSServer, api.NTPServer, api.ArtifactServer, api.Registry, api.LoadBalancer, api.CustomPlaybook, api.Secret}
}

func (r *retention) selectNamedRoots(env api.Object, field string, kind api.Kind, objects []api.Object) []SelectionIssue {
	var problems []SelectionIssue
	declared := make(map[string]bool, len(objects))
	for _, object := range objects {
		declared[object.Name()] = true
	}
	remediation := ""
	seen := map[string]bool{}
	for i, value := range env.Spec().Get(field).Items() {
		name := value.Text()
		if seen[name] {
			continue
		}
		seen[name] = true
		if !declared[name] {
			if remediation == "" {
				remediation = clusterChoices(kind, objects)
			}
			problems = append(problems, SelectionIssue{Object: env, Issue: unresolvedCluster(kind, "$.spec."+field+"["+strconv.Itoa(i)+"]", name, remediation), Target: string(kind) + "/" + name})
			continue
		}
		r.listed[string(kind)+"/"+name] = true
		r.keep(kind, name)
		if kind == api.StorageCluster {
			r.fullStorage[name] = true
		}
	}
	return problems
}

func (r *retention) omittedRoots(env api.Object) []SelectionIssue {
	var problems []SelectionIssue
	for _, identity := range slices.Sorted(maps.Keys(r.attached)) {
		if r.listed[identity] || !r.retained[identity] {
			continue
		}
		kind, name, _ := strings.Cut(identity, "/")
		field := "storageClusters"
		if api.Kind(kind) == api.ContainerCluster {
			field = "containerClusters"
		}
		problems = append(problems, SelectionIssue{Object: env, Target: identity, Issue: api.Issue{Code: "api.invariant", Field: "$.spec." + field,
			Message:     identity + " is attached to StorageExport/" + r.attached[identity] + " of the selection, but spec." + field + " omits it",
			Remediation: "add " + name + " to spec." + field + ", or remove the attachment"}})
	}
	return problems
}

func selectsClusters(env api.Object, field string) bool {
	values := env.Spec().Get(field)
	return values.Present() && values.Len() > 0
}

const listedClusterNames = 16

func unresolvedCluster(kind api.Kind, field, name, remediation string) api.Issue {
	message := "cluster selection entry is not a cluster name"
	if api.ValidLexical("name", name) {
		message = "no " + string(kind) + " named " + name + " is declared"
	}
	return api.Issue{Code: "api.reference", Field: field, Message: message, Remediation: remediation}
}

func clusterChoices(kind api.Kind, objects []api.Object) string {
	declared := make([]string, 0, len(objects))
	for _, object := range objects {
		if api.ValidLexical("name", object.Name()) {
			declared = append(declared, object.Name())
		}
	}
	slices.Sort(declared)
	declared = slices.Compact(declared)
	switch {
	case len(declared) > listedClusterNames:
		return "select one of: " + strings.Join(declared[:listedClusterNames], ", ") + ", and " + strconv.Itoa(len(declared)-listedClusterNames) + " more"
	case len(declared) > 0:
		return "select one of: " + strings.Join(declared, ", ")
	}
	return "declare the " + string(kind) + " or remove the entry"
}

func (r *retention) expand(catalog api.Catalog, attachments []Attachment) {
	// Each pass adds identities only; at most the finite catalog can be added.
	for {
		before := len(r.retained) + len(r.fullStorage)
		for _, object := range catalog.Objects() {
			r.retainDependencies(catalog, object)
		}
		for _, attachment := range attachments {
			if r.retained[string(api.ContainerCluster)+"/"+attachment.ClusterRef] {
				r.keep(api.StorageExport, attachment.ExportRef)
			}
			export, ok := catalog.Find(api.StorageExport, attachment.ExportRef)
			if !ok {
				continue
			}
			storage := export.Spec().Get("clusterRef").Text()
			if r.retained[string(api.ContainerCluster)+"/"+attachment.ClusterRef] {
				r.reach(string(api.StorageCluster)+"/"+storage, attachment.ExportRef)
			}
			if r.fullStorage[storage] {
				r.reach(string(api.ContainerCluster)+"/"+attachment.ClusterRef, attachment.ExportRef)
				r.keep(api.ContainerCluster, attachment.ClusterRef)
			}
		}
		if before == len(r.retained)+len(r.fullStorage) {
			break
		}
	}
}

func (r *retention) reach(identity, export string) {
	if _, recorded := r.attached[identity]; !recorded && !r.listed[identity] {
		r.attached[identity] = export
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
	if kind == api.StorageReplication && r.fullStorage[spec.Get("peerClusterRef").Text()] {
		r.keep(kind, object.Name())
	}
	if !r.retained[object.Identity()] {
		return
	}
	if membership, bound := r.members(object); bound {
		for _, node := range membership.Nodes {
			r.keep(api.Machine, node.Machine)
		}
	}
	if infrastructureservices.IsService(kind) && spec.Get("management").Text() == "managed" {
		r.keep(api.Machine, spec.Get("machineRef").Text())
	}
	if storageChild(kind) {
		r.keep(api.StorageCluster, spec.Get("clusterRef").Text())
		retainReferences(spec, api.Schema(kind), storageChild, r.keep)
	}
	switch kind {
	case api.Machine:
		r.keep(api.InfraProvider, spec.Get("substrate", "providerRef").Text())
	case api.InfraProvider:
		retainReferences(spec, api.Schema(kind), func(referenced api.Kind) bool { return referenced == api.Machine || referenced == api.ContainerCluster }, r.keep)
	case api.StorageReplication:
		r.keepFullStorage(spec.Get("clusterRef").Text())
		r.keepFullStorage(spec.Get("peerClusterRef").Text())
	case api.ClusterAddonBinding, api.ClusterAddonProfile:
		for _, name := range spec.Get("profileRefs").Strings() {
			r.keep(api.ClusterAddonProfile, name)
		}
		for _, name := range spec.Get("addonRefs").Strings() {
			r.keep(api.ClusterAddon, name)
		}
	case api.StorageExport:
		for _, nfs := range catalog.OfKind(api.StorageNFSExport) {
			if nfs.Spec().Get("clusterRef").Text() == spec.Get("clusterRef").Text() {
				r.keep(api.StorageNFSExport, nfs.Name())
			}
		}
	}
}

func storageChild(kind api.Kind) bool {
	return slices.Contains(api.Children(api.StorageCluster), kind)
}

func retainReferences(value api.Value, shape *api.Shape, wanted func(api.Kind) bool, keep func(api.Kind, string)) {
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
			if wanted(kind) {
				keep(kind, value.Text())
			}
		}
	}
	if value.Type() == api.Mapping {
		for _, field := range value.Fields() {
			if f, ok := shape.Field(field.Name); ok {
				retainReferences(field.Value, f.Shape, wanted, keep)
			}
		}
	}
	if value.Type() == api.Sequence {
		for _, item := range value.Items() {
			retainReferences(item, shape.Element, wanted, keep)
		}
	}
}
