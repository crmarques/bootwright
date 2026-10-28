package prerequisites

import "github.com/crmarques/bootwright/internal/diagnostics"

type CheckRequest struct{ ContextName string }

// SetupRequest carries no context. Setup prepares the prerequisites every
// context on this host shares; the prerequisites a context adds are installed
// by the controller stage of its own apply.
type SetupRequest struct {
	DryRun           bool
	SkipConfirmation bool
	WheelMirror      string
	DownloadOnly     bool
	Destination      string
}

// Check scopes tell the operator which command settles a missing prerequisite:
// HostScope is context-independent and owned by setup, ContextScope is selected
// by one context's desired state and owned by its controller stage.
const (
	HostScope    = "host"
	ContextScope = "context"
)

// CheckIDs lists every check identity setup and readiness report, in the order
// a report lists them, so each one a presenter labels is known in one place.
func CheckIDs() []string {
	return []string{
		"host", "installed-host", "fips-mode", "host-packages", "python-runtime", "target-tools",
		"libvirt-client", "hypervisor", "installer-media", "nmstate", "iputils", "controller-binding", "state-root", "setup-state", "listener-firewall",
	}
}

type Check struct {
	ID       string
	Required string
	Observed string
	Status   string
	Scope    string
	Target   string
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
	Route    string
	Platform Platform
	DryRun   bool
	Outcome  string
	Checks   []Check
	Actions  []string
	// Dependencies names the host package requirements the plan installs.
	Dependencies []string
	// Preview is dnf's own preview of that install, display text only.
	Preview *HostPackagePreview
	// Files lists every locked wheel, for a dry run's machine output.
	Files             []LockedFile
	Export            *WheelExport
	PlanPresented     bool
	PresentedWarnings int
	// Next is the one command that settles a not-ready readiness check, as
	// preflight decided it, the setup a dry run previewed, or the bootwright
	// command a refused or failed setup's remediation names. It is empty for
	// every other outcome and for a remediation that names no command.
	Next string
	// ProgressPresented records that progress rows were already streamed, so a
	// failure report adds only its outcome rather than repeating the headline.
	ProgressPresented bool
	Progress          []ActionProgress
	// Warnings are informational lines, such as an untested release. They
	// never change Outcome.
	Warnings []diagnostics.Diagnostic
	Table    *ReceiptTable
	// LogLocation names on this host the directory of the setup run that kept
	// what this invocation's controller Ansible printed. It is empty when no
	// Ansible started or its output could not be retained, and never changes
	// Outcome.
	LogLocation string
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
