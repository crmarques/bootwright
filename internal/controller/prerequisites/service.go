package prerequisites

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

type Options struct {
	ListenerChecks ListenerChecks
	// AmbientRoute is the acquisition route the invoking environment selected.
	// It applies to context-free work alone; a selected context always uses its
	// own controller Machine's proxy choice.
	AmbientRoute controller.Route
	Confirmer    Confirmer
	Presenter    PlanPresenter
	Progress     ProgressReporter
	Tools        TargetToolCatalog
	Now          func() time.Time
}

// Ports are what setup owns: the setup subtree, the host's own dnf, the
// OS interpreter's runtime and the lock this build embeds.
type Ports struct {
	Setup    SetupStore
	Packages HostPackages
	Python   PythonRuntime
	Lock     RuntimeLock
	Owned    HostOwnedObjects
}

type Service struct {
	storage  Storage
	compiler Compiler
	host     HostInspector
	ports    Ports
	options  Options
}

func New(storage Storage, compiler Compiler, host HostInspector, ports Ports, options Options) Service {
	return Service{storage: storage, compiler: compiler, host: host, ports: ports, options: options}
}

func (s Service) available(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, releases := s.host.(ReleaseInspector)
	if s.storage == nil || s.compiler == nil || !releases || s.ports.Setup == nil || s.ports.Packages == nil ||
		s.ports.Python == nil || s.ports.Lock.Digest == "" || len(s.ports.Lock.ABIs) == 0 {
		return availability.ErrNotImplemented
	}
	return nil
}

func (s Service) Check(ctx context.Context, request CheckRequest) (*Report, error) {
	if err := s.available(ctx); err != nil {
		return nil, err
	}
	view, err := s.ports.Setup.ReadSetup(ctx)
	if err != nil {
		return nil, err
	}
	var result *Report
	err = s.storage.ReadController(ctx, request.ContextName, func(stored StorageView) error {
		phase := ReadinessPhase
		if request.Quiet {
			phase = ""
		}
		current, err := s.inspect(ctx, view, stored, phase, !request.installation, request.installation)
		if err != nil {
			if len(current.report.Checks) != 0 {
				current.report.Outcome = "not-ready"
				current.report.Next = remedyInvocation(err)
				result = &current.report
			}
			return err
		}
		result = &current.report
		if !request.SkipListeners && s.options.ListenerChecks != nil && current.stored.Context.Name != "" {
			checks, err := s.options.ListenerChecks(ctx, current.catalog, current.stored.Context.Name)
			if err != nil {
				return err
			}
			code, remedy := "", ""
			for _, row := range checks {
				check := Check{ID: "listener-firewall", Required: row.Required, Observed: row.Observed, Status: row.Status, Scope: ContextScope, Target: row.Target}
				result.Checks = append(result.Checks, check)
				s.report(ctx, result, checkEvent(ReadinessPhase, check))
				if row.Status != "ready" && code != "preflight.failed" {
					code, remedy = "preflight.unknown", row.Observed
					if row.Status == "not-ready" {
						code = "preflight.failed"
					}
				}
			}
			if code != "" && current.ready() {
				result.Outcome = "not-ready"
				if code == "preflight.failed" {
					return failure("preflight.failed", "controller listener firewall admission is closed", remedy)
				}
				return failure("preflight.unknown", "controller listener firewall admission is unknown", remedy)
			}
		}
		if current.contextReady() {
			result.Outcome = "ready"
			return nil
		}
		result.Outcome = "not-ready"
		result.Next = current.next()
		return failure("preflight.failed", "required controller prerequisites are not ready", "run "+current.next())
	})
	return result, err
}

