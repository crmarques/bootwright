package environment

import (
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
)

type Attachment struct{ ClusterRef, ConsumerRef string }
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
	if len(authoredLists(env)) == 0 {
		return result
	}
	closure := &retention{catalog: catalog, retained: map[string]bool{}, full: map[string]bool{}, attached: map[string]string{}, members: Members(members)}
	result.Problems = append(result.Problems, closure.selectRoots(catalog, env)...)
	closure.expand(catalog, attachments)
	excludedRoots := []string{}
	for _, object := range catalog.Objects() {
		if api.IsClusterKind(object.Kind()) && !closure.retained[object.Identity()] {
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
		if category, root := api.CategoryOf(object.Kind()); root {
			if category == api.ContainerClusterCategory {
				result.ExcludedContainerClusters = append(result.ExcludedContainerClusters, object.Name())
			} else {
				result.ExcludedStorageClusters = append(result.ExcludedStorageClusters, object.Name())
			}
			result.Excluded[object.Identity()] = "add " + object.Name() + " to Environment spec." + category.SelectionField()
			result.Problems = append(result.Problems, excludedRoot(object, "spec."+category.SelectionField()))
			continue
		}
		switch object.Kind() {
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
	for _, family := range api.Families() {
		if field := family.Category.SelectionField(); selectsClusters(env, field) {
			fields = append(fields, "spec."+field)
		}
	}
	return fields
}

type retention struct {
	catalog  api.Catalog
	retained map[string]bool
	full     map[string]bool
	attached map[string]string
	listed   map[string]bool
	members  func(api.Object) (Membership, bool)
}

func (r *retention) keep(kind api.Kind, name string) {
	if name != "" {
		r.retained[string(kind)+"/"+name] = true
	}
}

func (r *retention) keepFull(kind api.Kind, name string) {
	if name != "" {
		r.keep(kind, name)
		r.full[string(kind)+"/"+name] = true
	}
}

func (r *retention) keepCluster(kind api.Kind, name string) {
	if api.IsClusterKind(kind) {
		r.keepFull(kind, name)
	} else {
		r.keep(kind, name)
	}
}

func (r *retention) clusterKind(category api.Category, name string) api.Kind {
	if object, ok := r.catalog.FindInCategory(category, name); ok {
		return object.Kind()
	}
	return api.CategoryKinds(category)[0]
}

func (r *retention) clusterIdentity(category api.Category, name string) string {
	return string(r.clusterKind(category, name)) + "/" + name
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
	for _, family := range api.Families() {
		field := family.Category.SelectionField()
		objects := catalog.OfCategory(family.Category)
		if !selectsClusters(env, field) {
			for _, o := range objects {
				r.listed[o.Identity()] = true
				r.keepFull(o.Kind(), o.Name())
			}
			continue
		}
		problems = append(problems, r.selectNamedRoots(env, field, family, objects)...)
	}
	r.keepStandaloneMachines(catalog)
	return problems
}

func (r *retention) keepStandaloneMachines(catalog api.Catalog) {
	roots := []string{}
	for _, kind := range api.ClusterKinds() {
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

func (r *retention) selectNamedRoots(env api.Object, field string, family api.Family, objects []api.Object) []SelectionIssue {
	var problems []SelectionIssue
	declared := make(map[string]api.Kind, len(objects))
	for _, object := range objects {
		declared[object.Name()] = object.Kind()
	}
	remediation := ""
	seen := map[string]bool{}
	for i, value := range env.Spec().Get(field).Items() {
		name := value.Text()
		if seen[name] {
			continue
		}
		seen[name] = true
		kind, found := declared[name]
		if !found {
			if remediation == "" {
				remediation = clusterChoices(family, objects)
			}
			problems = append(problems, SelectionIssue{Object: env, Issue: unresolvedCluster(family, "$.spec."+field+"["+strconv.Itoa(i)+"]", name, remediation), Target: string(family.Kinds[0]) + "/" + name})
			continue
		}
		r.listed[string(kind)+"/"+name] = true
		r.keepFull(kind, name)
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
		category, _ := api.CategoryOf(api.Kind(kind))
		field := category.SelectionField()
		problems = append(problems, SelectionIssue{Object: env, Target: identity, Issue: api.Issue{Code: "api.invariant", Field: "$.spec." + field,
			Message:     identity + " is attached to CephConsumer/" + r.attached[identity] + " of the selection, but spec." + field + " omits it",
			Remediation: "add " + name + " to spec." + field + ", or remove the attachment"}})
	}
	return problems
}

func selectsClusters(env api.Object, field string) bool {
	values := env.Spec().Get(field)
	return values.Present() && values.Len() > 0
}

const listedClusterNames = 16

func familyKinds(family api.Family) string {
	names := make([]string, 0, len(family.Kinds))
	for _, kind := range family.Kinds {
		names = append(names, string(kind))
	}
	return strings.Join(names, " or ")
}

func unresolvedCluster(family api.Family, field, name, remediation string) api.Issue {
	message := "cluster selection entry is not a cluster name"
	if api.ValidLexical("name", name) {
		message = "no " + familyKinds(family) + " named " + name + " is declared"
	}
	return api.Issue{Code: "api.reference", Field: field, Message: message, Remediation: remediation}
}

func clusterChoices(family api.Family, objects []api.Object) string {
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
	return "declare the " + familyKinds(family) + " or remove the entry"
}

func (r *retention) expand(catalog api.Catalog, attachments []Attachment) {
	// Each pass adds identities only; at most the finite catalog can be added.
	for {
		before := len(r.retained) + len(r.full)
		for _, object := range catalog.Objects() {
			r.retainDependencies(catalog, object)
		}
		for _, attachment := range attachments {
			consumer := r.clusterIdentity(api.ContainerClusterCategory, attachment.ClusterRef)
			attached, ok := consumerObject(catalog, attachment.ConsumerRef)
			if r.retained[consumer] && ok {
				r.keep(attached.Kind(), attachment.ConsumerRef)
			}
			if !ok {
				continue
			}
			storage := r.parentOf(attached.Kind(), attached.Spec())
			if r.retained[consumer] {
				r.reach(storage, attachment.ConsumerRef)
			}
			if r.full[storage] {
				r.reach(consumer, attachment.ConsumerRef)
				r.keepFull(r.clusterKind(api.ContainerClusterCategory, attachment.ClusterRef), attachment.ClusterRef)
			}
		}
		if before == len(r.retained)+len(r.full) {
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
	parent := r.parentOf(kind, spec)
	if parent != "" && r.full[parent] {
		r.keep(kind, object.Name())
	}
	if peer, category := peerCluster(kind, spec); peer != "" && r.full[r.clusterIdentity(category, peer)] {
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
	if parent != "" {
		root, name, _ := strings.Cut(parent, "/")
		r.keep(api.Kind(root), name)
		retainReferences(spec, api.Schema(kind), func(referenced api.Kind) bool { return childOf(referenced) == api.Kind(root) }, r.keep)
	}
	switch kind {
	case api.Machine:
		r.keep(api.InfraProvider, spec.Get("substrate", "providerRef").Text())
	case api.InfraProvider:
		retainReferences(spec, api.Schema(kind), func(referenced api.Kind) bool {
			category, root := api.CategoryOf(referenced)
			return referenced == api.Machine || root && category == api.ContainerClusterCategory
		}, r.keepCluster)
	case api.ClusterAddonBinding, api.ClusterAddonProfile:
		for _, name := range spec.Get("profileRefs").Strings() {
			r.keep(api.ClusterAddonProfile, name)
		}
		for _, name := range spec.Get("addonRefs").Strings() {
			r.keep(api.ClusterAddon, name)
		}
	}
	if peer, category := peerCluster(kind, spec); peer != "" {
		for _, end := range []string{spec.Get("clusterRef").Text(), peer} {
			r.keepFull(r.clusterKind(category, end), end)
		}
	}
	for _, share := range consumerShares(kind) {
		for _, object := range catalog.OfKind(share) {
			if object.Spec().Get("clusterRef").Text() == spec.Get("clusterRef").Text() {
				r.keep(share, object.Name())
			}
		}
	}
}

func consumerObject(catalog api.Catalog, name string) (api.Object, bool) {
	for _, family := range api.Families() {
		if kind, ok := family.Roles[api.ConsumerRole]; ok {
			if object, found := catalog.Find(kind, name); found {
				return object, true
			}
		}
	}
	return api.Object{}, false
}

var consumerShareKinds = sync.OnceValue(func() map[api.Kind][]api.Kind {
	shares := map[api.Kind][]api.Kind{}
	for _, family := range api.Families() {
		consumer, consumes := family.Roles[api.ConsumerRole]
		share, shared := family.Roles[api.ConsumerShareRole]
		if consumes && shared {
			shares[consumer] = append(shares[consumer], share)
		}
	}
	return shares
})

func consumerShares(kind api.Kind) []api.Kind { return consumerShareKinds()[kind] }

var peerCategories = sync.OnceValue(func() map[api.Kind]api.Category {
	peers := map[api.Kind]api.Category{}
	for _, kind := range api.Kinds() {
		shape := api.Schema(kind)
		if shape == nil {
			continue
		}
		if field, ok := shape.Field("peerClusterRef"); ok && len(field.Shape.Reference) != 0 {
			if category, root := api.CategoryOf(field.Shape.Reference[0]); root {
				peers[kind] = category
			}
		}
	}
	return peers
})

func peerCluster(kind api.Kind, spec api.Value) (string, api.Category) {
	category, ok := peerCategories()[kind]
	if !ok {
		return "", ""
	}
	return spec.Get("peerClusterRef").Text(), category
}

func childOf(kind api.Kind) api.Kind {
	for _, root := range api.ClusterKinds() {
		if slices.Contains(api.Children(root), kind) {
			return root
		}
	}
	return ""
}

func (r *retention) parentOf(kind api.Kind, spec api.Value) string {
	root := childOf(kind)
	if root == "" {
		return ""
	}
	category, _ := api.CategoryOf(root)
	return r.clusterIdentity(category, spec.Get("clusterRef").Text())
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
