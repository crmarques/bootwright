package inspection

import (
	"context"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/secrets"
)

type CurrentSelection func(context.Context) (string, error)

type Graph interface {
	Effective(ctx context.Context, contextName string) (api.Catalog, error)
}

type CephProof struct {
	Reason          string
	FSID            string
	Image           Image
	Monitors        []Monitor
	Dashboard       Dashboard
	Endpoints       []Endpoint
	Specializations []string
}

type Custodied struct {
	Material secrets.Material
	Unproved bool
}

type DashboardCustody interface {
	DashboardPassword(ctx context.Context, contextName, cluster string) (Custodied, bool, error)
}

type ClientCustody interface {
	ClientKeyring(ctx context.Context, contextName, client string) (Custodied, bool, error)
}

type Custody interface {
	DashboardCustody
	ClientCustody
	ObjectUserCredentials(ctx context.Context, contextName, user string) (Custodied, bool, error)
}

type CephEvidence interface {
	ManagedCeph(ctx context.Context, contextName string, catalog api.Catalog, cluster api.Object) (CephProof, error)
	Resources(ctx context.Context, contextName string, catalog api.Catalog) (map[string][]Resource, error)
	ObjectUserProved(ctx context.Context, contextName string, catalog api.Catalog, user api.Object) (bool, error)
}

type InstallEvidence interface {
	Installed(ctx context.Context, contextName, cluster string) (bool, error)
}

type HostKeys interface {
	HostKeyProved(ctx context.Context, contextName, machine string) (bool, error)
}
