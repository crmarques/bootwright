package inspection

import (
	"context"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
)

type Service struct {
	graph     Graph
	ceph      CephEvidence
	installs  InstallEvidence
	keys      HostKeys
	selection CurrentSelection
	custody   Custody
}

func New(graph Graph, ceph CephEvidence, installs InstallEvidence, keys HostKeys, selection CurrentSelection, custody Custody) Service {
	return Service{graph: graph, ceph: ceph, installs: installs, keys: keys, selection: selection, custody: custody}
}

type ListClustersRequest struct {
	ContextName string
}

type ClusterInfoRequest struct {
	ContextName string
	Names       []string
	Secrets     bool
}

func (s Service) ListClusters(ctx context.Context, request ListClustersRequest) (*ListResult, error) {
	selected, catalog, err := s.read(ctx, request.ContextName)
	if err != nil {
		return nil, err
	}
	containers, storages := selectClusters(catalog, nil)
	result := &ListResult{Context: selected, Clusters: make([]ClusterRef, 0, len(containers)+len(storages))}
	for _, cluster := range containers {
		result.Clusters = append(result.Clusters, ClusterRef{Name: cluster.Name(), Kind: KindContainerCluster})
	}
	for _, cluster := range storages {
		result.Clusters = append(result.Clusters, ClusterRef{Name: cluster.Name(), Kind: KindStorageCluster})
	}
	slices.SortStableFunc(result.Clusters, func(a, b ClusterRef) int { return strings.Compare(a.Name, b.Name) })
	return result, nil
}

