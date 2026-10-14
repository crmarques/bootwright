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

// Ports are what setup v2 owns: the setup subtree, the host's own dnf, the
// OS interpreter's runtime and the lock this build embeds.
type Ports struct {
	Setup    SetupStore
	Packages HostPackages
	Python   PythonRuntime
	Lock     RuntimeLock
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
		current, err := s.inspect(ctx, view, stored, ReadinessPhase, false)
		if err != nil {
			if len(current.report.Checks) != 0 {
				current.report.Outcome = "not-ready"
				current.report.Next = remedyInvocation(err)
				result = &current.report
			}
			return err
		}
		result = &current.report
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
	mirror, err := ParseWheelMirror(request.WheelMirror)
	if err != nil {
		return nil, failure("controller.setup", err.Error(), "pass --wheel-mirror as "+WheelMirrorUsage)
	}
	if request.DownloadOnly {
		return s.downloadOnly(ctx, request, mirror)
	}
	if err := s.available(ctx); err != nil {
		return nil, err
	}
	if request.DryRun {
		return s.dryRun(ctx, mirror)
	}
	view, err := s.ports.Setup.ReadSetup(ctx)
	if err != nil {
		return nil, err
	}
	current, err := s.inspect(ctx, view, StorageView{}, InspectionPhase, false)
	if err != nil {
		return nil, err
	}
	current.mirror = mirror
	if current.ready() {
		current.report.Outcome = "unchanged"
		return &current.report, nil
	}
	if err := s.preview(ctx, &current); err != nil {
		return &current.report, err
	}
	current.plan()
	if s.options.Presenter == nil {
		return &current.report, failure("controller.setup", "setup plan presentation is not configured", "")
	}
	if err := s.options.Presenter.PresentControllerPlan(ctx, cloneReport(current.report)); err != nil {
		return nil, err
	}
	current.report.PlanPresented = true
	if !request.SkipConfirmation {
		if s.options.Confirmer == nil {
			return &current.report, failure("controller.setup", "setup requires confirmation", "review the plan and repeat with --yes")
		}
		if err := s.options.Confirmer.Confirm(ctx, "setup", "this host"); err != nil {
			return &current.report, err
		}
	}
	if err := ctx.Err(); err != nil {
		return &current.report, err
	}
	return s.execute(ctx, current)
}

// dryRun never elevates: it reads no setup record, previews nothing with dnf
// and names what setup would install by requirement.
func (s Service) dryRun(ctx context.Context, mirror WheelMirror) (*Report, error) {
	current, err := s.inspect(ctx, SetupView{}, StorageView{}, "", true)
	if err != nil {
		return nil, err
	}
	current.mirror = mirror
	current.plan()
	current.report.Outcome, current.report.Next = "planned", setupInvocation
	current.report.Files = s.lockedFiles()
	return &current.report, s.stateRoot(ctx, &current.report)
}

func (s Service) downloadOnly(ctx context.Context, request SetupRequest, mirror WheelMirror) (*Report, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.ports.Python == nil || s.ports.Lock.Digest == "" {
		return nil, availability.ErrNotImplemented
	}
	if request.DryRun {
		return nil, failure("controller.setup", "--download-only writes the locked files, so it cannot be a dry run", "drop --dry-run, or use setup --dry-run --output json to list the locked files")
	}
	if request.Destination == "" {
		return nil, failure("controller.setup", "--download-only needs a directory to write", "pass --dest-dir <directory>")
	}
	route := inspection{selection: controller.Baseline().WithAmbientRoute(s.options.AmbientRoute)}
	export, err := s.ports.Python.Export(ctx, request.Destination, route.route(), mirror)
	if err != nil {
		return nil, err
	}
	return &Report{Route: route.routeSummary(), Outcome: "downloaded", Export: &export}, nil
}

func (s Service) lockedFiles() []LockedFile {
	return slices.Clone(s.ports.Lock.Files)
}

// stateRoot reports in a dry run whether this build can use the store's
// state root, and returns the store's refusal of one it cannot use.
func (s Service) stateRoot(ctx context.Context, report *Report) error {
	inspector, ok := s.storage.(StateRootInspector)
	if !ok {
		return nil
	}
	inspection, err := inspector.InspectStateRoot(ctx)
	if err != nil {
		return err
	}
	report.setCheck(Check{ID: "state-root", Required: inspection.Required, Observed: inspection.Observed, Status: inspection.Status, Scope: HostScope})
	return inspection.Refusal
}

func (s Service) now() string {
	now := time.Now
	if s.options.Now != nil {
		now = s.options.Now
	}
	return now().UTC().Format(time.RFC3339Nano)
}

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
	report.Actions = slices.Clone(report.Actions)
	report.Dependencies = slices.Clone(report.Dependencies)
	report.Warnings = slices.Clone(report.Warnings)
	report.Files = slices.Clone(report.Files)
	if report.Export != nil {
		export := *report.Export
		export.Files = slices.Clone(export.Files)
		report.Export = &export
	}
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
	return Check{id, required, "not verified", "not-ready", scope}
}

func readiness(id, required string, ready bool, scope string) Check {
	if ready {
		return Check{id, required, required, "ready", scope}
	}
	return Check{id, required, "missing or unverified", "not-ready", scope}
}

func setupCommand() string { return "run " + setupInvocation }

const setupInvocation = "bootwright setup"

func stageInvocation(name string) string {
	return "bootwright apply --stage controller --context " + name
}

func stageCommand(name string) string { return "run " + stageInvocation(name) }

func failure(code, message, remediation string) error {
	return diagnostics.NewFailureWithRemediation(code, message, "", remediation)
}
