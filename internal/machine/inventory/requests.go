package inventory

type ListRequest struct {
	ContextName string
	Clusters    []string
	Silent      bool
}

type ListResult struct {
	Context  string
	Machines []MachineRow
}

// MachineRow is one Machine as desired state declares it and durable evidence
// proves it. Address is the effective SSH contact, empty when the Machine
// declares none.
type MachineRow struct {
	Name     string
	Address  string
	OS       string
	Provider string
	Clusters []string
	State    string
}
