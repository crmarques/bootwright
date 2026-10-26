package inspection

import (
	"context"
	"errors"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/secrets"
)

var ErrObjectUserMaterial = errors.New("the custodied object-user material is not one S3 credential of its user")

func (s Service) revealObjectUser(ctx context.Context, selected string, catalog api.Catalog, entry *StorageEntry, name string) error {
	user, declared := catalog.Find(api.StorageObjectUser, name)
	if !declared || user.Spec().Get("clusterRef").Text() != entry.Name {
		return failure("access.target", "StorageCluster "+entry.Name+" of context "+selected+" declares no StorageObjectUser named "+name,
			"bootwright cluster info --context "+selected+" --name "+entry.Name+" lists its object users")
	}
	for _, user := range []api.Object{user} {
		gatewayName := user.Spec().Get("objectGatewayRef").Text()
		gateway, exists := catalog.Find(api.StorageObjectGateway, gatewayName)
		if !exists || gateway.Spec().Get("clusterRef").Text() != entry.Name {
			return failure("access.unavailable", "the object user's gateway does not belong to the selected cluster", "restore the context's original storage graph")
		}
		proved, err := s.ceph.ObjectUserProved(ctx, selected, catalog, user)
		if err != nil {
			return err
		}
		if !proved {
			return failure("access.unavailable", "the original apply does not prove StorageObjectUser "+user.Name()+" in this cluster and gateway", "restore the original storage scope before requesting its credentials")
		}
		read, found, err := s.custody.ObjectUserCredentials(ctx, selected, user.Name())
		if err != nil {
			read.Material.Clear()
			if errors.Is(err, ErrObjectUserMaterial) {
				return failure("secret.store.corrupt", "this context holds invalid S3 material for StorageObjectUser "+user.Name(), "restore the original context custody before continuing")
			}
			return err
		}
		if !found || read.Unproved || !validObjectCredentials(read.Material) {
			read.Material.Clear()
			return failure("access.unavailable", "this context holds no proved S3 credentials for StorageObjectUser "+user.Name(), "bootwright apply --context "+selected+" --stage clusters")
		}
		entry.Ceph.ObjectUsers = append(entry.Ceph.ObjectUsers, ObjectUserAccess{Name: user.Name(), Gateway: gatewayName, Credentials: read.Material})
	}
	return nil
}

func validObjectCredentials(material secrets.Material) bool {
	if !slices.Equal(material.Parts(), []secrets.Part{secrets.PasswordPart, secrets.UsernamePart}) {
		return false
	}
	for _, part := range []secrets.Part{secrets.UsernamePart, secrets.PasswordPart} {
		value, ok := material.Part(part)
		valid := ok && len(value) > 0 && len(value) <= 128
		for _, c := range value {
			valid = valid && c >= 0x21 && c <= 0x7e
		}
		clear(value)
		if !valid {
			return false
		}
	}
	return true
}

func ClearSecrets(result *InfoResult) {
	for _, cluster := range result.Storage {
		if cluster.Ceph == nil {
			continue
		}
		if cluster.Ceph.Dashboard != nil {
			cluster.Ceph.Dashboard.Password.Clear()
		}
		for _, user := range cluster.Ceph.ObjectUsers {
			user.Credentials.Clear()
		}
	}
}
