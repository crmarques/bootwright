package prerequisites

import (
	"context"

	"github.com/crmarques/bootwright/internal/controller"
)

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

type HostInspector interface {
	Platform(context.Context) (Platform, error)
	Identity(context.Context) (controller.InstalledHostIdentity, error)
	Runtime(context.Context, RuntimeRequirement) (RuntimeInspection, error)
}

type Check struct {
	ID       string
	Required string
	Observed string
	Status   string
}

type Report struct {
	ContextName   string
	Machine       string
	Platform      Platform
	DryRun        bool
	Outcome       string
	Checks        []Check
	Actions       []string
	Dependencies  []string
	PlanPresented bool
	Progress      []ActionProgress
}

// ActionProgress is the operator-visible outcome of one planned setup action.
// It reports what the receipt durably records, so an interrupted setup shows
// which effects completed rather than only that it did not finish.
type ActionProgress struct {
	ID      string
	Phase   string
	Outcome string
}

type PlanPresenter interface {
	PresentControllerPlan(context.Context, Report) error
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

// ProgressReporter receives events while setup runs. Reporting is best effort:
// a reporting failure never changes an effect or its recorded outcome.
type ProgressReporter interface {
	ReportProgress(context.Context, ProgressEvent)
}
