package main

import (
	"context"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	containeraccess "github.com/crmarques/bootwright/internal/containercluster/access"
	"github.com/crmarques/bootwright/internal/containercluster/agentinstall"
	"github.com/crmarques/bootwright/internal/environment/inspection"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/machine/inventory"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
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
		hostKeyProofs{keys: machineHostKeys{reconciler: reconciler}}, currentSelection(selection), dashboardCustody{reader: producedReader{binder: binder}})
}

type producedEntries interface {
	ReadProduced(ctx context.Context, contextName, block, name string) (containeraccess.Custodied, bool, error)
}

type dashboardCustody struct{ reader producedEntries }

func (d dashboardCustody) DashboardPassword(ctx context.Context, contextName, cluster string) (inspection.Custodied, bool, error) {
	read, found, err := d.reader.ReadProduced(ctx, contextName, ceph.DashboardBlock(cluster), ceph.DashboardOutput)
	return inspection.Custodied{Material: read.Material, Unproved: read.Unproved}, found, err
}

type cephInspection struct{ reconciler cephReader }

func (c cephInspection) Resources(ctx context.Context, contextName string, catalog api.Catalog) (map[string][]inspection.Resource, error) {
	found, err := c.reconciler.StorageResources(ctx, contextName)
	if err != nil {
		return nil, err
	}
	rows := map[string][]inspection.Resource{}
	for _, cluster := range found.Clusters {
		declared, ok := catalog.Find(api.StorageCluster, cluster.Name)
		if management := declared.Spec().Get("management").Text(); cluster.Kind != string(api.StorageCluster) || !ok || management != "" && management != inspection.ManagementManaged {
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

func (c cephInspection) ManagedCeph(ctx context.Context, contextName string, catalog api.Catalog, cluster api.Object) (inspection.CephProof, error) {
	proofs, err := c.reconciler.Evidence(ctx, contextName, string(api.StorageCluster), cluster.Name())
	if err != nil {
		return inspection.CephProof{}, err
	}
	found, reason := ceph.Inspect(catalog, cluster, proofs)
	if reason != "" {
		return inspection.CephProof{Reason: reason}, nil
	}
	proof := inspection.CephProof{
		FSID:  found.FSID,
		Image: inspection.Image{Reference: found.Image.Reference, Digest: found.Image.Digest, Platform: found.Image.Platform},
	}
	for _, monitor := range found.Monitors {
		proof.Monitors = append(proof.Monitors, inspection.Monitor{Host: monitor.Host, FQDN: monitor.FQDN, Address: monitor.Address})
	}
	d := found.Dashboard
	proof.Dashboard = inspection.Dashboard{Status: d.Status, Reason: d.Reason, User: d.User, URLs: []inspection.DashboardURL{}, CertificateSHA256: d.CertificateSHA256}
	for _, url := range d.URLs {
		proof.Dashboard.URLs = append(proof.Dashboard.URLs, inspection.DashboardURL{Host: url.Host, URL: url.URL, Role: url.Role})
	}
	return proof, nil
}

type installProofs struct{ reconciler evidenceReader }

func (i installProofs) Installed(ctx context.Context, contextName, cluster string) (bool, error) {
	proofs, err := i.reconciler.Evidence(ctx, contextName, string(api.ContainerCluster), cluster)
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
