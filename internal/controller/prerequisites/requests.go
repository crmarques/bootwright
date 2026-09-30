package prerequisites

import "github.com/crmarques/bootwright/internal/diagnostics"

type CheckRequest struct{ ContextName string }

// SetupRequest carries no context. Setup prepares the prerequisites every
// context on this host shares; the prerequisites a context adds are installed
// by the controller stage of its own apply.
type SetupRequest struct {
	DryRun           bool
	SkipConfirmation bool
	// PurgeOldBundles retires the execution bundles this host no longer needs,
	// after the setup it runs beside has completed, or before that setup
	// publishes a new bundle into a host already at its bound.
	PurgeOldBundles bool
}

// Check scopes tell the operator which command settles a missing prerequisite:
// HostScope is context-independent and owned by setup, ContextScope is selected
// by one context's desired state and owned by its controller stage.
const (
	HostScope    = "host"
	ContextScope = "context"
)

type Check struct {
	ID       string
	Required string
	Observed string
	Status   string
	Scope    string
}

// Summary is what the operator reads beside a check: the observation when it
// holds, and the requirement beside the observation when it does not, because
// the difference is the actionable part.
func (c Check) Summary() string {
	if c.Status == "ready" {
		return c.Observed
	}
	return "required " + c.Required + "; observed " + c.Observed
}

type Report struct {
	ContextName string
	Machine     string
	// Route names the acquisition route this scope resolved and where it came
	// from, so a report says how the host reaches its publishers.
	Route         string
	Platform      Platform
	DryRun        bool
	Outcome       string
	Checks        []Check
	Actions       []string
	Dependencies  []string
	PlanPresented bool
	// ProgressPresented records that progress rows were already streamed, so a
	// failure report adds only its outcome rather than repeating the headline.
	ProgressPresented bool
	Progress          []ActionProgress
	// RetiredBundles names the superseded execution bundles this invocation
	// removed, in canonical order. It is empty unless retirement was asked for
	// and either the setup it runs beside completed or that setup made room at
	// the host's bound.
	RetiredBundles []string
	// Warnings are what dependency resolution read but did not refuse, such as
	// a publisher page of a newer Index API minor. They never change Outcome.
	Warnings []diagnostics.Diagnostic
}

// PendingScope names the narrowest scope that has an unmet check, so a result
// can offer the one command that settles it. An unmet host prerequisite always
// wins, because the prerequisites a context adds need a prepared host first.
func PendingScope(report Report) string {
	pending := ""
	for _, check := range report.Checks {
		if check.Status == "ready" {
			continue
		}
		if check.Scope == HostScope || check.Scope == "" {
			return HostScope
		}
		pending = check.Scope
	}
	return pending
}
