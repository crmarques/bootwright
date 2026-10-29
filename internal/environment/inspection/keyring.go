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
	cluster, err := keyringTarget(catalog, selected, name, client)
	if err != nil {
		return nil, err
	}
	clientKind, _ := api.RoleKind(api.StorageClusterCategory, api.ClientRole)
	if s.custody == nil {
		return nil, availability.ErrNotImplemented
	}
	custodied, found, err := s.custody.ClientKeyring(ctx, selected, client)
	if err != nil {
		custodied.Material.Clear()
		if errors.Is(err, ErrClientMaterial) {
			return nil, failure("secret.store.corrupt",
				"this context's custody holds a keyring entry for "+string(clientKind)+" "+client+" that is not one Ceph credential of client."+client,
				"read it natively with the administrator command that bootwright cluster info --context "+selected+" --name "+name+" prints, followed by auth get client."+client)
		}
		return nil, err
	}
	if !found {
		custodied.Material.Clear()
		return nil, failure("access.unavailable",
			"this context holds no keyring for "+string(clientKind)+" "+client+" of "+string(cluster.Kind())+" "+name+"; an apply keeps it once it creates the client",
			"bootwright apply --context "+selected+" --stage clusters")
	}
	return &KeyringResult{Context: selected, Cluster: name, Client: client, Material: custodied.Material, Unproved: custodied.Unproved}, nil
}

func keyringTarget(catalog api.Catalog, selected, name, client string) (api.Object, error) {
	clientKind, _ := api.RoleKind(api.StorageClusterCategory, api.ClientRole)
	storageKinds := api.CategoryKinds(api.StorageClusterCategory)
	discovery := "bootwright cluster info --context " + selected
	clients := discovery + " --name " + name + " lists the " + string(clientKind) + "s of " + name
	kinds := discovery + " lists the clusters this context selects and their kinds"
	if name == "" {
		return api.Object{}, failure("access.target", "bootwright cluster keyring requires one cluster name", discovery+" lists the clusters this context selects")
	}
	if client == "" {
		return api.Object{}, failure("access.target", "bootwright cluster keyring requires one "+string(clientKind)+" name", clients)
	}
	if container, ok := catalog.FindInCategory(api.ContainerClusterCategory, name); ok {
		return api.Object{}, failure("cluster.not-applicable",
			"bootwright cluster keyring does not apply to "+name+", "+api.Article(container.Kind())+" "+string(container.Kind())+"; it "+storageOnly(), kinds)
	}
	cluster, ok := catalog.FindInCategory(api.StorageClusterCategory, name)
	if !ok {
		return api.Object{}, failure("access.target", "the context "+selected+" selects no "+api.KindPhrase(storageKinds, false)+" named "+name,
			"name a managed "+api.KindPhrase(storageKinds, false)+" this context selects; "+discovery+" lists them")
	}
	if external(cluster) {
		return api.Object{}, failure("cluster.not-applicable",
			"bootwright cluster keyring does not apply to "+name+", an external "+string(cluster.Kind())+"; it "+storageOnly(), kinds)
	}
	declared, ok := catalog.Find(clientKind, client)
	if !ok {
		return api.Object{}, failure("access.target", "the context "+selected+" selects no "+string(clientKind)+" named "+client, clients)
	}
	if owner := declared.Spec().Get("clusterRef").Text(); owner != name {
		held := cluster.Kind()
		if declaredOwner, ok := catalog.FindInCategory(api.StorageClusterCategory, owner); ok {
			held = declaredOwner.Kind()
		}
		return api.Object{}, failure("access.target", "the "+string(clientKind)+" "+client+" belongs to "+string(held)+" "+owner+", not "+name, clients)
	}
	return cluster, nil
}
