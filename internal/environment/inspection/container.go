package inspection

import api "github.com/crmarques/bootwright/api/v1alpha1"

type ContainerDetails struct {
	Distribution, Release, ReleaseImage, Platform, InstallMode, BaseDomain, IngressDomain string
	FIPS                                                                                  bool
	NetworkType                                                                           string
	MachineNetworks, ServiceNetworks                                                      []string
	ClusterNetworks                                                                       []ContainerNetwork
	Nodes                                                                                 []ContainerNode
	Endpoints                                                                             []ContainerEndpoint
	Status, Reason, Remedy                                                                string
}

type ContainerNetwork struct {
	CIDR       string
	HostPrefix int
}

type ContainerNode struct {
	Name, Role, Machine, FQDN, Address string
}

type ContainerEndpoint struct {
	Service, URL, Address, Source string
}

func (s Service) WithContainerDetails(project func(api.Catalog, api.Object) ContainerDetails) Service {
	s.containerDetails = project
	return s
}

func (s Service) describeContainer(catalog api.Catalog, cluster api.Object, selected string, installed bool) *ContainerDetails {
	if s.containerDetails == nil {
		return nil
	}
	details := s.containerDetails(catalog, cluster)
	details.Status = StatusReady
	if !installed {
		details.Status = StatusUnavailable
		details.Reason = "no completed installation is recorded in this context"
		details.Remedy = "bootwright apply --context " + selected + " --stage clusters"
	}
	return &details
}
