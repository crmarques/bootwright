package prerequisites

type Platform struct {
	OS           string `json:"os"`
	Release      string `json:"release"`
	Architecture string `json:"architecture"`
}

type InstalledFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type InstalledLink struct {
	Path   string `json:"path"`
	Target string `json:"target"`
}

type RuntimeRequirement struct {
	Version     string          `json:"version"`
	Files       []InstalledFile `json:"files"`
	LockPath    string          `json:"lockPath"`
	Links       []InstalledLink `json:"links"`
	SELinuxMode string          `json:"selinuxMode"`
}

type RuntimeInspection struct {
	Present  bool
	Ready    bool
	Conflict bool
	Version  string
}

// ActionProgress is the operator-visible outcome of one planned setup action.
// It reports what the receipt durably records, so an interrupted setup shows
// which effects completed rather than only that it did not finish.
type ActionProgress struct {
	ID      string
	Phase   string
	Outcome string
}

// Progress phases, in the order a setup runs them: inspection verifies each
// host check, resolution discovers exact dependency identities before the
// plan, and setup performs the approved actions after confirmation. Readiness
// is preflight's inspection, which never plans.
const (
	InspectionPhase = "inspection"
	ReadinessPhase  = "readiness"
	ResolutionPhase = "resolution"
	SetupPhase      = "setup"
)

// ProgressEvent is one operator-visible step of an authorized setup. Action
// names the step: a receipt action identity during setup, or the dependency
// family during resolution. Detail is safe display text naming the work in
// flight; Step and Steps are present only when the phase knows its extent.
type ProgressEvent struct {
	Phase  string
	Action string
	Status string
	Detail string
	Step   int
	Steps  int
	// Completed of Declared are the sub-steps this action has proved, out of
	// the sub-steps it knows it must take. Zero reports no completion.
	Completed int
	Declared  int
}
