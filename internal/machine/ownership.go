package machine

// OwnershipState is what a context's durable operation evidence proves about
// one API object: the verb that froze its blocks and the least settled state
// they reached. It is the Machine domain's whole vocabulary for ownership, so
// an inspection and a power operation read the same evidence the same way.
type OwnershipState struct {
	Verb  string
	State string
}

// The verbs and block states an OwnershipState carries. They mirror the
// reconciliation vocabulary the evidence was published in; a value outside
// this set is unknown, never assumed settled.
const (
	VerbApply    = "apply"
	VerbDestroy  = "destroy"
	BlockPending = "pending"
	BlockRunning = "running"
	BlockFailed  = "failed"
	BlockUnknown = "unknown"
	BlockDone    = "done"
)

// Realized reports an object whose effects this context currently owns: an
// apply proved them, and no removal has completed since.
func (s OwnershipState) Realized() bool {
	return s.State == BlockDone && s.Verb != VerbDestroy
}
