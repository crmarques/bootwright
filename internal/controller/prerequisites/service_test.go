package prerequisites

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

const testPython = "/usr/bin/python3.14"

var testLock = RuntimeLock{
	Digest: strings.Repeat("1", 64), AnsibleCore: "2.21.5", ABIs: []string{"3.14", "3.13", "3.12"}, GlibcFloor: "2.34",
	Tested: []TestedRelease{{ID: "fedora", Versions: []string{"43", "44"}}, {ID: "rhel", Versions: []string{"9", "10"}}},
	Files: []LockedFile{
		{ABI: "3.14", Filename: "ansible_core-2.21.5-py3-none-any.whl", URL: "https://files.pythonhosted.org/a/ansible_core-2.21.5-py3-none-any.whl", SHA256: strings.Repeat("a", 64)},
		{Filename: "pip-26.0.1-py3-none-any.whl", URL: "https://files.pythonhosted.org/p/pip-26.0.1-py3-none-any.whl", SHA256: strings.Repeat("b", 64)},
	},
}

// setupFixture is a host whose every port records what it was asked, in one
// event log, so a test proves what ran before what.
type setupFixture struct {
	t        *testing.T
	service  Service
	store    *memorySetup
	host     *releaseHost
	packages *hostPackages
	python   *osPython
	events   []string
	plan     Report
	confirm  error
}

func newSetupFixture(t *testing.T) *setupFixture {
	t.Helper()
	f := &setupFixture{t: t}
	identity, err := controller.NewInstalledHostIdentity(controller.LinuxInstalledIdentityV1, strings.Repeat("1", 32), "11111111-2222-4333-8444-555555555555", "22222222-3333-4444-8555-666666666666")
	if err != nil {
		t.Fatal(err)
	}
	f.host = &releaseHost{owner: f, identity: identity, release: HostRelease{ID: "fedora", VersionID: "44", Architecture: "amd64"}}
	f.store = &memorySetup{owner: f}
	f.packages = &hostPackages{owner: f, present: map[string]bool{"openssh-clients": true, "podman": true}, refuse: map[string]bool{}}
	f.python = &osPython{owner: f, installed: true, ready: true}
	f.service = New(noController{}, noCompiler{}, f.host, Ports{Setup: f.store, Packages: f.packages, Python: f.python, Lock: testLock}, Options{
		Confirmer: f, Presenter: f, Now: func() time.Time { return time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC) },
	})
	return f
}

// prepared gives the host the receipt a completed setup of the current
// runtime leaves.
func (f *setupFixture) prepared() {
	f.store.view = SetupView{Exists: true, State: SetupState{Format: SetupStateFormat, Receipt: &HostReceipt{
		Lock: testLock.Digest, Runtime: ReceiptRuntime{Digest: RuntimeDigest(testLock.Digest, "3.14", testPython), Interpreter: testPython, ABI: "3.14"},
	}}}
}

func (f *setupFixture) record(event string) { f.events = append(f.events, event) }

func (f *setupFixture) count(event string) int {
	count := 0
	for _, recorded := range f.events {
		if recorded == event {
			count++
		}
	}
	return count
}

func (f *setupFixture) Confirm(context.Context, string, string) error {
	f.record("confirm")
	return f.confirm
}

func (f *setupFixture) PresentControllerScope(context.Context, string, Report) error { return nil }

func (f *setupFixture) PresentControllerPlan(_ context.Context, report Report) error {
	f.record("plan")
	f.plan = report
	return nil
}

type releaseHost struct {
	owner    *setupFixture
	identity controller.InstalledHostIdentity
	release  HostRelease
	preload  bool
}

func (h *releaseHost) LoaderPreload(context.Context) (bool, error) { return h.preload, nil }

func (h *releaseHost) Platform(context.Context) (Platform, error) {
	return Platform{OS: h.release.ID, Release: h.release.VersionID, Architecture: h.release.Architecture}, nil
}

