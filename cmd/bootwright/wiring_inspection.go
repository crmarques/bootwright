package main

import (
	"context"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	containeraccess "github.com/crmarques/bootwright/internal/containercluster/access"
	"github.com/crmarques/bootwright/internal/containercluster/agentinstall"
	"github.com/crmarques/bootwright/internal/environment/inspection"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/machine/inventory"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
	"github.com/crmarques/bootwright/internal/storage/ceph"
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

func wireInspection(state inventory.EffectiveState, reconciler lifecycle.Service, binder *custody.Service, selection contexts.SelectionStore) inspection.Service {
	return inspection.New(effectiveGraph{state: state}, cephInspection{reconciler: reconciler}, installProofs{reconciler: reconciler},
		hostKeyProofs{keys: machineHostKeys{reconciler: reconciler}}, currentSelection(selection), inspectionCustody{reader: producedReader{binder: binder}}).WithMembers(clusterMembers())
}

type producedEntries interface {
	ReadProduced(ctx context.Context, contextName, block, name string) (containeraccess.Custodied, bool, error)
}

type inspectionCustody struct{ reader producedEntries }

func (d inspectionCustody) ObjectUserCredentials(ctx context.Context, contextName, user string) (inspection.Custodied, bool, error) {
	read, found, err := d.reader.ReadProduced(ctx, contextName, ceph.ObjectUserBlockID(user), ceph.ObjectUserOutput)
	if err != nil || !found {
		return inspection.Custodied{Material: read.Material, Unproved: read.Unproved}, found, err
	}
	defer read.Material.Clear()
	value, ok := read.Material.Part(secrets.ValuePart)
	defer clear(value)
	if !ok || !slices.Equal(read.Material.Parts(), []secrets.Part{secrets.ValuePart}) {
		return inspection.Custodied{}, true, inspection.ErrObjectUserMaterial
	}
	material, ok := ceph.ObjectUserCredentials(value, user)
	if !ok {
		return inspection.Custodied{}, true, inspection.ErrObjectUserMaterial
	}
	return inspection.Custodied{Material: material, Unproved: read.Unproved}, true, nil
}

func (d inspectionCustody) DashboardPassword(ctx context.Context, contextName, cluster string) (inspection.Custodied, bool, error) {
	read, found, err := d.reader.ReadProduced(ctx, contextName, ceph.DashboardBlock(cluster), ceph.DashboardOutput)
	return inspection.Custodied{Material: read.Material, Unproved: read.Unproved}, found, err
}

func (d inspectionCustody) ClientKeyring(ctx context.Context, contextName, client string) (inspection.Custodied, bool, error) {
	read, found, err := d.reader.ReadProduced(ctx, contextName, ceph.ClientBlockID(client), ceph.ClientOutput)
	if err != nil || !found {
		return inspection.Custodied{Material: read.Material, Unproved: read.Unproved}, found, err
	}
	defer read.Material.Clear()
	value, ok := read.Material.Part(secrets.ValuePart)
	defer clear(value)
	if !ok || !slices.Equal(read.Material.Parts(), []secrets.Part{secrets.ValuePart}) {
		return inspection.Custodied{}, true, inspection.ErrClientMaterial
	}
	keyring, ok := ceph.ClientKeyring(value, client)
	defer clear(keyring)
	if !ok {
		return inspection.Custodied{}, true, inspection.ErrClientMaterial
	}
	return inspection.Custodied{Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: keyring}), Unproved: read.Unproved}, true, nil
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
	for i := range proofs {
		matches := 0
		for _, block := range frozen.Frozen {
			if block.ID == ceph.ObjectUserBlockID(user.Name()) && block.Kind == proofs[i].Kind && block.Object == proofs[i].Object && block.Implementation == proofs[i].Implementation {
				proofs[i].Block = block
				matches++
			}
		}
		if matches != 1 {
			return false, nil
		}
	}
	return ceph.ObjectUserProved(catalog, user, proofs), nil
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

func (cephInspection) ObjectUserGateway(user api.Object) string { return ceph.ObjectUserGateway(user) }

func (c cephInspection) ManagedStorage(ctx context.Context, contextName string, catalog api.Catalog, cluster api.Object) (inspection.StorageProof, error) {
	administration := ceph.ClusterAdministration(cluster)
	proofs, err := c.reconciler.Evidence(ctx, contextName, string(api.CephCluster), cluster.Name())
	if err != nil {
		return inspection.StorageProof{}, err
	}
	found, reason := ceph.Inspect(catalog, cluster, proofs)
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
	proof.Endpoints, err = c.endpoints(ctx, contextName, cluster.Name(), proofs)
	if err != nil {
		return inspection.StorageProof{}, err
	}
	return proof, nil
}

func (c cephInspection) endpoints(ctx context.Context, contextName, cluster string, proofs []lifecycle.BlockEvidence) ([]inspection.Endpoint, error) {
	found, err := c.reconciler.StorageResources(ctx, contextName)
	if err != nil {
		return nil, err
	}
	var frozen []reconciliation.Block
	if found != nil {
		frozen = found.Frozen
	}
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

type hostKeyProofs struct{ keys hostKeyReader }

func (h hostKeyProofs) HostKeyProved(ctx context.Context, contextName, name string) (bool, error) {
	_, found, err := h.keys.HostKey(ctx, contextName, name)
	return found, err
}
