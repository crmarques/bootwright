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

type StorageProof struct {
	Release         string
	AdminNode       string
	AdminCommand    string
	Reason          string
	FSID            string
	Image           Image
	Monitors        []Monitor
	Dashboard       Dashboard
	Monitoring      []MonitoringAccess
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

type MonitoringCustody interface {
	MonitoringCredentials(ctx context.Context, contextName, cluster, service string) (Custodied, bool, error)
	GrafanaPassword(ctx context.Context, contextName, cluster string) (Custodied, bool, error)
}

type Custody interface {
	DashboardCustody
	ClientCustody
	MonitoringCustody
	ObjectUserCredentials(ctx context.Context, contextName, user string) (Custodied, bool, error)
}

type StorageEvidence interface {
	ManagedStorage(ctx context.Context, contextName string, catalog api.Catalog, cluster api.Object) (StorageProof, error)
	Resources(ctx context.Context, contextName string, catalog api.Catalog) (map[string][]Resource, error)
	ObjectUserGateway(user api.Object) string
	ObjectUserProved(ctx context.Context, contextName string, catalog api.Catalog, user api.Object) (bool, error)
}

type InstallEvidence interface {
	Installed(ctx context.Context, contextName, cluster string) (bool, error)
}

type HostKeys interface {
	HostKeyProved(ctx context.Context, contextName, name string, object api.Object) (bool, error)
}

type ClientAccess interface {
	ClientReason(context.Context, string, string, string) (string, error)
}

type ConsoleCustody interface {
	ConsolePassword(context.Context, string, string) (Custodied, bool, error)
}