func (h *releaseHost) Identity(context.Context) (controller.InstalledHostIdentity, error) {
	h.owner.record("identity")
	return h.identity, nil
}

func (h *releaseHost) Release(context.Context) (HostRelease, error) {
	h.owner.record("release")
	return h.release, nil
}

type hostPackages struct {
	owner   *setupFixture
	present map[string]bool
	refuse  map[string]bool
	preview string
}

func (p *hostPackages) Qualify(context.Context) error {
	p.owner.record("qualify")
	return nil
}

func (p *hostPackages) Missing(_ context.Context, requirements []string) ([]string, error) {
	p.owner.record("missing")
	missing := []string{}
	for _, requirement := range requirements {
		if !p.present[requirement] {
			missing = append(missing, requirement)
		}
	}
	return missing, nil
}

func (p *hostPackages) Installed(ctx context.Context, requirements []string) ([]HostPackageEvidence, error) {
	p.owner.record("installed")
	return p.Present(ctx, requirements)
}

func (p *hostPackages) Present(_ context.Context, requirements []string) ([]HostPackageEvidence, error) {
	evidence := []HostPackageEvidence{}
	for _, requirement := range requirements {
		if p.present[requirement] {
			evidence = append(evidence, HostPackageEvidence{Requirement: requirement, Package: provider(requirement), Repository: "fedora"})
		}
	}
	return evidence, nil
}

func provider(requirement string) string {
	name := strings.NewReplacer("(abi) = ", "", " ", "").Replace(requirement)
	return name + "-1.0-1.fc44.x86_64"
}

func (p *hostPackages) Preview(_ context.Context, missing []string) (HostPackagePreview, error) {
	p.owner.record("preview " + strings.Join(missing, ","))
	for _, requirement := range missing {
		if p.refuse[requirement] {
			return HostPackagePreview{}, failure("controller.install", "no package in the host's repositories provides "+requirement, "Enable the repository that provides it in the host's dnf configuration, then run setup again.")
		}
	}
	return HostPackagePreview{Text: p.preview, Notes: []string{"this install also updates core packages (glibc); dnf applies it as for any package install"}}, nil
}

func (p *hostPackages) Install(_ context.Context, missing []string, _ RunOutput) ([]HostPackageEvidence, error) {
	p.owner.record("install " + strings.Join(missing, ","))
	evidence := []HostPackageEvidence{}
	for _, requirement := range missing {
		p.present[requirement] = true
		evidence = append(evidence, HostPackageEvidence{Requirement: requirement, Package: provider(requirement), Repository: "fedora"})
	}
	return evidence, nil
}

type osPython struct {
	owner     *setupFixture
	installed bool
	ready     bool
	glibcLow  bool
	cached    bool
	named     map[string][]string
	mirrors   []string
	exported  []string
	buildErr  error
	fetchErr  error
}

func (p *osPython) interpreter() PythonInterpreter {
	return PythonInterpreter{Path: testPython, ABI: "3.14", Version: "3.14.8", Glibc: "2.43"}
}

func (p *osPython) runtime() Runtime {
	return Runtime{Digest: RuntimeDigest(testLock.Digest, "3.14", testPython), LockDigest: testLock.Digest, Interpreter: p.interpreter(), AnsibleCore: testLock.AnsibleCore}
}

func (p *osPython) Select(context.Context, string) (PythonInterpreter, error) {
	p.owner.record("select")
	if p.glibcLow {
		return PythonInterpreter{}, errors.Join(ErrNoPythonInterpreter, ErrGlibcBelowFloor)
	}
	if !p.installed && !p.owner.packages.present["python3.14"] && !p.owner.packages.present["python3.13"] {
		return PythonInterpreter{}, ErrNoPythonInterpreter
	}
	return p.interpreter(), nil
}

func (p *osPython) Wheels(context.Context, string) ([]RuntimeWheel, error) {
	p.owner.record("wheels")
	return []RuntimeWheel{{Filename: "ansible_core-2.21.5-py3-none-any.whl", Size: 2_400_000, Cached: p.cached}, {Filename: "pip-26.0.1-py3-none-any.whl", Size: 1_800_000, Cached: true}}, nil
}