// CheckInstallation checks the baseline alone or includes the explicitly
// selected context's locally installed dependencies.
func (s Service) CheckInstallation(ctx context.Context, name string) (*Report, error) {
	if name == "" {
		return s.CheckHost(ctx)
	}
	report, err := s.Check(ctx, CheckRequest{ContextName: name, SkipListeners: true, Quiet: true, installation: true})
	if report == nil || ctx.Err() != nil {
		return report, err
	}
	report.Route = ""
	if rootErr := s.stateRoot(ctx, report); rootErr != nil {
		report.Outcome = "not-ready"
		report.Next = remedyInvocation(rootErr)
		return report, rootErr
	}
	if report.Next != setupInvocation && PendingScope(*report) == HostScope && (err == nil || report.Next != "") {
		report.Outcome, report.Next = "not-ready", setupInvocation
		return report, failure("preflight.failed", "required host prerequisites are not ready", "run "+setupInvocation)
	}
	return report, err
}

// CheckHost verifies the installed baseline without selecting a context or
// collecting wheel requirements for a setup plan.
func (s Service) CheckHost(ctx context.Context) (*Report, error) {
	if err := s.available(ctx); err != nil {
		return nil, err
	}
	view, err := s.ports.Setup.ReadSetup(ctx)
	if err != nil {
		return nil, err
	}
	current, err := s.inspect(ctx, view, StorageView{}, "", false, false)
	current.report.Route = ""
	if err == nil {
		err = s.stateRoot(ctx, &current.report)
	}
	current.report.Outcome = "not-ready"
	if err != nil {
		current.report.Next = remedyInvocation(err)
		return &current.report, err
	}
	if current.ready() && PendingScope(current.report) == "" {
		current.report.Outcome = "ready"
		return &current.report, nil
	}
	current.report.Next = setupInvocation
	return &current.report, failure("preflight.failed", "required host prerequisites are not ready", "run "+setupInvocation)
}

func (s Service) Setup(ctx context.Context, request SetupRequest) (*Report, error) {
	report, err := s.setup(ctx, request)
	if report != nil && err != nil {
		report.Next = remedyInvocation(err)
	}
	return report, err
}

// remedyInvocation is the bootwright command a refusal's remediation runs,
// so a result never offers a command its diagnostic does not.
func remedyInvocation(err error) string {
	for _, reported := range diagnostics.Of(err) {
		command := "run " + setupInvocation
		if index := strings.Index(reported.Remediation, command); index >= 0 && !strings.HasPrefix(reported.Remediation[index+len(command):], " -") {
			return setupInvocation
		}
	}
	return ""
}

func (s Service) setup(ctx context.Context, request SetupRequest) (*Report, error) {
	if err := s.available(ctx); err != nil {
		return nil, err
	}
	view, err := s.ports.Setup.ReadSetup(ctx)
	if err != nil {
		return nil, err
	}
	if request.DryRun {
		current, err := s.inspect(ctx, view, StorageView{}, "", true, false)
		current.report.DryRun = true
		current.report.Route = ""
		current.report.Table = nil
		current.report.Files = slices.Clone(s.ports.Lock.Files)
		if err != nil {
			current.report.Outcome = "not-ready"
			return &current.report, err
		}
		if current.ready() {
			current.report.Outcome = "unchanged"
			current.report.Next = "bootwright context init --name <name> --input-dir <dir>"
		} else {
			current.plan()
			current.report.Next = setupInvocation
		}
		err = s.stateRoot(ctx, &current.report)
		if err != nil {
			current.report.Outcome = "not-ready"
		}
		return &current.report, err
	}
	current, err := s.inspect(ctx, view, StorageView{}, InspectionPhase, true, false)
	if err != nil {
		return nil, err
	}
	if current.ready() {
		current.report.Outcome = "unchanged"
		return &current.report, nil
	}
	if err := s.preview(ctx, &current); err != nil {
		return &current.report, err
	}
	current.plan()
	if s.options.Presenter == nil {
		return &current.report, failure("controller.install", "setup plan presentation is not configured", "")
	}
	if err := s.options.Presenter.PresentControllerPlan(ctx, cloneReport(current.report)); err != nil {
		return nil, err
	}
	current.report.PlanPresented = true
	current.report.PresentedWarnings = len(current.report.Warnings)
	if !request.SkipConfirmation {
		if s.options.Confirmer == nil {
			return &current.report, failure("controller.install", "install requires confirmation", "review the plan and repeat with --yes")
		}
		if err := s.options.Confirmer.Confirm(ctx, "install", "this host"); err != nil {
			return &current.report, err
		}
	}
	if err := ctx.Err(); err != nil {
		return &current.report, err
	}
	return s.execute(ctx, current)
}

