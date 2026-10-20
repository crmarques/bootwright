package inspection

import "github.com/crmarques/bootwright/internal/secrets"

const (
	KindContainerCluster = "ContainerCluster"
	KindStorageCluster   = "StorageCluster"

	StatusNotApplicable  = "not-applicable"
	StatusNotImplemented = "not-implemented"
	StatusUnavailable    = "unavailable"
	StatusReady          = "ready"

	ManagementManaged  = "managed"
	ManagementExternal = "external"

	CommandExec       = "cluster exec"
	CommandKeyring    = "cluster keyring"
	CommandKubeconfig = "cluster kubeconfig"
	CommandKubectl    = "cluster kubectl"
	CommandOC         = "cluster oc"
	CommandRsh        = "cluster rsh"

	DashboardReady    = "ready"
	DashboardUnproved = "unproved"
	DashboardActive   = "active"
	DashboardStandby  = "standby"

	EndpointReady    = "ready"
	EndpointUnproved = "unproved"
	IssuerBound      = "bound"
	IssuerCephadm    = "cephadm"

	ResourceUnsupported = "unsupported"
	ResourcePending     = "pending"
	ResourceDone        = "done"
	ResourceFailed      = "failed"
	ResourceUnknown     = "unknown"
)

type ClusterRef struct {
	Name string
	Kind string
}

type ListResult struct {
	Context  string
	Clusters []ClusterRef
}

type AccessCommand struct {
	Command string
	Status  string
	Node    string
	Reason  string
}

type ContainerEntry struct {
	Name           string
	Kind           string
	AccessCommands []AccessCommand
}

type Image struct {
	Reference string
	Digest    string
	Platform  string
}

type Monitor struct {
	Host    string
	FQDN    string
	Address string
}

type DashboardURL struct {
	Host string
	URL  string
	Role string
}

type Dashboard struct {
	Status            string
	Reason            string
	User              string
	URLs              []DashboardURL
	CertificateSHA256 string
	Password          secrets.Material
	Revealed          bool
	Warn              bool
}

type ManagedCeph struct {
	Management      string
	Release         string
	FSID            string
	Image           *Image
	Specializations []string
	Monitors        []Monitor
	AdminNode       string
	CephCommand     string
	Status          string
	Reason          string
	Remedy          string
	Dashboard       *Dashboard
	Resources       []Resource
	Endpoints       []Endpoint
}

type Resource struct {
	Kind   string
	Name   string
	Status string
	Scope  string
}

type Endpoint struct {
	Service           string
	URLs              []string
	CertificateSHA256 string
	Issuer            string
	Status            string
	Reason            string
}

type StorageEntry struct {
	Name           string
	Kind           string
	AccessCommands []AccessCommand
	Ceph           *ManagedCeph
}

type InfoResult struct {
	Context  string
	Clusters []ContainerEntry
	Storage  []StorageEntry
}