func (p *osPython) RuntimeWheels(_ context.Context, digest string) ([]string, error) {
	named, found := p.named[digest]
	if !found {
		return nil, ErrRuntimeUnavailable
	}
	return named, nil
}

func (p *osPython) Fetch(_ context.Context, _ string, _ SetupEgress, mirror WheelMirror) error {
	p.owner.record("fetch")
	p.mirrors = append(p.mirrors, mirror.String())
	return p.fetchErr
}

func (p *osPython) Export(_ context.Context, destination string, _ SetupEgress, mirror WheelMirror) (WheelExport, error) {
	p.owner.record("export")
	p.exported = append(p.exported, destination+" "+mirror.String())
	return WheelExport{Directory: destination, Files: []ExportedWheel{{Filename: "ansible_core-2.21.5-py3-none-any.whl", SHA256: strings.Repeat("a", 64), Size: 2_400_000, Origin: OriginDownload}}}, nil
}

func (p *osPython) Inspect(context.Context, PythonInterpreter) (Runtime, bool, error) {
	p.owner.record("inspect")
	return p.runtime(), p.ready, nil
}

func (p *osPython) Build(ctx context.Context, publisher RuntimePublisher, _ PythonInterpreter) (Runtime, error) {
	p.owner.record("build")
	if p.buildErr != nil {
		return Runtime{}, p.buildErr
	}
	stage, err := publisher.StageRuntime(ctx)
	if err != nil {
		return Runtime{}, err
	}
	if _, err := publisher.PublishRuntime(ctx, stage, p.runtime().Digest); err != nil {
		return Runtime{}, err
	}
	p.ready = true
	return p.runtime(), nil
}

func (p *osPython) WithPython(context.Context, Runtime, func(PythonLaunch) error) error {
	return errors.New("no launch in a setup test")
}

type memorySetup struct {
	owner    *setupFixture
	view     SetupView
	refusal  error
	recorded []SetupState
	// published, held and pruned stand in for the runtimes directory and
	// the wheel cache.
	published []string
	held      map[string]bool
	kept      []string
}

func (m *memorySetup) AwaitSetup(_ context.Context, callback func(SetupTransaction) error) error {
	m.owner.record("await")
	return callback(memoryTransaction{m})
}

func (m *memorySetup) ReadSetup(context.Context) (SetupView, error) {
	m.owner.record("read")
	return m.view, m.refusal
}

func (m *memorySetup) MutateSetup(_ context.Context, callback func(SetupTransaction) error) error {
	m.owner.record("lock")
	return callback(memoryTransaction{m})
}

func (m *memorySetup) HoldRuntime(context.Context, string) (RuntimeHold, error) {
	return nil, ErrRuntimeUnavailable
}

type memoryTransaction struct{ m *memorySetup }

type memoryStage string

func (s memoryStage) Path() string { return string(s) }

type memoryRun struct{}

func (memoryRun) Write(data []byte) (int, error) { return len(data), nil }
func (memoryRun) Location() string               { return "/var/lib/bootwright/setup/runs/setup-000043" }
func (memoryRun) Close() error                   { return nil }

func (t memoryTransaction) View() SetupView { return t.m.view }

func (t memoryTransaction) Record(_ context.Context, value SetupState) error {
	t.m.owner.record("record")
	t.m.recorded = append(t.m.recorded, value)
	t.m.view = SetupView{Exists: true, State: value}
	return nil
}

func (t memoryTransaction) DiscardForeign(context.Context) error {
	t.m.owner.record("discard")
	t.m.view, t.m.published = SetupView{}, nil
	t.m.owner.python.ready, t.m.owner.python.cached = false, false
	return nil
}

func (t memoryTransaction) OpenRun(context.Context) (string, SetupRun, error) {
	return "setup-000043", memoryRun{}, nil
}

