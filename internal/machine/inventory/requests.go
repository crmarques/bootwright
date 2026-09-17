package inventory

type ListRequest struct {
	ContextName string
	Clusters    []string
	Silent      bool
	// Power asks each selected Machine's own management controller what its
	// power is. It is the one thing this command contacts a host for, so it is
	// opt-in: without it the command reads local state alone.
	Power bool
}

type ListResult struct {
	Context  string
	Machines []MachineRow
	// PowerRead reports whether any controller was asked at all, so a consumer
	// can tell a reading that was never taken from one that came back with no
	// answer. Both leave MachineRow.Power empty.
	PowerRead bool
}

// MachineRow is one Machine as desired state declares it and durable evidence
// proves it. Address is the effective SSH contact, empty when the Machine
// declares none. Lifecycle is what this context's own operations carried the
// Machine through; Power is what its management controller answered, and stays
// empty unless a reading was asked for and a controller was reachable.
type MachineRow struct {
	Name      string
	Address   string
	OS        string
	Provider  string
	Clusters  []string
	Lifecycle string
	Power     string
}
