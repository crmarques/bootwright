package prerequisites

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
)

const (
	packagesStep = "native-packages"
	runtimeStep  = "runtime"
	receiptStep  = "receipt"
	discardStep  = "discard-setup-state"
	unknownRepo  = "unknown"
)

// requested is what setup asks of dnf: the missing baseline and, without a
// qualified interpreter, the python(abi) the preview chose.
func (i inspection) requested() []string {
	requested := slices.Clone(i.missing)
	if i.python != "" {
		requested = append(requested, i.python)
	}
	return requested
}

// preview runs dnf's own --assumeno preview as root before confirmation. With
// no qualified interpreter, the first locked ABI whose preview resolves is the
// one setup installs.
func (s Service) preview(ctx context.Context, current *inspection) error {
	if len(current.requested()) == 0 {
		return nil
	}
	candidates := slices.Clone(current.candidates)
	if len(candidates) == 0 {
		candidates = []string{""}
	}
	event := ProgressEvent{Phase: ResolutionPhase, Action: packagesStep, Status: "running", Detail: "dnf install --assumeno"}
	s.report(ctx, &current.report, event)
	var last error
	for _, candidate := range candidates {
		requested := slices.Clone(current.missing)
		if candidate != "" {
			requested = append(requested, candidate)
		}
		preview, err := s.ports.Packages.Preview(ctx, requested)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			last = err
			continue
		}
		if candidate != "" && candidate != current.python {
			current.python, current.abi = candidate, abiOf(candidate)
			if current.wheels, err = s.ports.Python.Wheels(ctx, current.abi); err != nil {
				return err
			}
		}
		current.report.Preview = &preview
		event.Status, event.Detail = "ok", strings.Join(requested, ", ")
		s.report(ctx, &current.report, event)
		return nil
	}
	event.Status, event.Detail = "failed", ""
	s.report(ctx, &current.report, event)
	return last
}

// plan writes the plan rows: discard-setup-state, resume, native-packages
// and runtime, or only the receipt when nothing else is missing.
func (i *inspection) plan() {
	actions := []string{}
	if i.view.Foreign != "" {
		actions = append(actions, DiscardForeignSetupAction(i.view.Foreign))
	}
	if pending := i.view.State.Pending; pending != nil {
		step := "setup"
		if len(pending.Actions) != 0 {
			step = pending.Actions[0]
		}
		actions = append(actions, InterruptedSetupNotice(pending.ID, step))
	}
	requested := i.requested()
	if len(requested) != 0 {
		actions = append(actions, "Install host packages with dnf: "+strings.Join(requested, ", "))
	}
	if !i.runtimeReady {
		actions = append(actions, i.runtimeAction())
	}
	if len(requested) == 0 && i.runtimeReady {
		actions = append(actions, "Record the setup receipt")
	}
	i.report.Actions, i.report.Dependencies = actions, requested
}

func (i inspection) runtimeAction() string {
	interpreter := "Python " + i.abi
	if i.interpreter.Path != "" {
		interpreter += " at " + i.interpreter.Path
	}
	download, cached := 0, 0
	var size int64
	for _, wheel := range i.wheels {
		if wheel.Cached {
			cached++
		} else {
			download++
			size += wheel.Size
		}
	}
	verb := "Prepare"
	if i.dryRun {
		verb = "Verify or prepare"
	}
	return fmt.Sprintf("%s the controller runtime: %s, ansible-core %s: %d of %d wheels to download (%.1f MB), %d cached",
		verb, interpreter, i.ansibleCore, download, len(i.wheels), float64(size)/1e6, cached)
}

func (i inspection) steps() []string {
	steps := []string{}
	if len(i.requested()) != 0 {
		steps = append(steps, packagesStep)
	}
	if !i.runtimeReady {
		steps = append(steps, runtimeStep)
	}
	return append(steps, receiptStep)
}