func (t memoryTransaction) StageRuntime(context.Context) (RuntimeStage, error) {
	return memoryStage("/var/lib/bootwright/setup/runtimes/.stage-1"), nil
}

func (t memoryTransaction) PublishRuntime(context.Context, RuntimeStage, string) (bool, error) {
	t.m.owner.record("publish")
	return true, nil
}

func (t memoryTransaction) DiscardRuntime(context.Context, RuntimeStage) error { return nil }
func (t memoryTransaction) RetireRuntime(_ context.Context, digest string) (bool, error) {
	if t.m.held[digest] {
		return false, nil
	}
	t.m.owner.record("retire " + digest[:4])
	t.m.published = slices.DeleteFunc(t.m.published, func(name string) bool { return name == digest })
	return true, nil
}
func (t memoryTransaction) Runtimes(context.Context) ([]string, error) {
	return slices.Clone(t.m.published), nil
}
func (t memoryTransaction) WheelPath(context.Context, string, string) (string, error) {
	return "", errors.New("no wheel path in a setup test")
}
func (t memoryTransaction) PruneWheels(_ context.Context, keep []string) (int, error) {
	t.m.kept = slices.Clone(keep)
	return 0, nil
}

type noController struct{}

func (noController) ReadController(_ context.Context, _ string, read func(StorageView) error) error {
	return read(StorageView{})
}

type noCompiler struct{}

func (noCompiler) Compile(context.Context, desiredstate.Sources) (*compilation.State, *compilation.Report, error) {
	return nil, nil, errors.New("setup selects no context")
}

func codeOf(err error) string {
	if found := diagnostics.Of(err); len(found) != 0 {
		return found[0].Code
	}
	return ""
}

// effects are the events that change the host or the store, or reach the
// network or a root dnf.
func (f *setupFixture) effects() []string {
	var effects []string
	for _, event := range f.events {
		for _, prefix := range []string{"lock", "record", "discard", "preview", "install", "fetch", "build", "publish"} {
			if strings.HasPrefix(event, prefix) {
				effects = append(effects, event)
			}
		}
	}
	return effects
}

func TestAReadyHostMakesNoRequestAndNoWrite(t *testing.T) {
	f := newSetupFixture(t)
	f.prepared()
	report, err := f.service.Setup(context.Background(), SetupRequest{})
	if err != nil || report.Outcome != "unchanged" {
		t.Fatalf("Setup = %+v, %v", report, err)
	}
	if effects := f.effects(); len(effects) != 0 || f.count("confirm") != 0 || f.count("plan") != 0 || f.count("wheels") != 0 {
		t.Fatalf("a ready host ran %v", f.events)
	}
	for _, check := range report.Checks {
		if check.Status != "ready" {
			t.Fatalf("check %s is %s on a ready host", check.ID, check.Status)
		}
	}
}

func TestThePlanShowsTheDnfPreviewVerbatim(t *testing.T) {
	f := newSetupFixture(t)
	f.prepared()
	f.packages.present["podman"] = false
	f.packages.preview = "Dependencies resolved.\n Package   Arch    Version   Repository   Size\nInstalling:\n podman    x86_64  5:5.8.1-1.fc44  updates  17 M\n\nOperation aborted.\n"
	report, err := f.service.Setup(context.Background(), SetupRequest{})
	if err != nil || report.Outcome != "changed" {
		t.Fatalf("Setup = %+v, %v", report, err)
	}
	if f.plan.Preview == nil || f.plan.Preview.Text != f.packages.preview || len(f.plan.Preview.Notes) != 1 {
		t.Fatalf("the presented plan's preview is %+v", f.plan.Preview)
	}
	if !slices.Equal(f.plan.Dependencies, []string{"podman"}) || !slices.Contains(f.plan.Actions, "Install host packages with dnf: podman") {
		t.Fatalf("the plan is %v, %v", f.plan.Dependencies, f.plan.Actions)
	}
	order := []string{"preview podman", "plan", "confirm", "lock", "record", "install podman"}
	position := 0
	for _, event := range f.events {
		if position < len(order) && event == order[position] {
			position++
		}
	}
	if position != len(order) {
		t.Fatalf("events %v do not run %v in order", f.events, order)
	}
	receipt := f.store.view.State.Receipt
	if receipt == nil || f.store.view.State.Pending != nil {
		t.Fatalf("the setup left %+v", f.store.view.State)
	}
	installedBy := map[string]string{}
	for _, item := range receipt.Packages {
		installedBy[item.Requirement] = item.InstalledBy
	}
	if installedBy["podman"] != "setup-000043" || installedBy["openssh-clients"] != PresentBeforeBootwright {
		t.Fatalf("the receipt's packages are %+v", receipt.Packages)
	}
}

