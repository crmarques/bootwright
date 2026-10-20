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
				"this context's custody holds a keyring entry for StorageClient "+client+" that is not one Ceph credential of client."+client,
				"read it natively with the administrator command that bootwright cluster info --context "+selected+" --name "+name+" prints, followed by auth get client."+client)
		}
		return nil, err
	}
	if !found {
		custodied.Material.Clear()
		return nil, failure("access.unavailable",
			"this context holds no keyring for StorageClient "+client+" of StorageCluster "+name+"; an apply keeps it once it creates the client",
			"bootwright apply --context "+selected+" --stage clusters")
	}
	return &KeyringResult{Context: selected, Cluster: name, Client: client, Material: custodied.Material, Unproved: custodied.Unproved}, nil
}

func keyringTarget(catalog api.Catalog, selected, name, client string) error {
	discovery := "bootwright cluster info --context " + selected
	clients := discovery + " --name " + name + " lists the StorageClients of " + name
	kinds := discovery + " lists the clusters this context selects and their kinds"
	if name == "" {
		return failure("access.target", "bootwright cluster keyring requires one cluster name", discovery+" lists the clusters this context selects")
	}
	if client == "" {
		return failure("access.target", "bootwright cluster keyring requires one StorageClient name", clients)
	}
	if _, ok := catalog.Find(api.ContainerCluster, name); ok {
		return failure("cluster.not-applicable",
			"bootwright cluster keyring does not apply to "+name+", an OpenShift or OKD ContainerCluster; it "+managedCephOnly, kinds)
	}
	cluster, ok := catalog.Find(api.StorageCluster, name)
	if !ok {
		return failure("access.target", "the context "+selected+" selects no StorageCluster named "+name,
			"name a managed Ceph StorageCluster this context selects; "+discovery+" lists them")
	}
	if external(cluster) {
		return failure("cluster.not-applicable",
			"bootwright cluster keyring does not apply to "+name+", an external Ceph StorageCluster; it "+managedCephOnly, kinds)
	}
	declared, ok := catalog.Find(api.StorageClient, client)
	if !ok {
		return failure("access.target", "the context "+selected+" selects no StorageClient named "+client, clients)
	}
	if owner := declared.Spec().Get("clusterRef").Text(); owner != name {
		return failure("access.target", "the StorageClient "+client+" belongs to StorageCluster "+owner+", not "+name, clients)
	}
	return nil
}