func (s Service) ClusterInfo(ctx context.Context, request ClusterInfoRequest) (*InfoResult, error) {
	selected, catalog, err := s.read(ctx, request.ContextName)
	if err != nil {
		return nil, err
	}
	names := canonicalNames(request.Names)
	containers, storages := selectClusters(catalog, names)
	if err := unknownClusters(selected, names, containers, storages); err != nil {
		return nil, err
	}
	if request.Secrets {
		if err := secretsTarget(selected, names, containers, storages); err != nil {
			return nil, err
		}
		if s.custody == nil {
			return nil, availability.ErrNotImplemented
		}
	}
	result := &InfoResult{Context: selected, Clusters: make([]ContainerEntry, 0, len(containers)), Storage: make([]StorageEntry, 0, len(storages))}
	for _, cluster := range containers {
		entry, err := s.containerEntry(ctx, selected, cluster)
		if err != nil {
			return nil, err
		}
		result.Clusters = append(result.Clusters, entry)
	}
	resources, err := s.resources(ctx, selected, catalog, storages)
	if err != nil {
		return nil, err
	}
	for _, cluster := range storages {
		entry, err := s.storageEntry(ctx, selected, catalog, cluster, resources[cluster.Name()])
		if err != nil {
			return nil, err
		}
		result.Storage = append(result.Storage, entry)
	}
	if request.Secrets {
		if err := s.revealDashboard(ctx, selected, result.Storage[0]); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (s Service) revealDashboard(ctx context.Context, selected string, entry StorageEntry) error {
	name := entry.Name
	custodied, found, err := s.custody.DashboardPassword(ctx, selected, name)
	if err != nil {
		custodied.Material.Clear()
		return err
	}
	if !found {
		return failure("access.unavailable",
			"this context holds no dashboard administrator password for StorageCluster "+name+"; an apply keeps it once it enables the dashboard",
			"bootwright apply --context "+selected+" --stage clusters")
	}
	dashboard := entry.Ceph.Dashboard
	if dashboard == nil {
		dashboard = &Dashboard{Status: DashboardUnproved, User: "admin", URLs: []DashboardURL{},
			Reason: "the cluster's evidence does not prove the dashboard: " + entry.Ceph.Reason}
		entry.Ceph.Dashboard = dashboard
	}
	if custodied.Unproved && dashboard.Status == DashboardReady {
		dashboard.Status = DashboardUnproved
		dashboard.Reason = "this context kept the password from an observation that never proved the dashboard reachable"
	}
	dashboard.Password, dashboard.Revealed = custodied.Material, true
	dashboard.Warn = dashboard.Status != DashboardReady
	return nil
}

func (s Service) read(ctx context.Context, contextName string) (string, api.Catalog, error) {
	if err := ctx.Err(); err != nil {
		return "", api.Catalog{}, err
	}
	if s.graph == nil || s.ceph == nil || s.installs == nil || s.keys == nil {
		return "", api.Catalog{}, availability.ErrNotImplemented
	}
	selected, err := machine.SelectedContext(ctx, s.selection, contextName)
	if err != nil {
		return "", api.Catalog{}, err
	}
	catalog, err := s.graph.Effective(ctx, selected)
	if err != nil {
		return "", api.Catalog{}, err
	}
	return selected, catalog, nil
}

func canonicalNames(requested []string) []string {
	var names []string
	for _, name := range requested {
		if name = strings.TrimSpace(name); name != "" {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

func unknownClusters(selected string, names []string, containers, storages []api.Object) error {
	var unknown []string
	for _, name := range names {
		found := func(cluster api.Object) bool { return cluster.Name() == name }
		if !slices.ContainsFunc(containers, found) && !slices.ContainsFunc(storages, found) {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	subject, remedy := "no ContainerCluster or StorageCluster named ", "name a cluster the context selects; "
	if len(unknown) > 1 {
		subject, remedy = "no ContainerClusters or StorageClusters named ", "name clusters the context selects; "
	}
	return failure("access.target", "the context "+selected+" selects "+subject+strings.Join(unknown, ", "),
		remedy+"bootwright cluster info --context "+selected+" lists them")
}

func selectClusters(catalog api.Catalog, names []string) ([]api.Object, []api.Object) {
	pick := func(kind api.Kind) []api.Object {
		var picked []api.Object
		for _, object := range catalog.OfKind(kind) {
			if len(names) == 0 || slices.Contains(names, object.Name()) {
				picked = append(picked, object)
			}
		}
		slices.SortStableFunc(picked, func(a, b api.Object) int { return strings.Compare(a.Name(), b.Name()) })
		return picked
	}
	return pick(api.ContainerCluster), pick(api.StorageCluster)
}

func external(cluster api.Object) bool {
	management := cluster.Spec().Get("management").Text()
	return management != "" && management != ManagementManaged
}

func secretsTarget(selected string, names []string, containers, storages []api.Object) error {
	discovery := "bootwright cluster info --context " + selected
	if len(names) > 1 {
		return failure("cli.usage", "--secrets reveals one cluster's values; name one cluster", "name one cluster with --name <cluster>; "+discovery+" lists the clusters this context selects")
	}
	var name string
	if len(names) == 1 {
		name = names[0]
	}
	switch {
	case name == "":
		return failure("access.target", "bootwright cluster info --secrets requires one cluster name", discovery+" lists the clusters this context selects")
	case len(containers) != 0:
		return failure("cluster.not-applicable",
			"bootwright cluster info --secrets does not apply to "+name+", an OpenShift or OKD ContainerCluster; its administrator kubeconfig is exported by bootwright cluster kubeconfig",
			"bootwright cluster kubeconfig --context "+selected+" --name "+name+" exports it; "+discovery+" lists the clusters this context selects and their kinds")
	case len(storages) != 0 && external(storages[0]):
		return failure("cluster.not-applicable",
			"bootwright cluster info --secrets does not apply to "+name+", an external Ceph StorageCluster; it applies to managed Ceph StorageClusters",
			discovery+" lists the clusters this context selects and their kinds")
	case len(storages) != 0:
		return nil
	}
	return failure("access.target", "the context "+selected+" selects no ContainerCluster or StorageCluster named "+name, discovery+" lists them")
}

func (s Service) containerEntry(ctx context.Context, selected string, cluster api.Object) (ContainerEntry, error) {
	installed, err := s.installs.Installed(ctx, selected, cluster.Name())
	if err != nil {
		return ContainerEntry{}, err
	}
	sessions := "sessions on an OpenShift or OKD ContainerCluster are not yet implemented; the cluster is reached through bootwright cluster kubeconfig"
	handoff := "this handoff is not yet implemented; the cluster is reached through bootwright cluster kubeconfig"
	kubeconfig := AccessCommand{Command: CommandKubeconfig, Status: StatusReady}
	if !installed {
		kubeconfig.Status = StatusUnavailable
		kubeconfig.Reason = "this context holds no administrator kubeconfig for " + cluster.Name() +
			" until an apply proves its installation complete; run bootwright apply --context " + selected
	}
	return ContainerEntry{Name: cluster.Name(), Kind: KindContainerCluster, AccessCommands: []AccessCommand{
		{Command: CommandExec, Status: StatusNotImplemented, Reason: sessions},
		{Command: CommandKeyring, Status: StatusNotApplicable, Reason: managedCephOnly},
		kubeconfig,
		{Command: CommandKubectl, Status: StatusNotImplemented, Reason: handoff},
		{Command: CommandOC, Status: StatusNotImplemented, Reason: handoff},
		{Command: CommandRsh, Status: StatusNotImplemented, Reason: sessions},
	}}, nil
}

func (s Service) resources(ctx context.Context, selected string, catalog api.Catalog, storages []api.Object) (map[string][]Resource, error) {
	if !slices.ContainsFunc(storages, func(cluster api.Object) bool { return !external(cluster) }) {
		return nil, nil
	}
	return s.ceph.Resources(ctx, selected, catalog)
}

func (s Service) storageEntry(ctx context.Context, selected string, catalog api.Catalog, cluster api.Object, resources []Resource) (StorageEntry, error) {
	containerOnly := "applies to OpenShift and OKD ContainerClusters only"
	if external(cluster) {
		roster := "the external StorageCluster " + cluster.Name() + " declares no node roster; sessions apply to managed Ceph StorageClusters"
		return StorageEntry{Name: cluster.Name(), Kind: KindStorageCluster, AccessCommands: []AccessCommand{
			{Command: CommandExec, Status: StatusNotApplicable, Reason: roster},
			{Command: CommandKeyring, Status: StatusNotApplicable, Reason: managedCephOnly},
			{Command: CommandKubeconfig, Status: StatusNotApplicable, Reason: containerOnly},
			{Command: CommandKubectl, Status: StatusNotApplicable, Reason: containerOnly},
			{Command: CommandOC, Status: StatusNotApplicable, Reason: containerOnly},
			{Command: CommandRsh, Status: StatusNotApplicable, Reason: roster},
		}}, nil
	}
	session, err := s.sessionAccess(ctx, selected, cluster)
	if err != nil {
		return StorageEntry{}, err
	}
	exec, rsh := session, session
	exec.Command, rsh.Command = CommandExec, CommandRsh
	ceph, err := s.cephEntry(ctx, selected, catalog, cluster, resources)
	if err != nil {
		return StorageEntry{}, err
	}
	return StorageEntry{Name: cluster.Name(), Kind: KindStorageCluster, Ceph: &ceph, AccessCommands: []AccessCommand{
		exec,
		keyringAccess(selected, cluster.Name(), resources),
		{Command: CommandKubeconfig, Status: StatusNotApplicable, Reason: containerOnly},
		{Command: CommandKubectl, Status: StatusNotApplicable, Reason: containerOnly},
		{Command: CommandOC, Status: StatusNotApplicable, Reason: containerOnly},
		rsh,
	}}, nil
}

const managedCephOnly = "applies to managed Ceph StorageClusters only"

func keyringAccess(selected, cluster string, resources []Resource) AccessCommand {
	access := AccessCommand{Command: CommandKeyring, Status: StatusUnavailable, Reason: "the StorageCluster " + cluster + " declares no StorageClient"}
	for _, resource := range resources {
		if resource.Kind != string(api.StorageClient) {
			continue
		}
		if resource.Status == ResourceDone {
			return AccessCommand{Command: CommandKeyring, Status: StatusReady}
		}
		access.Reason = "no StorageClient of " + cluster + " has a completed apply; run bootwright apply --context " + selected + " --stage clusters"
	}
	return access
}

func (s Service) sessionAccess(ctx context.Context, selected string, cluster api.Object) (AccessCommand, error) {
	nodes := cluster.Spec().Get("ceph", "topology", "nodes").Items()
	if len(nodes) == 0 {
		return AccessCommand{Status: StatusUnavailable, Reason: "the StorageCluster " + cluster.Name() + " declares no node to open a session on"}, nil
	}
	slices.SortStableFunc(nodes, func(a, b api.Value) int {
		return strings.Compare(a.Get("name").Text(), b.Get("name").Text())
	})
	node, host := nodes[0].Get("name").Text(), nodes[0].Get("machineRef").Text()
	proved, err := s.keys.HostKeyProved(ctx, selected, host)
	if err != nil {
		return AccessCommand{}, err
	}
	if !proved {
		return AccessCommand{Status: StatusUnavailable, Node: node, Reason: "the Machine " + host + ", which runs node " + node +
			", has no proved host key; bootwright apply --context " + selected + " --stage machines records it"}, nil
	}
	return AccessCommand{Status: StatusReady, Node: node}, nil
}

func (s Service) cephEntry(ctx context.Context, selected string, catalog api.Catalog, cluster api.Object, resources []Resource) (ManagedCeph, error) {
	ceph := cluster.Spec().Get("ceph")
	entry := ManagedCeph{
		Management: ManagementManaged, Release: ceph.Get("release").Text(),
		AdminNode: ceph.Get("cephadm", "bootstrap", "node").Text(), Resources: []Resource{}, Endpoints: []Endpoint{},
	}
	if resources != nil {
		entry.Resources = resources
	}
	proof, err := s.ceph.ManagedCeph(ctx, selected, catalog, cluster)
	if err != nil {
		return ManagedCeph{}, err
	}
	reason := proof.Reason
	if reason == "" && (proof.FSID == "" || entry.AdminNode == "") {
		reason = "the cluster's evidence or its declared bootstrap node does not name the administrator's host"
	}
	if reason != "" {
		entry.Status, entry.Reason = StatusUnavailable, reason
		entry.Remedy = "bootwright apply --context " + selected + " --stage clusters"
		return entry, nil
	}
	root := "/var/lib/bootwright/ceph/" + proof.FSID
	entry.Status, entry.FSID = StatusReady, proof.FSID
	entry.Image = &proof.Image
	entry.Specializations = proof.Specializations
	entry.Monitors = proof.Monitors
	dashboard := proof.Dashboard
	entry.Dashboard = &dashboard
	if proof.Endpoints != nil {
		entry.Endpoints = proof.Endpoints
	}
	entry.CephCommand = "bootwright cluster exec --context " + selected + " --name " + cluster.Name() + " --node " + entry.AdminNode +
		" -- sudo -n ceph --conf " + root + "/ceph.conf --keyring " + root + "/ceph.client.admin.keyring"
	return entry, nil
}

func failure(code, message, remediation string) error {
	return diagnostics.NewFailureWithRemediation(code, message, "", remediation)
}