func (i inspection) planDigest(lock RuntimeLock) string {
	sum := sha256.Sum256([]byte(strings.Join(append([]string{SetupStateFormat, "plan", lock.Digest, i.interpreter.Path, i.runtime.Digest}, i.requested()...), "\x00")))
	return hex.EncodeToString(sum[:])
}

// execute repeats the inspection under the setup lock and refuses a host that
// moved since the plan was confirmed, then performs the plan.
func (s Service) execute(ctx context.Context, approved inspection) (*Report, error) {
	current := approved
	err := s.ports.Setup.MutateSetup(ctx, func(tx SetupTransaction) error {
		fresh, err := s.inspect(ctx, tx.View(), StorageView{}, "", false)
		if err != nil {
			return err
		}
		if !approved.samePlan(fresh) {
			return failure("controller.conflict", "the host changed after the setup plan was confirmed", setupCommand())
		}
		current.view = fresh.view
		return s.apply(ctx, tx, &current)
	})
	if err != nil {
		current.report.Outcome = "planned"
		if len(current.report.Progress) != 0 {
			current.report.Outcome = "incomplete"
		}
		return &current.report, err
	}
	current.report.Outcome = "changed"
	for index := range current.report.Checks {
		check := &current.report.Checks[index]
		if check.Status != "ready" {
			check.Status, check.Observed = "ready", check.Required
		}
	}
	return &current.report, nil
}

