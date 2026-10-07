package contexts

import (
	"context"
	"errors"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

type Service struct {
	reader     DirectoryReader
	compiler   Compiler
	repository Repository
	guard      ContextMutationGuard
	confirmer  Confirmer
	options    Options
}

func New(reader DirectoryReader, compiler Compiler, repository Repository, guard ContextMutationGuard, confirmer Confirmer, options ...Options) Service {
	s := Service{reader: reader, compiler: compiler, repository: repository, guard: guard, confirmer: confirmer}
	if len(options) != 0 {
		s.options = options[0]
	}
	return s
}

var namePattern = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$`)

func (s Service) ready(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.repository == nil {
		return availability.ErrNotImplemented
	}
	return nil
}

func validName(name string) error {
	if !namePattern.MatchString(name) {
		return StateError("context name must be a lowercase DNS label of at most 63 characters")
	}
	return nil
}

func summary(r Record, selected Selection) Summary {
	return Summary{Name: r.Name, Mode: r.Mode, Current: r.Name == selected.Name, Configured: r.Revision != ""}
}

func commitRegistry(ctx context.Context, tx Transaction, reg Registry) error {
	reg.Contexts = slices.Clone(reg.Contexts)
	slices.SortFunc(reg.Contexts, func(a, b Record) int { return strings.Compare(a.Name, b.Name) })
	return tx.Commit(ctx, reg)
}

func findRecord(reg Registry, name string) int {
	return slices.IndexFunc(reg.Contexts, func(r Record) bool { return r.Name == name })
}

func (s Service) disposition(ctx context.Context, tx Transaction, name string) (Disposition, error) {
	if s.guard == nil {
		return Disposition{}, StateError("context mutation guard is not configured")
	}
	data, err := tx.MutationState(ctx, name)
	if err != nil {
		return Disposition{}, err
	}
	result, err := s.guard.Check(ctx, data)
	if canceled := ctx.Err(); canceled != nil {
		return Disposition{}, canceled
	}
	return result, err
}

// confirmPresented asks the ordinary confirmation only after the presenter has
// shown what the command changes, immediately before the prompt, and never
// asks without showing it. It reports whether the plan was shown.
func (s Service) confirmPresented(ctx context.Context, skip bool, action, name string, present func(Presenter) error) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if skip {
		return false, nil
	}
	if s.confirmer == nil {
		return false, StateError("confirmation requires an interactive terminal or --yes")
	}
	if s.options.Presenter == nil {
		return false, StateError("context plan presentation is not configured")
	}
	if err := present(s.options.Presenter); err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return true, err
	}
	if err := s.confirmer.Confirm(ctx, action, name); err != nil {
		return true, err
	}
	return true, ctx.Err()
}

func (s Service) admit(ctx context.Context, path string) (desiredstate.Sources, string, string, *compilation.Report, error) {
	if s.reader == nil || s.compiler == nil {
		return desiredstate.Sources{}, "", "", nil, StateError("context admission is not configured")
	}
	if err := s.repository.CheckInputDirectory(ctx, path); err != nil {
		return desiredstate.Sources{}, "", "", nil, err
	}
	if err := ctx.Err(); err != nil {
		return desiredstate.Sources{}, "", "", nil, err
	}
	sources, err := s.reader.ReadDirectory(ctx, path)
	if err != nil {
		return desiredstate.Sources{}, "", "", nil, err
	}
	if err = ctx.Err(); err != nil {
		return desiredstate.Sources{}, "", "", nil, err
	}
	state, report, err := s.compiler.Compile(ctx, desiredstate.Sources{Files: slices.Clone(sources.Files), Markers: slices.Clone(sources.Markers), Roots: slices.Clone(sources.Roots)})
	if err != nil {
		return desiredstate.Sources{}, "", "", nil, err
	}
	if err = ctx.Err(); err != nil {
		return desiredstate.Sources{}, "", "", nil, err
	}
	if state == nil || report == nil || len(sources.Roots) != 1 {
		return desiredstate.Sources{}, "", "", nil, StateError("context admission returned incomplete input")
	}
	environments := state.Authored().OfKind(api.Environment)
	if len(environments) != 1 {
		return desiredstate.Sources{}, "", "", nil, StateError("context admission returned no unique Environment")
	}
	origin, ok := state.Origin(environments[0].Identity())
	if !ok || !filepath.IsAbs(origin.Path) {
		return desiredstate.Sources{}, "", "", nil, StateError("context admission returned no Environment provenance")
	}
	directory := filepath.Dir(origin.Path)
	relative, err := filepath.Rel(sources.Roots[0], directory)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return desiredstate.Sources{}, "", "", nil, StateError("Environment is outside the admitted input directory")
	}
	effectiveEnvironment, found := state.Effective().Find(api.Environment, environments[0].Name())
	if !found {
		return desiredstate.Sources{}, "", "", nil, StateError("context admission returned no effective Environment")
	}
	return sources, directory, effectiveEnvironment.Spec().Get("controller", "machineRef").Text(), report, nil
}

func (s Service) configuration(ctx context.Context, name, path string) (Configuration, error) {
	config := DefaultConfiguration(name)
	if path != "" {
		if s.options.ConfigurationReader == nil {
			return Configuration{}, ConfigurationError("Context configuration reader is not configured")
		}
		data, err := s.options.ConfigurationReader.ReadFile(ctx, path, MaxConfigurationBytes)
		if err != nil {
			return Configuration{}, reshapeFailure(err, func(reported *diagnostics.Diagnostic) {
				if reported.Remediation == "" {
					reported.Remediation = configurationFileRemediation
				}
			})
		}
		config, err = ParseConfiguration(name, data)
		if err != nil {
			return Configuration{}, underConfigurationFile(err, path)
		}
	}
	if s.options.ValidateConfiguration == nil {
		return Configuration{}, ConfigurationError("secret store implementation resolver is not configured")
	}
	if err := s.options.ValidateConfiguration(ctx, config); err != nil {
		return Configuration{}, underConfigurationFile(err, path)
	}
	return config, ctx.Err()
}

func underConfigurationFile(err error, path string) error {
	if path == "" {
		return err
	}
	return reshapeFailure(err, func(reported *diagnostics.Diagnostic) {
		if reported.Source == nil {
			reported.Source = &diagnostics.SourceLocation{Path: path}
		}
	})
}

func selectionFor(record Record) Selection {
	return Selection{Version: SelectionVersion, Name: record.Name}
}

func (s Service) selection(ctx context.Context) (Selection, error) {
	if s.options.Selection == nil {
		return Selection{}, StateError("current context selection is not configured")
	}
	return s.options.Selection.Read(ctx)
}

func readyRecord(reg Registry, name string) (Record, error) {
	index := findRecord(reg, name)
	if index < 0 {
		return Record{}, AbsentContext(name)
	}
	record := reg.Contexts[index]
	if record.Mode != Ready {
		return Record{}, NotReady(record)
	}
	return record, nil
}

func (s Service) Init(ctx context.Context, request InitRequest) (*AdmissionResult, error) {
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	if err := validName(request.Name); err != nil {
		return nil, err
	}
	if s.options.Selection == nil || s.options.InitializeSecrets == nil {
		return nil, StateError("context selection or secret initialization is not configured")
	}
	config, err := s.configuration(ctx, request.Name, request.ConfigurationFile)
	if err != nil {
		return nil, err
	}
	var sources desiredstate.Sources
	var environment string
	var report *compilation.Report
	if request.InputDirectory != "" {
		sources, environment, _, report, err = s.admit(ctx, request.InputDirectory)
		if err != nil {
			return nil, err
		}
	}
	var result *AdmissionResult
	err = s.repository.Transact(ctx, true, slices.Clone(sources.Roots), func(tx Transaction) error {
		reg := tx.Registry()
		if index := findRecord(reg, request.Name); index >= 0 && reg.Contexts[index].Mode != Initializing {
			return AlreadyExists(reg.Contexts[index])
		}
		record, err := tx.Reserve(ctx, request.Name, environment, config.Canonical())
		if err != nil {
			return err
		}
		record.EnvironmentDirectory = environment
		if err := tx.InitializeSecrets(ctx, record.Name, func(area secretstore.Area) error {
			return s.options.InitializeSecrets(ctx, record, area)
		}); err != nil {
			return err
		}
		if request.InputDirectory != "" {
			record.Revision, err = tx.Publish(ctx, record.Name, environment, sources)
			if err != nil {
				return err
			}
		}
		record.Mode = Ready
		reg = tx.Registry()
		index := findRecord(reg, record.Name)
		if index < 0 {
			return StateError("context reservation did not publish its initializing record")
		}
		reg.Contexts[index] = record
		if err := commitRegistry(ctx, tx, reg); err != nil {
			return err
		}
		result = admissionResult(record, selectionFor(record), sources, report)
		if err := s.options.Selection.Write(ctx, selectionFor(record)); err != nil {
			return selectionNotUpdated(record.Name, err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, StateError("context publication returned no result")
	}
	return result, nil
}

func admissionResult(record Record, selected Selection, sources desiredstate.Sources, report *compilation.Report) *AdmissionResult {
	result := &AdmissionResult{Context: summary(record, selected), FilesCopied: len(sources.Files) + len(sources.Markers), InputChanged: report != nil}
	if report != nil {
		result.Counts = report.Counts
		result.Diagnostics = slices.Clone(report.Diagnostics)
	}
	return result
}

// admittedUpdate is what an update admits before it takes any lock: the
// Context configuration it compares and the input it would publish.
type admittedUpdate struct {
	config      Configuration
	sources     desiredstate.Sources
	environment string
	controller  string
	report      *compilation.Report
}

func (s Service) Update(ctx context.Context, request UpdateRequest) (*AdmissionResult, error) {
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	if err := validName(request.Name); err != nil {
		return nil, err
	}
	if request.ConfigurationFile == "" && request.InputDirectory == "" {
		return nil, ConfigurationError("context update requires --file or --input-dir")
	}
	selected, err := s.selection(ctx)
	if err != nil {
		return nil, err
	}
	admitted, err := s.admitUpdate(ctx, request)
	if err != nil {
		return nil, err
	}
	var result *AdmissionResult
	err = s.repository.Transact(ctx, false, slices.Clone(admitted.sources.Roots), func(tx Transaction) error {
		var err error
		result, err = s.updateUnder(ctx, tx, request, selected, admitted)
		return err
	})
	if err != nil {
		return nil, absentWhenEmpty(err, request.Name)
	}
	if result == nil {
		return nil, StateError("context update returned no result")
	}
	return result, nil
}

// admitUpdate reads and compiles what an update was given, before any lock.
func (s Service) admitUpdate(ctx context.Context, request UpdateRequest) (admittedUpdate, error) {
	var admitted admittedUpdate
	var err error
	if request.ConfigurationFile != "" {
		if admitted.config, err = s.configuration(ctx, request.Name, request.ConfigurationFile); err != nil {
			return admittedUpdate{}, err
		}
	}
	if request.InputDirectory != "" {
		admitted.sources, admitted.environment, admitted.controller, admitted.report, err = s.admit(ctx, request.InputDirectory)
		if err != nil {
			return admittedUpdate{}, err
		}
	}
	return admitted, nil
}

// updateUnder is the update under the root lock: it compares the Context
// configuration, decides the input update, and publishes changed input once
// its plan was presented and confirmed. Input equal to the selected revision
// keeps that revision, publishing nothing and asking nothing.
func (s Service) updateUnder(ctx context.Context, tx Transaction, request UpdateRequest, selected Selection, admitted admittedUpdate) (*AdmissionResult, error) {
	reg := tx.Registry()
	record, err := readyRecord(reg, request.Name)
	if err != nil {
		return nil, err
	}
	if request.ConfigurationFile != "" {
		if err := sameConfiguration(ctx, tx, record.Name, admitted.config); err != nil {
			return nil, err
		}
	}
	if request.InputDirectory == "" {
		return admissionResult(record, selected, desiredstate.Sources{}, nil), nil
	}
	unchanged, warnings, err := s.decideUpdate(ctx, tx, reg, record, admitted)
	if err != nil {
		return nil, err
	}
	if unchanged {
		result := admissionResult(record, selected, desiredstate.Sources{}, nil)
		result.Counts, result.Diagnostics = admitted.report.Counts, slices.Clone(admitted.report.Diagnostics)
		return result, nil
	}
	plan := UpdatePlan{
		Context: record.Name, InputDirectory: admitted.sources.Roots[0], FilesCopied: len(admitted.sources.Files) + len(admitted.sources.Markers),
		Counts: admitted.report.Counts, Diagnostics: slices.Clone(warnings),
	}
	presented, err := s.confirmPresented(ctx, request.SkipConfirmation, "update", record.Name, func(presenter Presenter) error {
		return presenter.PresentUpdate(ctx, plan)
	})
	if err != nil {
		return nil, err
	}
	if record.Revision, err = tx.Publish(ctx, record.Name, admitted.environment, admitted.sources); err != nil {
		return nil, err
	}
	record.EnvironmentDirectory = admitted.environment
	reg.Contexts[findRecord(reg, record.Name)] = record
	if err := commitRegistry(ctx, tx, reg); err != nil {
		return nil, err
	}
	result := admissionResult(record, selected, admitted.sources, admitted.report)
	result.Diagnostics, result.Presented = warnings, presented
	return result, nil
}

// sameConfiguration refuses a Context configuration other than the stored one,
// which is immutable.
func sameConfiguration(ctx context.Context, tx Transaction, name string, config Configuration) error {
	data, err := tx.Configuration(ctx, name)
	if err != nil {
		return err
	}
	stored, err := ParseConfiguration(name, data)
	if err != nil {
		return StateError("stored Context configuration is malformed")
	}
	if config != stored {
		return ConfigurationError("Context configuration is immutable; secret store changes require a separate context")
	}
	return nil
}

// decideUpdate runs every safeguard of an input update under the root lock and
// the context lease, in order: the controller binding, then the mutation
// guard, so every protected state refuses even an identical input, and only
// then whether the input changes the selected revision at all. Changed input
// returns its warnings: admission's, and over a completed apply the warning
// that apply refuses it until a destroy.
func (s Service) decideUpdate(ctx context.Context, tx Transaction, reg Registry, record Record, admitted admittedUpdate) (bool, []diagnostics.Diagnostic, error) {
	if reg.Controller != (ControllerDescriptor{}) {
		if err := tx.CheckControllerInput(ctx, record.Name, admitted.controller); err != nil {
			return false, nil, err
		}
	}
	disposition, err := s.disposition(ctx, tx, record.Name)
	switch {
	case errors.Is(err, ErrUnreadableEvidence):
		return false, nil, unreadableEvidenceRefusal("context.state", record.Name)
	case err != nil:
		return false, nil, err
	case !disposition.Update:
		return false, nil, IncompleteOperation(record.Name)
	}
	unchanged, err := tx.Unchanged(ctx, record.Name, admitted.environment, admitted.sources)
	if err != nil || unchanged {
		return unchanged, nil, err
	}
	warnings := slices.Clone(admitted.report.Diagnostics)
	if disposition.Applied {
		warnings = append(warnings, appliedInputWarning(record.Name))
	}
	return false, warnings, nil
}

func (s Service) Use(ctx context.Context, request UseRequest) (*UseResult, error) {
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	if err := validName(request.Name); err != nil {
		return nil, err
	}
	if s.options.Selection == nil {
		return nil, StateError("current context selection is not configured")
	}
	var result *UseResult
	err := s.repository.Transact(ctx, false, nil, func(tx Transaction) error {
		record, err := readyRecord(tx.Registry(), request.Name)
		if err != nil {
			return err
		}
		selected := selectionFor(record)
		if err := s.options.Selection.Write(ctx, selected); err != nil {
			return err
		}
		result = &UseResult{Context: summary(record, selected)}
		return nil
	})
	if err != nil {
		return nil, absentWhenEmpty(err, request.Name)
	}
	if result == nil {
		return nil, StateError("context selection returned no result")
	}
	return result, nil
}

func (s Service) List(ctx context.Context, _ ListRequest) (*ListResult, error) {
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	selected, err := s.selection(ctx)
	if err != nil {
		return nil, err
	}
	reg, err := s.repository.View(ctx)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := &ListResult{Contexts: []Summary{}}
	for _, record := range reg.Contexts {
		result.Contexts = append(result.Contexts, summary(record, selected))
	}
	slices.SortFunc(result.Contexts, func(a, b Summary) int { return strings.Compare(a.Name, b.Name) })
	return result, nil
}

func (s Service) Current(ctx context.Context, _ CurrentRequest) (*CurrentResult, error) {
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	selected, err := s.selection(ctx)
	if err != nil {
		return nil, err
	}
	if selected.Name == "" {
		return nil, NoSelection()
	}
	reg, err := s.repository.View(ctx)
	if err != nil {
		return nil, err
	}
	record, err := readyRecord(reg, selected.Name)
	if err != nil {
		return nil, err
	}
	return &CurrentResult{Context: summary(record, selected)}, ctx.Err()
}

// deleteAction is what a deletion's confirmation asks, naming what it abandons:
// objects bootwright status lists, or objects that cannot be listed and why,
// and with them any custodied cluster kubeconfig the keyring it removes holds,
// with the command that exports it first. A lost context's keyring went with
// its directory, so its confirmation names none.
func deleteAction(plan DeletionPlan) string {
	custody := " any custodied cluster kubeconfig (export it first with bootwright cluster kubeconfig --context " + plan.Context + " --name <cluster>)"
	switch {
	case plan.Abandons == AbandonsUnlisted && plan.Lost:
		return "delete with orphaned objects that cannot be listed (" + plan.Reason + ")"
	case plan.Abandons == AbandonsUnlisted:
		return "delete with orphaned objects that cannot be listed (" + plan.Reason + "), removing" + custody
	case plan.Abandons == AbandonsOwned:
		return "delete, abandoning the objects bootwright status --context " + plan.Context + " lists and" + custody
	}
	return "delete"
}

func (s Service) Delete(ctx context.Context, request DeleteRequest) (*DeleteResult, error) {
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	if err := validName(request.Name); err != nil {
		return nil, err
	}
	if !request.Purge {
		return nil, UnsafeDelete("context deletion requires --purge")
	}
	selected, err := s.selection(ctx)
	if err != nil {
		return nil, err
	}
	var result *DeleteResult
	err = s.repository.TransactDeletion(ctx, request.Name, func(tx Transaction) error {
		reg := tx.Registry()
		index := findRecord(reg, request.Name)
		if index < 0 {
			return AbsentContext(request.Name)
		}
		record := reg.Contexts[index]
		plan, err := s.decideDeletion(ctx, tx, record, request.AllowOrphans)
		if err != nil {
			return err
		}
		if _, err := s.confirmPresented(ctx, request.SkipConfirmation, deleteAction(plan), record.Name, func(presenter Presenter) error {
			return presenter.PresentDeletion(ctx, plan)
		}); err != nil {
			return err
		}
		if err := tx.Delete(ctx, record); err != nil {
			return err
		}
		result = &DeleteResult{Name: record.Name, Outcome: "deleted", OrphansAbandoned: plan.Abandons != AbandonsNone, ReleasedReservations: plan.Reservations}
		if selected.Name == record.Name {
			if err := s.options.Selection.Clear(ctx, selected); err != nil {
				return StateError("context was deleted, but its current selection could not be cleared")
			}
			result.CurrentCleared = true
		}
		return nil
	})
	if err != nil {
		return nil, absentWhenEmpty(err, request.Name)
	}
	if result == nil {
		return nil, StateError("context deletion returned no result")
	}
	return result, nil
}

// decideDeletion runs a deletion's safeguards under the root lock and the
// context lease and names what it removes and abandons, the host reservations
// it releases included, before anything asks or changes.
func (s Service) decideDeletion(ctx context.Context, tx Transaction, record Record, allowOrphans bool) (DeletionPlan, error) {
	plan := DeletionPlan{Context: record.Name, Mode: record.Mode, Revision: record.Revision, Abandons: AbandonsNone}
	if record.Mode == Ready {
		var err error
		if plan.Abandons, plan.Reason, plan.Lost, err = s.abandonment(ctx, tx, record.Name, allowOrphans); err != nil {
			return DeletionPlan{}, err
		}
	}
	reservations, err := tx.HostReservations(ctx, record.Name)
	if err != nil {
		return DeletionPlan{}, err
	}
	plan.Reservations = reservations
	return plan, nil
}

// abandonment is what deleting a ready context abandons, from the guard's
// reading of its evidence under the lease that reading takes first, so a live
// lease refuses before anything else, and whether the context is lost. A
// context that owns objects, or whose directory or evidence leaves what it
// owns unlisted, is deleted only with the orphan acknowledgement.
func (s Service) abandonment(ctx context.Context, tx Transaction, name string, allowOrphans bool) (Abandonment, string, bool, error) {
	disposition, err := s.disposition(ctx, tx, name)
	switch {
	case errors.Is(err, ErrLostContext):
		if !allowOrphans {
			return "", "", false, err
		}
		return AbandonsUnlisted, "its directory is gone", true, nil
	case errors.Is(err, ErrUnreadableEvidence):
		if !allowOrphans {
			return "", "", false, unreadableEvidenceRefusal("context.unsafe-delete", name)
		}
		return AbandonsUnlisted, "its mutation evidence cannot be read", false, nil
	case err != nil:
		return "", "", false, err
	case !disposition.Dispose:
		if !allowOrphans {
			return "", "", false, orphanRefusal(name)
		}
		return AbandonsOwned, "", false, nil
	}
	return AbandonsNone, "", false, nil
}
