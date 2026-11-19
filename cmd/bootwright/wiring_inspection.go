package main

import (
	"context"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/containercluster"
	containeraccess "github.com/crmarques/bootwright/internal/containercluster/access"
	"github.com/crmarques/bootwright/internal/containercluster/agentinstall"
	"github.com/crmarques/bootwright/internal/environment/inspection"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/machine/inventory"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets/custody"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
	"github.com/crmarques/bootwright/internal/storage/ceph"
	"github.com/crmarques/bootwright/internal/trust"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

type evidenceReader interface {
	Evidence(ctx context.Context, contextName, kind, object string) ([]lifecycle.BlockEvidence, error)
}

type cephReader interface {
	evidenceReader
	StorageResources(ctx context.Context, contextName string) (*lifecycle.StorageResourcesResult, error)
}

type hostKeyReader interface {
	HostKey(ctx context.Context, contextName, name string) (machine.HostKeyEvidence, bool, error)
}

type trustRecords interface {
	ReadHostKeys(ctx context.Context, contextName string) ([]byte, error)
}

func wireInspection(state inventory.EffectiveState, reconciler lifecycle.Service, binder *custody.Service, selection contexts.SelectionStore, records trustRecords) inspection.Service {
	return inspection.New(effectiveGraph{state: nodeAccessState{state: state}}, cephInspection{reconciler: reconciler}, installProofs{reconciler: reconciler},
		hostKeyProofs{keys: machineHostKeys{reconciler: reconciler}, records: records}, currentSelection(selection), inspectionCustody{reader: producedReader{binder: binder}, administrator: reconciler.AdminCredentials(), frozen: reconciler, current: binder}).WithMembers(clusterMembers()).WithAdministrator(clusterAdministrator).WithClients(clusterClientMetadata{reconciler: reconciler, custody: binder}).WithContainerDetails(containerDetails).WithConsoleCustody(inspectionCustody{reader: producedReader{binder: binder}, administrator: reconciler.AdminCredentials(), frozen: reconciler, current: binder})
}

type producedEntries interface {
	ReadProduced(ctx context.Context, contextName, block, name string) (containeraccess.Custodied, bool, error)
}

type storageResourcesReader interface {
	StorageResources(ctx context.Context, contextName string) (*lifecycle.StorageResourcesResult, error)
}

type currentSecrets interface {
	ReadCurrent(ctx context.Context, request custody.ReadCurrentRequest) ([]secretstore.BoundMaterial, error)
}

type inspectionCustody struct {
	reader        producedEntries
	administrator administratorPasswordReader
	frozen        storageResourcesReader
	current       currentSecrets
}

func (d inspectionCustody) MonitoringCredentials(ctx context.Context, contextName, cluster, service string) (inspection.Custodied, bool, error) {
	read, found, err := d.reader.ReadProduced(ctx, contextName, ceph.GatewayBlock(cluster), ceph.MonitoringOutput)
	if err != nil || !found {
		return inspection.Custodied{Material: read.Material, Unproved: read.Unproved}, found, err
	}
	defer read.Material.Clear()
	login, ok := ceph.MonitoringCredentialsOf(read.Material, service)
	if !ok {
		return inspection.Custodied{}, true, inspection.ErrMonitoringMaterial
	}
	return inspection.Custodied{Material: login, Unproved: read.Unproved}, true, nil
}

func (d inspectionCustody) GrafanaPassword(ctx context.Context, contextName, cluster string) (inspection.Custodied, bool, error) {
	if d.frozen == nil || d.current == nil {
		return inspection.Custodied{}, false, availability.ErrNotImplemented
	}
	resources, err := d.frozen.StorageResources(ctx, contextName)
	if err != nil || resources == nil {
		return inspection.Custodied{}, false, err
	}
	secret, bound := ceph.GrafanaPasswordSecret(cluster, resources.Frozen)
	if !bound {
		return inspection.Custodied{}, false, nil
	}
	read, err := d.current.ReadCurrent(ctx, custody.ReadCurrentRequest{ContextName: contextName, Names: []string{secret}})
	if err != nil {
		return inspection.Custodied{}, false, err
	}
	if len(read) != 1 {
		for _, item := range read {
			item.Material.Clear()
		}
		return inspection.Custodied{}, false, inspection.ErrMonitoringMaterial
	}
	return inspection.Custodied{Material: read[0].Material}, true, nil
}

func (d inspectionCustody) ObjectUserCredentials(ctx context.Context, contextName, user string) (inspection.Custodied, bool, error) {
	read, found, err := d.reader.ReadProduced(ctx, contextName, ceph.ObjectUserBlockID(user), ceph.ObjectUserOutput)
	if err != nil || !found {
		return inspection.Custodied{Material: read.Material, Unproved: read.Unproved}, found, err
	}
	defer read.Material.Clear()
	material, ok := ceph.ObjectUserCredentialsOf(read.Material, user)
	if !ok {
		return inspection.Custodied{}, true, inspection.ErrObjectUserMaterial
	}
	return inspection.Custodied{Material: material, Unproved: read.Unproved}, true, nil
}

func (d inspectionCustody) DashboardPassword(ctx context.Context, contextName, cluster string) (inspection.Custodied, bool, error) {
	if d.administrator == nil {
		return inspection.Custodied{}, false, availability.ErrNotImplemented
	}
	read, found, err := d.administrator.AdministratorPassword(ctx, contextName, cluster, ceph.DashboardBlock(cluster), ceph.DashboardOutput)
	return inspection.Custodied{Material: read.Material, Unproved: read.Unproved}, found, err
}

func (d inspectionCustody) ClientKeyring(ctx context.Context, contextName, client string) (inspection.Custodied, bool, error) {
	read, found, err := d.reader.ReadProduced(ctx, contextName, ceph.ClientBlockID(client), ceph.ClientOutput)
	if err != nil || !found {
		return inspection.Custodied{Material: read.Material, Unproved: read.Unproved}, found, err
	}
	defer read.Material.Clear()
	keyring, ok := ceph.ClientKeyringOf(read.Material, client)
	if !ok {
		return inspection.Custodied{}, true, inspection.ErrClientMaterial
	}
	return inspection.Custodied{Material: keyring, Unproved: read.Unproved}, true, nil
}

type cephInspection struct{ reconciler cephReader }

func (c cephInspection) ObjectUserProved(ctx context.Context, contextName string, catalog api.Catalog, user api.Object) (bool, error) {
	proofs, err := c.reconciler.Evidence(ctx, contextName, string(api.CephObjectUser), user.Name())
	if err != nil {
		return false, err
	}
	frozen, err := c.reconciler.StorageResources(ctx, contextName)
	if err != nil {
		return false, err
	}
	if frozen == nil {
		return false, nil
	}
	return ceph.ObjectUserProvedAgainst(catalog, user, proofs, frozen.Frozen), nil
}

func (c cephInspection) Resources(ctx context.Context, contextName string, catalog api.Catalog) (map[string][]inspection.Resource, error) {
	found, err := c.reconciler.StorageResources(ctx, contextName)
	if err != nil {
		return nil, err
	}
	rows := map[string][]inspection.Resource{}
	for _, cluster := range found.Clusters {
		declared, ok := catalog.Find(api.CephCluster, cluster.Name)
		if management := declared.Spec().Get("management").Text(); cluster.Kind != string(api.CephCluster) || !ok || management != "" && management != inspection.ManagementManaged {
			continue
		}
		resources := make([]inspection.Resource, 0, len(cluster.Resources))
		for _, r := range cluster.Resources {
			resources = append(resources, inspection.Resource{Kind: r.Kind, Name: r.Name, Status: string(r.Status),
				Scope: ceph.ResourceScope(catalog, api.Kind(r.Kind), r.Name, found.Frozen)})
		}
		rows[cluster.Name] = resources
	}
	return rows, nil
}

func clusterAdministrator(cluster api.Object) string {
	if node := containercluster.SessionNode(cluster); node != "" {
		return node
	}
	return ceph.ClusterAdministration(cluster).Node
}

func (cephInspection) ObjectUserGateway(user api.Object) string { return ceph.ObjectUserGateway(user) }

func (c cephInspection) ManagedStorage(ctx context.Context, contextName string, catalog api.Catalog, cluster api.Object) (inspection.StorageProof, error) {
	administration := ceph.ClusterAdministration(cluster)
	proofs, err := c.reconciler.Evidence(ctx, contextName, string(api.CephCluster), cluster.Name())
	if err != nil {
		return inspection.StorageProof{}, err
	}
	resources, err := c.reconciler.StorageResources(ctx, contextName)
	if err != nil {
		return inspection.StorageProof{}, err
	}
	var frozen []reconciliation.Block
	if resources != nil {
		frozen = resources.Frozen
	}
	found, reason := ceph.Inspect(catalog, cluster, frozen, proofs)
	if reason != "" {
		return inspection.StorageProof{Release: administration.Release, AdminNode: administration.Node, Reason: reason}, nil
	}
	proof := inspection.StorageProof{
		Release: administration.Release, AdminNode: administration.Node, AdminCommand: administration.Command(contextName, cluster.Name(), found.FSID),
		FSID:            found.FSID,
		Image:           inspection.Image{Reference: found.Image.Reference, Digest: found.Image.Digest, Platform: found.Image.Platform},
		Specializations: found.Specializations,
	}
	for _, monitor := range found.Monitors {
		proof.Monitors = append(proof.Monitors, inspection.Monitor{Host: monitor.Host, FQDN: monitor.FQDN, Address: monitor.Address})
	}
	d := found.Dashboard
	proof.Dashboard = inspection.Dashboard{Status: d.Status, Reason: d.Reason, User: d.User, URLs: []inspection.DashboardURL{}, CertificateSHA256: d.CertificateSHA256}
	for _, url := range d.URLs {
		proof.Dashboard.URLs = append(proof.Dashboard.URLs, inspection.DashboardURL{Host: url.Host, URL: url.URL, Role: url.Role})
	}
	for _, access := range found.Monitoring {
		proof.Monitoring = append(proof.Monitoring, inspection.MonitoringAccess{Service: access.Service, URL: access.URL})
	}
	proof.Endpoints, err = c.endpoints(ctx, contextName, cluster.Name(), frozen, proofs)
	if err != nil {
		return inspection.StorageProof{}, err
	}
	return proof, nil
}

func (c cephInspection) endpoints(ctx context.Context, contextName, cluster string, frozen []reconciliation.Block, proofs []lifecycle.BlockEvidence) ([]inspection.Endpoint, error) {
	all := slices.Clone(proofs)
	for _, name := range ceph.EndpointGateways(cluster, frozen) {
		gateway, err := c.reconciler.Evidence(ctx, contextName, string(api.CephObjectGateway), name)
		if err != nil {
			return nil, err
		}
		all = append(all, gateway...)
	}
	endpoints := []inspection.Endpoint{}
	for _, row := range ceph.InspectEndpoints(cluster, frozen, all) {
		endpoints = append(endpoints, inspection.Endpoint{Service: row.Service, URLs: row.URLs, CertificateSHA256: row.CertificateSHA256,
			Issuer: row.Issuer, Status: row.Status, Reason: row.Reason})
	}
	return endpoints, nil
}

type installProofs struct{ reconciler evidenceReader }

func (i installProofs) Installed(ctx context.Context, contextName, cluster string) (bool, error) {
	proofs, err := i.reconciler.Evidence(ctx, contextName, string(api.OpenShiftCluster), cluster)
	if err != nil {
		return false, err
	}
	for _, proof := range proofs {
		if proof.Implementation == agentinstall.InstallImplementation && proof.Verb == reconciliation.Apply && proof.State == reconciliation.BlockDone {
			return true, nil
		}
	}
	return false, nil
}

type hostKeyProofs struct {
	keys    hostKeyReader
	records trustRecords
}

func (h hostKeyProofs) HostKeyProved(ctx context.Context, contextName, name string, object api.Object) (bool, error) {
	target, reachable := machine.SSH(object)
	if reachable && target.KnownHostsRef != "" {
		return true, nil
	}
	if machine.Installed(object) {
		_, found, err := h.keys.HostKey(ctx, contextName, name)
		return found, err
	}
	if !reachable || h.records == nil {
		return false, nil
	}
	data, err := h.records.ReadHostKeys(ctx, contextName)
	if err != nil {
		return false, err
	}
	store, err := trust.Decode(data)
	if err != nil {
		return false, nil
	}
	record, found := store.Find(name)
	return found && record.Address == target.Address && record.Port == target.Port, nil
}

func (d inspectionCustody) ConsolePassword(ctx context.Context, contextName, cluster string) (inspection.Custodied, bool, error) {
	if d.administrator == nil {
		return inspection.Custodied{}, false, availability.ErrNotImplemented
	}
	read, found, err := d.administrator.AdministratorPassword(ctx, contextName, cluster, agentinstall.InstallBlockID(cluster), agentinstall.ConsolePasswordOutput)
	return inspection.Custodied{Material: read.Material, Unproved: read.Unproved}, found, err
}

type administratorPasswordReader interface {
	AdministratorPassword(context.Context, string, string, string, string) (secretstore.ProducedMaterial, bool, error)
}