func TestAnInterruptedSetupIsReinspectedAfterConfirmation(t *testing.T) {
	f := newSetupFixture(t)
	f.prepared()
	f.python.ready = false
	f.store.view.State.Pending = &PendingSetup{ID: "setup-000042", PlanDigest: strings.Repeat("2", 64), Actions: []string{"runtime", "receipt"}, StartedAt: "2026-10-13T10:00:00Z"}
	report, err := f.service.Setup(context.Background(), SetupRequest{})
	if err != nil || report.Outcome != "changed" {
		t.Fatalf("Setup = %+v, %v", report, err)
	}
	notice := InterruptedSetupNotice("setup-000042", "runtime")
	if !slices.Contains(f.plan.Actions, notice) {
		t.Fatalf("the plan %v does not say %q", f.plan.Actions, notice)
	}
	confirm := slices.Index(f.events, "confirm")
	if confirm < 0 || !slices.Contains(f.events[confirm:], "select") || !slices.Contains(f.events[confirm:], "missing") || f.count("build") != 1 {
		t.Fatalf("the host was not inspected again after confirmation: %v", f.events)
	}
	if first := f.store.recorded[0]; first.Pending == nil || first.Pending.ID != "setup-000043" {
		t.Fatalf("the setup recorded %+v before its effects", first.Pending)
	}
	if final := f.store.view.State; final.Pending != nil || final.Receipt == nil || final.Runtimes.Current != f.python.runtime().Digest {
		t.Fatalf("the setup left %+v", final)
	}
}

// A setup that fails before any dnf transaction began, as a wheel fetch does,
// leaves no unknown outcome: it clears its pending intent, so the runtime the
// kept receipt names still launches. One that ran dnf first keeps the intent.
func TestAFailureBeforeAnyDnfTransactionClearsThePendingIntent(t *testing.T) {
	for name, test := range map[string]struct {
		missing string
		pending bool
	}{
		"a failed wheel fetch":               {},
		"a failed wheel fetch after dnf ran": {missing: "podman", pending: true},
	} {
		t.Run(name, func(t *testing.T) {
			f := newSetupFixture(t)
			f.prepared()
			kept := *f.store.view.State.Receipt
			f.python.ready = false
			if test.missing != "" {
				f.packages.present[test.missing] = false
			}
			f.python.fetchErr = failure("controller.install", "the wheel origin did not answer", "pass --wheel-mirror")
			if _, err := f.service.Setup(context.Background(), SetupRequest{}); codeOf(err) != "controller.install" {
				t.Fatalf("Setup = %v", err)
			}
			state := f.store.view.State
			if first := f.store.recorded[0]; first.Pending == nil {
				t.Fatalf("the setup recorded no intent before its effects: %+v", first)
			}
			if (state.Pending != nil) != test.pending || state.Receipt == nil || state.Receipt.Lock != kept.Lock || state.Receipt.Runtime != kept.Runtime {
				t.Fatalf("the failed setup left %+v", state)
			}
			if err := SetupLaunchRefusal(f.store.view); (err != nil) != test.pending {
				t.Fatalf("a launch of the kept runtime = %v", err)
			}
		})
	}
}

