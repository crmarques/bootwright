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
)

type Service struct {
	reader     DirectoryReader
	compiler   Compiler
	repository Repository
	guard      ContextMutationGuard
	confirmer  Confirmer
}

func New(reader DirectoryReader, compiler Compiler, repository Repository, guard ContextMutationGuard, confirmer Confirmer) Service {
	return Service{reader: reader, compiler: compiler, repository: repository, guard: guard, confirmer: confirmer}
}

type InitRequest struct {
	Name             string
	InputDirectory   string
	SkipConfirmation bool
}

type UpdateRequest struct {
	Name             string
	InputDirectory   string
	SkipConfirmation bool
}

type UseRequest struct{ Name string }

type ListRequest struct{}

type CurrentRequest struct{ Short bool }

type DeleteRequest struct {
	Name             string
	Purge            bool
	SkipConfirmation bool
	AbandonResources bool
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

func summary(r Record, current string) Summary {
	return Summary{Name: r.Name, ID: r.ID, Mode: r.Mode, Current: r.Name == current}
}

func commitRegistry(ctx context.Context, tx Transaction, reg Registry) error {
	reg.Identities = slices.Clone(reg.Identities)
	reg.Contexts = slices.Clone(reg.Contexts)
	slices.SortFunc(reg.Identities, func(a, b Identity) int { return strings.Compare(a.EnvironmentDirectory, b.EnvironmentDirectory) })
	slices.SortFunc(reg.Contexts, func(a, b Record) int { return strings.Compare(a.Name, b.Name) })
	return tx.Commit(ctx, reg)
}

func findRecord(reg Registry, name string) int {
	return slices.IndexFunc(reg.Contexts, func(r Record) bool { return r.Name == name })
}

func (s Service) disposition(ctx context.Context, tx Transaction, id string) (Disposition, error) {
	if s.guard == nil {
		return Disposition{}, StateError("context mutation guard is not configured")
	}
	data, err := tx.MutationState(ctx, id)
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

func (s Service) admit(ctx context.Context, path string) (desiredstate.Sources, string, *compilation.Report, error) {
	if s.reader == nil || s.compiler == nil {
		return desiredstate.Sources{}, "", nil, StateError("context admission is not configured")
	}
	if err := s.repository.CheckInputDirectory(ctx, path); err != nil {
		return desiredstate.Sources{}, "", nil, err
	}
	if err := ctx.Err(); err != nil {
		return desiredstate.Sources{}, "", nil, err
	}
	sources, err := s.reader.ReadDirectory(ctx, path)
	if err != nil {
		return desiredstate.Sources{}, "", nil, err
	}
	if err = ctx.Err(); err != nil {
		return desiredstate.Sources{}, "", nil, err
	}
	state, report, err := s.compiler.Compile(ctx, desiredstate.Sources{Files: slices.Clone(sources.Files), Markers: slices.Clone(sources.Markers), Roots: slices.Clone(sources.Roots)})
	if err != nil {
		return desiredstate.Sources{}, "", nil, err
	}
	if err = ctx.Err(); err != nil {
		return desiredstate.Sources{}, "", nil, err
	}
	if state == nil || report == nil || len(sources.Roots) != 1 {
		return desiredstate.Sources{}, "", nil, StateError("context admission returned incomplete input")
	}
	environments := state.Authored().OfKind(api.Environment)
	if len(environments) != 1 {
		return desiredstate.Sources{}, "", nil, StateError("context admission returned no unique Environment")
	}
	origin, ok := state.Origin(environments[0].Identity())
	if !ok || !filepath.IsAbs(origin.Path) {
		return desiredstate.Sources{}, "", nil, StateError("context admission returned no Environment provenance")
	}
	directory := filepath.Dir(origin.Path)
	relative, err := filepath.Rel(sources.Roots[0], directory)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return desiredstate.Sources{}, "", nil, StateError("Environment is outside the admitted input directory")
	}
	return sources, directory, report, nil
}

func (s Service) Init(ctx context.Context, request InitRequest) (*AdmissionResult, error) {
	return s.publish(ctx, request.Name, request.InputDirectory, request.SkipConfirmation, false)
}

func (s Service) Update(ctx context.Context, request UpdateRequest) (*AdmissionResult, error) {
	return s.publish(ctx, request.Name, request.InputDirectory, request.SkipConfirmation, true)
}

func (s Service) publish(ctx context.Context, name, path string, skip, update bool) (*AdmissionResult, error) {
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	if err := validName(name); err != nil {
		return nil, err
	}
	sources, environment, report, err := s.admit(ctx, path)
	if err != nil {
		return nil, err
	}
	var result *AdmissionResult
	err = s.repository.Transact(ctx, !update, append(slices.Clone(sources.Roots), environment), func(tx Transaction) error {
		reg := tx.Registry()
		index := findRecord(reg, name)
		if update && index < 0 {
			return StateError("named context does not exist")
		}
		var record Record
		if index >= 0 {
			record = reg.Contexts[index]
			if record.Mode == RecoveryOnly {
				return StateError("recovery-only contexts cannot be updated or recreated")
			}
			if record.EnvironmentDirectory != environment {
				return StateError("replacement input changes the context Environment directory; create a separate context")
			}
			if !update && !skip {
				return StateError("context recreation requires --yes")
			}
		} else {
			for _, other := range reg.Contexts {
				if other.EnvironmentDirectory == environment {
					return StateError("Environment directory already belongs to another named context")
				}
			}
			id, reserveErr := tx.Reserve(ctx, environment)
			if reserveErr != nil {
				return reserveErr
			}
			record = Record{Name: name, ID: id, EnvironmentDirectory: environment, Mode: Active}
			if !slices.ContainsFunc(reg.Identities, func(i Identity) bool { return i.EnvironmentDirectory == environment }) {
				reg.Identities = append(reg.Identities, Identity{EnvironmentDirectory: environment, ID: id})
			}
		}
		disposition, guardErr := s.disposition(ctx, tx, record.ID)
		if guardErr != nil {
			return guardErr
		}
		if update && !disposition.Update {
			return StateError("context has an incomplete operation; preserve it for reconciliation")
		}
		if !update && !disposition.Dispose {
			return StateError("context recreation requires positive disposal evidence")
		}
		if update {
			if confirmErr := s.confirm(ctx, skip, "update", name); confirmErr != nil {
				return confirmErr
			}
		}
		revision, publishErr := tx.Publish(ctx, record.ID, environment, sources)
		if publishErr != nil {
			return publishErr
		}
		record.Revision = revision
		if index < 0 {
			reg.Contexts = append(reg.Contexts, record)
		} else {
			reg.Contexts[index] = record
		}
		if !update {
			reg.Current = name
		}
		if commitErr := commitRegistry(ctx, tx, reg); commitErr != nil {
			return commitErr
		}
		result = &AdmissionResult{Context: summary(record, reg.Current), Counts: report.Counts, FilesCopied: len(sources.Files) + len(sources.Markers), Diagnostics: slices.Clone(report.Diagnostics)}
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

func (s Service) Use(ctx context.Context, request UseRequest) (*UseResult, error) {
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	if err := validName(request.Name); err != nil {
		return nil, err
	}
	var result *UseResult
	err := s.repository.Transact(ctx, false, nil, func(tx Transaction) error {
		reg := tx.Registry()
		index := findRecord(reg, request.Name)
		if index < 0 {
			return StateError("named context does not exist")
		}
		reg.Current = request.Name
		if err := commitRegistry(ctx, tx, reg); err != nil {
			return err
		}
		result = &UseResult{Context: summary(reg.Contexts[index], reg.Current)}
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
	reg, err := s.repository.View(ctx)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := &ListResult{Contexts: []Summary{}}
	for _, record := range reg.Contexts {
		result.Contexts = append(result.Contexts, summary(record, reg.Current))
	}
	slices.SortFunc(result.Contexts, func(a, b Summary) int { return strings.Compare(a.Name, b.Name) })
	return result, nil
}

func (s Service) Current(ctx context.Context, _ CurrentRequest) (*CurrentResult, error) {
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	reg, err := s.repository.View(ctx)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	index := findRecord(reg, reg.Current)
	if reg.Current == "" || index < 0 {
		return nil, StateError("no current context is selected")
	}
	return &CurrentResult{Context: summary(reg.Contexts[index], reg.Current)}, nil
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
	var result *DeleteResult
	err := s.repository.Transact(ctx, false, nil, func(tx Transaction) error {
		reg := tx.Registry()
		index := findRecord(reg, request.Name)
		if index < 0 {
			return StateError("named context does not exist")
		}
		record := reg.Contexts[index]
		disposition, err := s.disposition(ctx, tx, record.ID)
		if err != nil {
			return err
		}
		outcome := "deleted"
		if !disposition.Dispose {
			if !request.AbandonResources || !disposition.Recovery {
				return UnsafeDelete("context has protected state; recovery-only archival requires --abandon-resources")
			}
			outcome = "recoveryOnly"
		}
		if err := s.confirm(ctx, request.SkipConfirmation, "delete", request.Name); err != nil {
			return err
		}
		if err := tx.Archive(ctx, record, outcome); err != nil {
			return err
		}
		cleared := false
		if outcome == "deleted" {
			reg.Contexts = slices.Delete(reg.Contexts, index, index+1)
			if reg.Current == record.Name {
				reg.Current = ""
				cleared = true
			}
		} else {
			reg.Contexts[index].Mode = RecoveryOnly
		}
		if err := commitRegistry(ctx, tx, reg); err != nil {
			return err
		}
		result = &DeleteResult{Name: record.Name, ID: record.ID, Outcome: outcome, CurrentCleared: cleared}
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
