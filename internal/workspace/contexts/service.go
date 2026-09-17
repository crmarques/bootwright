package contexts

import (
	"context"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
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

func (s Service) confirm(ctx context.Context, skip bool, action, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if skip {
		return nil
	}
	if s.confirmer == nil {
		return StateError("confirmation requires an interactive terminal or --yes")
	}
	if err := s.confirmer.Confirm(ctx, action, name); err != nil {
		return err
	}
	return ctx.Err()
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
			return Configuration{}, err
		}
		config, err = ParseConfiguration(name, data)
		if err != nil {
			return Configuration{}, err
		}
	}
	if s.options.ValidateConfiguration == nil {
		return Configuration{}, ConfigurationError("secret store implementation resolver is not configured")
	}
	if err := s.options.ValidateConfiguration(ctx, config); err != nil {
		return Configuration{}, err
	}
	return config, ctx.Err()
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
		return Record{}, StateError("named context does not exist")
	}
	record := reg.Contexts[index]
	if record.Mode == Initializing {
		return Record{}, StateError("context initialization is incomplete; retry context init --name " + record.Name + " with the original configuration and input")
	}
	if record.Mode != Ready {
		return Record{}, StateError("context deletion is incomplete; retry context delete --name " + record.Name + " --purge")
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
			return StateError("context name already exists; use context update or delete it explicitly")
		}
		record, err := tx.Reserve(ctx, request.Name, environment, config.Canonical())
		if err != nil {
			return err
		}
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
			return StateError("context was created, but current selection could not be updated; run context use --name " + record.Name)
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
	var config Configuration
	if request.ConfigurationFile != "" {
		config, err = s.configuration(ctx, request.Name, request.ConfigurationFile)
		if err != nil {
			return nil, err
		}
	}
	var sources desiredstate.Sources
	var environment, controllerMachine string
	var report *compilation.Report
	if request.InputDirectory != "" {
		sources, environment, controllerMachine, report, err = s.admit(ctx, request.InputDirectory)
		if err != nil {
			return nil, err
		}
	}
	var result *AdmissionResult
	err = s.repository.Transact(ctx, false, slices.Clone(sources.Roots), func(tx Transaction) error {
		reg := tx.Registry()
		record, err := readyRecord(reg, request.Name)
		if err != nil {
			return err
		}
		if request.ConfigurationFile != "" {
			data, err := tx.Configuration(ctx, record.Name)
			if err != nil {
				return err
			}
			stored, err := ParseConfiguration(record.Name, data)
			if err != nil {
				return StateError("stored Context configuration is malformed")
			}
			if config != stored {
				return ConfigurationError("Context configuration is immutable; secret store changes require a separate context")
			}
		}
		if request.InputDirectory == "" {
			result = admissionResult(record, selected, sources, nil)
			return nil
		}
		if reg.Controller != (ControllerDescriptor{}) {
			guard, ok := tx.(ControllerInputGuard)
			if !ok {
				return StateError("controller binding guard is unavailable")
			}
			if err := guard.CheckControllerInput(ctx, record.Name, controllerMachine); err != nil {
				return err
			}
		}
		disposition, err := s.disposition(ctx, tx, record.Name)
		if err != nil {
			return err
		}
		if !disposition.Update {
			return StateError("context has an incomplete operation; preserve its input for continuation")
		}
		if err := s.confirm(ctx, request.SkipConfirmation, "update", request.Name); err != nil {
			return err
		}
		record.Revision, err = tx.Publish(ctx, record.Name, environment, sources)
		if err != nil {
			return err
		}
		record.EnvironmentDirectory = environment
		reg.Contexts[findRecord(reg, request.Name)] = record
		if err := commitRegistry(ctx, tx, reg); err != nil {
			return err
		}
		result = admissionResult(record, selected, sources, report)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, StateError("context update returned no result")
	}
	return result, nil
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
		return nil, err
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
		return nil, StateError("no current context is selected; use context use --name <name>")
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

func deleteAction(orphans bool) string {
	if orphans {
		return "delete with orphaned objects"
	}
	return "delete"
}

func orphanRefusal(name string) error {
	return UnsafeDeleteWithRemediation("context still owns realized objects; deletion would orphan them",
		"remove them with bootwright destroy --context "+name+", or abandon them with --allow-orphans")
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
	err = s.repository.Transact(ctx, false, nil, func(tx Transaction) error {
		reg := tx.Registry()
		index := findRecord(reg, request.Name)
		if index < 0 {
			return StateError("named context does not exist")
		}
		record := reg.Contexts[index]
		orphans := false
		if record.Mode == Ready {
			disposition, err := s.disposition(ctx, tx, record.Name)
			if err != nil {
				return err
			}
			if !disposition.Dispose {
				if !request.AllowOrphans {
					return orphanRefusal(record.Name)
				}
				orphans = true
			}
		}
		if err := s.confirm(ctx, request.SkipConfirmation, deleteAction(orphans), request.Name); err != nil {
			return err
		}
		if err := tx.Delete(ctx, record); err != nil {
			return err
		}
		result = &DeleteResult{Name: record.Name, Outcome: "deleted", OrphansAbandoned: orphans}
		if selected.Name == record.Name {
			if err := s.options.Selection.Clear(ctx, selected); err != nil {
				return StateError("context was deleted, but its current selection could not be cleared")
			}
			result.CurrentCleared = true
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, StateError("context deletion returned no result")
	}
	return result, nil
}
