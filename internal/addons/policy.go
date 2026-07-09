package addons

const DefaultClusterAddonFieldManager = "bootwright"

type ClusterAddonPolicy struct {
	Prune           bool
	ServerSideApply *bool
	FieldManager    string
	ContinueOnError bool
}

func (p ClusterAddonPolicy) UseServerSideApply() bool {
	return p.ServerSideApply == nil || *p.ServerSideApply
}

func DefaultPolicy() ClusterAddonPolicy {
	enabled := true
	return ClusterAddonPolicy{
		ServerSideApply: &enabled,
		FieldManager:    DefaultClusterAddonFieldManager,
	}
}