func TestAnUntestedReleaseIsInformational(t *testing.T) {
	f := newSetupFixture(t)
	f.prepared()
	f.host.release.VersionID = "45"
	report, err := f.service.Setup(context.Background(), SetupRequest{})
	if err != nil || report.Outcome != "unchanged" {
		t.Fatalf("Setup = %+v, %v", report, err)
	}
	want := "Fedora 45 is newer than the releases this build was tested on (43, 44); setup checks its floors and continues"
	if len(report.Warnings) != 1 || report.Warnings[0].Code != "controller.untested-release" || report.Warnings[0].Severity != "warning" || report.Warnings[0].Message != want {
		t.Fatalf("warnings = %+v", report.Warnings)
	}
	f.host.release.VersionID = "44"
	if report, err := f.service.Setup(context.Background(), SetupRequest{}); err != nil || len(report.Warnings) != 0 {
		t.Fatalf("a tested release warned: %+v, %v", report, err)
	}
}

func TestAnUnknownFamilyRefusesBeforeEffects(t *testing.T) {
	for _, release := range []HostRelease{
		{ID: "ubuntu", VersionID: "24.04", Like: []string{"debian"}, Architecture: "amd64"},
		{ID: "fedora", VersionID: "44", Architecture: "arm64"},
	} {
		f := newSetupFixture(t)
		f.host.release = release
		_, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
		if codeOf(err) != "controller.unsupported" {
			t.Fatalf("%+v: Setup = %v", release, err)
		}
		if effects := f.effects(); len(effects) != 0 || f.count("missing") != 0 || f.count("select") != 0 || f.count("qualify") != 0 {
			t.Fatalf("%+v: a refused host ran %v", release, f.events)
		}
	}
}

func TestADerivativeIsAdmittedByIDLike(t *testing.T) {
	f := newSetupFixture(t)
	f.prepared()
	f.host.release = HostRelease{ID: "rocky", VersionID: "9.6", Like: []string{"rhel", "centos", "fedora"}, Architecture: "amd64"}
	report, err := f.service.Check(context.Background(), CheckRequest{})
	if err != nil || report.Outcome != "ready" {
		t.Fatalf("Check = %+v, %v", report, err)
	}
	if host := report.Checks[0]; host.ID != "host" || host.Status != "ready" || host.Observed != "rocky 9.6/amd64 (like rhel centos fedora)" {
		t.Fatalf("host check = %+v", host)
	}
	if len(report.Warnings) != 1 || !strings.HasPrefix(report.Warnings[0].Message, "rocky 9.6 is not among the releases this build was tested on (Fedora 43, 44; RHEL 9, 10)") {
		t.Fatalf("warnings = %+v", report.Warnings)
	}
}

func TestNoLockedInterpreterAddsThePythonABIRequirement(t *testing.T) {
	f := newSetupFixture(t)
	f.python.installed, f.python.ready = false, false
	f.packages.refuse["python3.14"] = true
	dry, err := f.service.Setup(context.Background(), SetupRequest{DryRun: true})
	if err != nil || !slices.Equal(dry.Dependencies, []string{"python3.14"}) || len(dry.Files) != len(testLock.Files) {
		t.Fatalf("dry run = %+v, %v", dry, err)
	}
	if effects := f.effects(); len(effects) != 0 || f.count("read") != 0 {
		t.Fatalf("a dry run ran %v", f.events)
	}
	f.events = nil
	report, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
	if err != nil || report.Outcome != "changed" {
		t.Fatalf("Setup = %+v, %v", report, err)
	}
	if !slices.Equal(report.Dependencies, []string{"python3.13"}) || f.count("preview python3.14") != 1 || f.count("install python3.13") != 1 {
		t.Fatalf("the setup chose %v after %v", report.Dependencies, f.events)
	}
	receipt := f.store.view.State.Receipt
	found := slices.ContainsFunc(receipt.Packages, func(item ReceiptPackage) bool {
		return item.Requirement == "python3.13" && item.InstalledBy == "setup-000043" && slices.Equal(item.RequiredBy, []string{"runtime"})
	})
	if !found {
		t.Fatalf("the receipt's packages are %+v", receipt.Packages)
	}
}