// apply records pending intent before any effect, installs, builds and
// publishes the runtime, then writes the receipt that clears pending.
func (s Service) apply(ctx context.Context, tx SetupTransaction, current *inspection) error {
	if current.view.Foreign != "" {
		if err := tx.DiscardForeign(ctx); err != nil {
			return err
		}
		s.report(ctx, &current.report, ProgressEvent{Phase: SetupPhase, Action: discardStep, Status: "changed"})
	}
	state := tx.View().State
	previous := state.Receipt
	id, run, err := tx.OpenRun(ctx)
	if err != nil {
		return err
	}
	defer run.Close()
	current.report.LogLocation = run.Location()
	if s.options.Progress != nil && ctx.Err() == nil && current.report.LogLocation != "" {
		s.options.Progress.ReportLogLocation(ctx, current.report.LogLocation)
	}
	steps := current.steps()
	state.Format = SetupStateFormat
	state.Pending = &PendingSetup{ID: id, PlanDigest: current.planDigest(s.ports.Lock), Actions: slices.Clone(steps), StartedAt: s.now()}
	current.report.Progress = make([]ActionProgress, len(steps))
	for index, step := range steps {
		current.report.Progress[index] = ActionProgress{ID: step, Phase: "planned"}
	}
	if err := tx.Record(ctx, state); err != nil {
		return err
	}
	var installed []HostPackageEvidence
	for index, step := range steps {
		progress := func(status string) {
			s.report(ctx, &current.report, ProgressEvent{Phase: SetupPhase, Action: step, Status: status, Step: index + 1, Steps: len(steps)})
		}
		current.report.Progress[index].Phase = "intent"
		progress("running")
		var err error
		switch step {
		case packagesStep:
			installed, err = s.install(ctx, current, run)
		case runtimeStep:
			err = s.prepareRuntime(ctx, tx, current)
		case receiptStep:
			err = s.recordReceipt(ctx, tx, current, state, id, installed, previous)
		}
		if err != nil {
			current.report.Progress[index] = ActionProgress{ID: step, Phase: "observed", Outcome: "failed"}
			progress("failed")
			return err
		}
		current.report.Progress[index] = ActionProgress{ID: step, Phase: "observed", Outcome: "changed"}
		progress("changed")
		if step != receiptStep {
			state.Pending.Actions = slices.Clone(steps[index+1:])
			if err := tx.Record(ctx, state); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s Service) install(ctx context.Context, current *inspection, run RunOutput) ([]HostPackageEvidence, error) {
	requested := current.requested()
	evidence, err := s.ports.Packages.Install(ctx, requested, run)
	if err != nil {
		return nil, err
	}
	still, err := s.ports.Packages.Missing(ctx, requested)
	if err != nil {
		return nil, err
	}
	if len(still) != 0 {
		return nil, failure("controller.setup", "dnf finished but nothing provides "+strings.Join(still, ", "), "check the host's dnf configuration, then run bootwright setup again")
	}
	return evidence, nil
}

func (s Service) prepareRuntime(ctx context.Context, tx SetupTransaction, current *inspection) error {
	interpreter := current.interpreter
	if interpreter.Path == "" {
		var err error
		if interpreter, err = s.ports.Python.Select(ctx, ""); err != nil {
			return err
		}
	}
	if err := s.ports.Python.Fetch(ctx, interpreter.ABI, current.route()); err != nil {
		return err
	}
	runtime, err := s.ports.Python.Build(ctx, tx, interpreter)
	if err != nil {
		return err
	}
	current.interpreter, current.abi, current.runtime, current.runtimeReady = interpreter, interpreter.ABI, runtime, true
	return nil
}

func (s Service) recordReceipt(ctx context.Context, tx SetupTransaction, current *inspection, state SetupState, id string, installed []HostPackageEvidence, previous *HostReceipt) error {
	if !current.runtimeReady || current.runtime.Digest == "" {
		return failure("controller.setup", "the controller runtime could not be verified after setup", setupCommand()+" again")
	}
	requirements, _ := BaselineHostRequirements("")
	if current.python != "" {
		requirements = append(requirements, current.python)
	}
	present, err := s.ports.Packages.Present(ctx, requirements)
	if err != nil {
		return err
	}
	packages := make([]ReceiptPackage, 0, len(present))
	for _, evidence := range present {
		packages = append(packages, receiptPackage(evidence, id, installed, previous))
	}
	interpreter, release := current.interpreter, current.release
	receipt := HostReceipt{
		Platform: ReceiptPlatform{ID: release.ID, VersionID: release.VersionID, Like: slices.Clone(release.Like), Architecture: release.Architecture,
			Glibc: interpreter.Glibc, Tested: tested(release, s.ports.Lock.Tested)},
		Lock: s.ports.Lock.Digest,
		Runtime: ReceiptRuntime{Digest: current.runtime.Digest, Interpreter: interpreter.Path, Python: interpreter.Version,
			ABI: interpreter.ABI, AnsibleCore: current.runtime.AnsibleCore},
		Closure:  RuntimeClosureDigest(s.ports.Lock.Digest, interpreter.ABI),
		Packages: packages,
	}
	if state.Runtimes.Current != current.runtime.Digest {
		if state.Runtimes.Current != "" {
			state.Runtimes.Previous = state.Runtimes.Current
		}
		state.Runtimes.Current = current.runtime.Digest
	}
	state.Pending, state.Receipt = nil, &receipt
	return tx.Record(ctx, state)
}

// receiptPackage names who installed a provider: this run, the run an earlier
// receipt recorded for the same provider, or nobody Bootwright knows of.
func receiptPackage(evidence HostPackageEvidence, id string, installed []HostPackageEvidence, previous *HostReceipt) ReceiptPackage {
	item := ReceiptPackage{Requirement: evidence.Requirement, RequiredBy: []string{"setup"}, Provider: evidence.Package,
		Repository: evidence.Repository, InstalledBy: PresentBeforeBootwright}
	if item.Repository == "" {
		item.Repository = unknownRepo
	}
	if strings.HasPrefix(evidence.Requirement, "python(abi)") {
		item.RequiredBy = []string{runtimeStep}
	}
	if slices.ContainsFunc(installed, func(other HostPackageEvidence) bool {
		return other.Requirement == evidence.Requirement && other.Package == evidence.Package
	}) {
		item.InstalledBy = id
	} else if previous != nil {
		for _, recorded := range previous.Packages {
			if recorded.Requirement == evidence.Requirement && recorded.Provider == evidence.Package {
				item.InstalledBy = recorded.InstalledBy
			}
		}
	}
	return item
}
