package v1alpha1

import (
	"slices"
	"sync"
)

type MachineView struct {
	Object            Object
	ProvidedDeclared  bool
	Provided          bool
	InstallProfileRef string
	ProviderRef       string
}

type CephClusterView struct {
	Object    Object
	Nodes     []CephNodeView
	Services  []CephServiceView
	Stretched bool
}

type CephNodeView struct {
	Name       string
	MachineRef string
}

type CephServiceView struct {
	Type string
	Spec Value
}

func machineViewOf(o Object) MachineView {
	spec := o.Spec()
	provided := spec.Get("os", "provided")
	return MachineView{
		Object:            o,
		ProvidedDeclared:  provided.Type() == Boolean,
		Provided:          provided.Bool(),
		InstallProfileRef: spec.Get("os", "installProfileRef").Text(),
		ProviderRef:       spec.Get("substrate", "providerRef").Text(),
	}
}

func (v MachineView) Installed() bool { return v.ProvidedDeclared && !v.Provided }

func cephClusterViewOf(o Object) CephClusterView {
	spec := o.Spec()
	view := CephClusterView{Object: o, Stretched: spec.Has("topology", "stretch")}
	for _, node := range spec.Get("topology", "nodes").Items() {
		view.Nodes = append(view.Nodes, CephNodeView{Name: node.Get("name").Text(), MachineRef: node.Get("machineRef").Text()})
	}
	for _, service := range spec.Get("services").Items() {
		view.Services = append(view.Services, CephServiceView{Type: service.Get("service_type").Text(), Spec: service})
	}
	return view
}

type catalogViews struct {
	once        sync.Once
	machineList []MachineView
	machines    map[string]MachineView
	clusterList []CephClusterView
	clusters    map[string]CephClusterView
}

func (c Catalog) views() *catalogViews {
	build := func(views *catalogViews) {
		views.machines, views.clusters = map[string]MachineView{}, map[string]CephClusterView{}
		for _, o := range c.byKind[Machine] {
			view := machineViewOf(o)
			views.machineList = append(views.machineList, view)
			if _, unique := c.Find(Machine, o.Name()); unique {
				views.machines[o.Name()] = view
			}
		}
		for _, o := range c.byKind[CephCluster] {
			view := cephClusterViewOf(o)
			views.clusterList = append(views.clusterList, view)
			if _, unique := c.Find(CephCluster, o.Name()); unique {
				views.clusters[o.Name()] = view
			}
		}
	}
	if c.memo == nil {
		views := &catalogViews{}
		build(views)
		return views
	}
	c.memo.once.Do(func() { build(c.memo) })
	return c.memo
}

func (c Catalog) Machine(name string) (MachineView, bool) {
	view, ok := c.views().machines[name]
	return view, ok
}

func (c Catalog) Machines() []MachineView { return slices.Clone(c.views().machineList) }

func (c Catalog) CephClusters() []CephClusterView {
	views := make([]CephClusterView, 0, len(c.views().clusterList))
	for _, view := range c.views().clusterList {
		views = append(views, view.detached())
	}
	return views
}

func (c Catalog) CephCluster(name string) (CephClusterView, bool) {
	view, ok := c.views().clusters[name]
	return view.detached(), ok
}

func (v CephClusterView) detached() CephClusterView {
	v.Nodes, v.Services = slices.Clone(v.Nodes), slices.Clone(v.Services)
	return v
}
