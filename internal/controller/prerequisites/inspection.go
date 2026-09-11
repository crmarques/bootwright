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
}

// ActionProgress is the operator-visible outcome of one planned setup action.
// It reports what the receipt durably records, so an interrupted setup shows
// which effects completed rather than only that it did not finish.
type ActionProgress struct {
	ID      string
	Phase   string
	Outcome string
}

// ProgressEvent is one operator-visible step of an authorized setup. Detail is
// safe display text naming the work in flight; Step and Steps are present only
// when the action knows its own extent.
type ProgressEvent struct {
	Action string
	Status string
	Detail string
	Step   int
	Steps  int
}