func TestAGlibcBelowTheFloorRefusesBeforeEffects(t *testing.T) {
	f := newSetupFixture(t)
	f.python.glibcLow = true
	_, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
	if codeOf(err) != "controller.unsupported" || len(f.effects()) != 0 {
		t.Fatalf("Setup = %v after %v", err, f.events)
	}
}

func TestAnotherBuildsSetupStateIsDiscardedAfterConfirmation(t *testing.T) {
	f := newSetupFixture(t)
	f.store.view = SetupView{Exists: true, Foreign: "bootwright.setup-state-v9"}
	report, err := f.service.Setup(context.Background(), SetupRequest{})
	if err != nil || report.Outcome != "changed" {
		t.Fatalf("Setup = %+v, %v", report, err)
	}
	if !slices.Contains(f.plan.Actions, DiscardForeignSetupAction("bootwright.setup-state-v9")) {
		t.Fatalf("the plan %v does not discard the other build's state", f.plan.Actions)
	}
	if confirm, discard := slices.Index(f.events, "confirm"), slices.Index(f.events, "discard"); confirm < 0 || discard < confirm {
		t.Fatalf("events = %v", f.events)
	}
}

// The discard removes the runtimes and the wheel cache with the record, so a
// healthy runtime there is rebuilt rather than named by a receipt.
func TestDiscardingAnotherBuildsStateRebuildsAHealthyRuntime(t *testing.T) {
	f := newSetupFixture(t)
	f.python.cached = true
	f.store.view = SetupView{Exists: true, Foreign: unrecognizedTestFormat}
	report, err := f.service.Setup(context.Background(), SetupRequest{})
	if err != nil || report.Outcome != "changed" {
		t.Fatalf("Setup = %+v, %v", report, err)
	}
	want := "Prepare the controller runtime: Python 3.14 at " + testPython + ", ansible-core 2.21.5: 2 of 2 wheels to download (4.2 MB), 0 cached"
	if !slices.Equal(f.plan.Actions, []string{DiscardForeignSetupAction(unrecognizedTestFormat), want}) {
		t.Fatalf("the plan is %q", f.plan.Actions)
	}
	discard, fetch, build := slices.Index(f.events, "discard"), slices.Index(f.events, "fetch"), slices.Index(f.events, "build")
	if discard < 0 || fetch < discard || build < fetch || f.count("build") != 1 || f.count("inspect") != 0 {
		t.Fatalf("events = %v", f.events)
	}
	if !f.python.ready || f.store.view.State.Receipt == nil || f.store.view.State.Runtimes.Current != f.python.runtime().Digest {
		t.Fatalf("the receipt names %+v over a runtime ready=%v", f.store.view.State, f.python.ready)
	}
}

const unrecognizedTestFormat = "content this build does not recognize"

func TestADeclinedPlanChangesNothing(t *testing.T) {
	f := newSetupFixture(t)
	f.python.ready = false
	f.confirm = errors.New("declined")
	if _, err := f.service.Setup(context.Background(), SetupRequest{}); err == nil {
		t.Fatal("a declined setup succeeded")
	}
	if effects := f.effects(); len(effects) != 0 {
		t.Fatalf("a declined setup ran %v", effects)
	}
}

func TestAStoreTheSetupReadRefusesPreviewsNothing(t *testing.T) {
	f := newSetupFixture(t)
	f.python.ready = false
	f.store.refusal = failure("context.state", "/var/lib/bootwright holds a store an earlier Bootwright build wrote (registry version 5)", "")
	report, err := f.service.Setup(context.Background(), SetupRequest{})
	if report != nil || codeOf(err) != "context.state" {
		t.Fatalf("Setup = %+v, %v", report, err)
	}
	if effects := f.effects(); len(effects) != 0 || f.count("plan") != 0 || f.count("confirm") != 0 || f.count("missing") != 0 {
		t.Fatalf("a refused store ran %v", f.events)
	}
}
