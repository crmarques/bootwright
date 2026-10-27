package inspection

import (
	"context"
	"errors"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/secrets"
)

var ErrClientMaterial = errors.New("the custodied client material is not one Ceph credential of its client")

type KeyringRequest struct {
	ContextName string
	Name        string
	Client      string
}

type KeyringResult struct {
	Context  string
	Cluster  string
	Client   string
	Material secrets.Material
	Unproved bool
}

func (s Service) ClientKeyring(ctx context.Context, request KeyringRequest) (*KeyringResult, error) {
	selected, catalog, err := s.read(ctx, request.ContextName)
	if err != nil {
		return nil, err
	}
	name, client := strings.TrimSpace(request.Name), strings.TrimSpace(request.Client)
	if err := keyringTarget(catalog, selected, name, client); err != nil {
		return nil, err
	}
	if s.custody == nil {
		return nil, availability.ErrNotImplemented
	}
	custodied, found, err := s.custody.ClientKeyring(ctx, selected, client)
	if err != nil {
		custodied.Material.Clear()
		if errors.Is(err, ErrClientMaterial) {
			return nil, failure("secret.store.corrupt",
				"this context's custody holds a keyring entry for CephClient "+client+" that is not one Ceph credential of client."+client,
				"read it natively with the administrator command that bootwright cluster info --context "+selected+" --name "+name+" prints, followed by auth get client."+client)
		}
		return nil, err
	}
	if !found {
		custodied.Material.Clear()
		return nil, failure("access.unavailable",
			"this context holds no keyring for CephClient "+client+" of CephCluster "+name+"; an apply keeps it once it creates the client",
			"bootwright apply --context "+selected+" --stage clusters")
	}
	return &KeyringResult{Context: selected, Cluster: name, Client: client, Material: custodied.Material, Unproved: custodied.Unproved}, nil
}

func keyringTarget(catalog api.Catalog, selected, name, client string) error {
	discovery := "bootwright cluster info --context " + selected
	clients := discovery + " --name " + name + " lists the CephClients of " + name
	kinds := discovery + " lists the clusters this context selects and their kinds"
	if name == "" {
		return failure("access.target", "bootwright cluster keyring requires one cluster name", discovery+" lists the clusters this context selects")
	}
	if client == "" {
		return failure("access.target", "bootwright cluster keyring requires one CephClient name", clients)
	}
	if _, ok := catalog.FindInCategory(api.ContainerClusterCategory, name); ok {
		return failure("cluster.not-applicable",
			"bootwright cluster keyring does not apply to "+name+", an OpenShiftCluster; it "+managedCephOnly, kinds)
	}
	cluster, ok := catalog.FindInCategory(api.StorageClusterCategory, name)
	if !ok {
		return failure("access.target", "the context "+selected+" selects no CephCluster named "+name,
			"name a managed CephCluster this context selects; "+discovery+" lists them")
	}
	if external(cluster) {
		return failure("cluster.not-applicable",
			"bootwright cluster keyring does not apply to "+name+", an external CephCluster; it "+managedCephOnly, kinds)
	}
	clientKind, _ := api.RoleKind(api.StorageClusterCategory, api.ClientRole)
	declared, ok := catalog.Find(clientKind, client)
	if !ok {
		return failure("access.target", "the context "+selected+" selects no CephClient named "+client, clients)
	}
	if owner := declared.Spec().Get("clusterRef").Text(); owner != name {
		return failure("access.target", "the CephClient "+client+" belongs to CephCluster "+owner+", not "+name, clients)
	}
	return nil
}