// stateRoot reports whether this build can use the store's state root and
// returns the store's refusal of one it cannot use.
func (s Service) stateRoot(ctx context.Context, report *Report) error {
	inspector, ok := s.storage.(StateRootInspector)
	if !ok {
		if report.DryRun {
			return nil
		}
		report.setCheck(unverified("state-root", "a qualified private state root", HostScope))
		return failure("preflight.unknown", "state-root inspection is unavailable", "use a build that supports state-root inspection on this host")
	}
	inspection, err := inspector.InspectStateRoot(ctx)
	if err != nil {
		return err
	}
	report.setCheck(Check{ID: "state-root", Required: inspection.Required, Observed: inspection.Observed, Status: inspection.Status, Scope: HostScope})
	return inspection.Refusal
}

const InstallDeadline = time.Hour

func (s Service) clock() time.Time {
	if s.options.Now != nil {
		return s.options.Now()
	}
	return time.Now()
}

func (s Service) now() string { return s.clock().UTC().Format(time.RFC3339Nano) }

// report never fails an operation: progress is presentation only.
func (s Service) report(ctx context.Context, report *Report, event ProgressEvent) {
	if s.options.Progress == nil || ctx.Err() != nil {
		return
	}
	report.ProgressPresented = true
	s.options.Progress.ReportProgress(ctx, event)
}

func cloneReport(report Report) Report {
	report.Checks = slices.Clone(report.Checks)
	report.Tools = slices.Clone(report.Tools)
	for index := range report.Tools {
		report.Tools[index].SelectedBy = slices.Clone(report.Tools[index].SelectedBy)
	}
	report.Actions = slices.Clone(report.Actions)
	report.Dependencies = slices.Clone(report.Dependencies)
	report.Warnings = slices.Clone(report.Warnings)
	report.Files = slices.Clone(report.Files)
	if report.Preview != nil {
		preview := *report.Preview
		preview.Notes = slices.Clone(preview.Notes)
		report.Preview = &preview
	}
	return report
}

// setCheck settles a check in place, or appends one that was not declared up
// front, so the report keeps catalog order either way.
func (r *Report) setCheck(check Check) {
	for index := range r.Checks {
		if r.Checks[index].ID == check.ID {
			r.Checks[index] = check
			return
		}
	}
	r.Checks = append(r.Checks, check)
}

func checkEvent(phase string, check Check) ProgressEvent {
	status := "unknown"
	switch check.Status {
	case "ready":
		status = "ok"
	case "not-ready":
		status = "failed"
	}
	return ProgressEvent{Phase: phase, Action: check.ID, Status: status, Detail: check.Summary()}
}

func unverified(id, required, scope string) Check {
	return Check{id, required, "not verified", "not-ready", scope, ""}
}

func readiness(id, required string, ready bool, scope string) Check {
	if ready {
		return Check{id, required, required, "ready", scope, ""}
	}
	return Check{id, required, "missing or unverified", "not-ready", scope, ""}
}

func setupCommand() string { return "run " + setupInvocation }

const setupInvocation = "bootwright install"

func stageInvocation(name string) string {
	return "bootwright apply --stage controller --context " + name
}

func stageCommand(name string) string { return "run " + stageInvocation(name) }

func failure(code, message, remediation string) error {
	return diagnostics.NewFailureWithRemediation(code, message, "", remediation)
}
