package enrollment

type EnrollRequest struct {
	ContextName      string
	Machines         []string
	Replace          []string
	DryRun           bool
	SkipConfirmation bool
}

// The actions one enrollment reports. Only Add and Replace change anything;
// Reuse and Skip exist so an operator can see why a Machine was left alone.
const (
	ActionAdd     = "add"
	ActionReuse   = "reuse"
	ActionReplace = "replace"
	ActionSkip    = "skip"
)

// Report is what an enrollment resolved and, unless it was a dry run, what it
// recorded. It carries public key material only.
type Report struct {
	Context string `json:"context"`
	DryRun  bool   `json:"dryRun"`
	// Pending counts the records this enrollment resolved to write, which is
	// what an operator confirms and what a dry run reports without writing.
	// Recorded counts what it actually wrote.
	Pending  int          `json:"pending"`
	Recorded int          `json:"recorded"`
	Hosts    []HostReport `json:"hosts"`
}

type HostReport struct {
	Machine             string `json:"machine"`
	Address             string `json:"address,omitempty"`
	Port                int    `json:"port,omitempty"`
	Action              string `json:"action"`
	KeyType             string `json:"keyType,omitempty"`
	Fingerprint         string `json:"fingerprint,omitempty"`
	PreviousFingerprint string `json:"previousFingerprint,omitempty"`
	Reason              string `json:"reason,omitempty"`
}
