package contexts

import (
	"context"
	"path/filepath"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

type admittedInput struct {
	config      *Configuration
	sources     desiredstate.Sources
	file        string
	environment string
	controller  string
	report      *compilation.Report
	addsTo      string
	kept        KeptInputChange
}

func (s Service) admitInputs(ctx context.Context, name, file, directory string) (admittedInput, error) {
	var admitted admittedInput
	if file != "" {
		var err error
		admitted, err = s.admitFile(ctx, name, file, directory != "")
		if err != nil {
			return admittedInput{}, err
		}
	}
	if directory != "" {
		if s.reader == nil || s.compiler == nil {
			return admittedInput{}, StateError("context admission is not configured")
		}
		if err := s.repository.CheckInputDirectory(ctx, directory); err != nil {
			return admittedInput{}, err
		}
		if err := ctx.Err(); err != nil {
			return admittedInput{}, err
		}
		var err error
		admitted.sources, err = s.reader.ReadDirectory(ctx, directory)
		if err != nil {
			return admittedInput{}, err
		}
	}
	if directory != "" || admitted.file != "" {
		return s.compileInput(ctx, admitted)
	}
	return admitted, ctx.Err()
}

func (s Service) admitFile(ctx context.Context, name, path string, configurationOnly bool) (admittedInput, error) {
	if s.options.FileReader == nil {
		return admittedInput{}, ConfigurationError("context input file reader is not configured")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return admittedInput{}, diagnostics.NewFailure("input.read", "input file path cannot be resolved", path)
	}
	maximum := desiredstate.MaxFileBytes
	if configurationOnly {
		maximum = MaxConfigurationBytes
	}
	data, err := s.options.FileReader.ReadFile(ctx, absolute, maximum)
	if err != nil {
		return admittedInput{}, reshapeFailure(err, func(reported *diagnostics.Diagnostic) {
			if reported.Remediation == "" {
				reported.Remediation = configurationFileRemediation
			}
		})
	}
	if err := ctx.Err(); err != nil {
		return admittedInput{}, err
	}
	if configurationOnly || startsWithContext(data) {
		config, err := ParseConfiguration(name, data)
		if err == nil {
			err = s.validateConfiguration(ctx, config)
		}
		if err != nil {
			return admittedInput{}, underInputFile(err, path)
		}
		return admittedInput{config: &config}, nil
	}

	if ext := filepath.Ext(path); ext != ".yaml" && ext != ".yml" {
		return admittedInput{}, diagnostics.NewFailureWithRemediation("input.read",
			"desired-state input file must end in lowercase .yaml or .yml", path, "pass a file with a .yaml or .yml extension")
	}
	// The existing revision format records one directory and the exact files
	// acquired under it. Using the parent as the root does not scan it.
	root := filepath.Dir(absolute)
	if err := s.repository.CheckInputFile(ctx, absolute); err != nil {
		return admittedInput{}, underInputFile(err, path)
	}
	return admittedInput{file: absolute, sources: desiredstate.Sources{
		Roots: []string{root},
		Files: []desiredstate.SourceFile{desiredstate.NewSourceFile(absolute, data)},
	}}, nil
}

func (s Service) compileInput(ctx context.Context, admitted admittedInput) (admittedInput, error) {
	if err := ctx.Err(); err != nil {
		return admittedInput{}, err
	}
	if s.compiler == nil {
		return admittedInput{}, StateError("context admission is not configured")
	}
	sources := admitted.sources
	state, report, err := s.compiler.Compile(ctx, desiredstate.Sources{Files: slices.Clone(sources.Files), Markers: slices.Clone(sources.Markers), Roots: slices.Clone(sources.Roots)})
	if err != nil {
		return admittedInput{}, err
	}
	if err := ctx.Err(); err != nil {
		return admittedInput{}, err
	}
	if state == nil || report == nil || len(sources.Roots) != 1 {
		return admittedInput{}, StateError("context admission returned incomplete input")
	}
	environments := state.Authored().OfKind(api.Environment)
	if len(environments) != 1 {
		return admittedInput{}, StateError("context admission returned no unique Environment")
	}
	origin, ok := state.Origin(environments[0].Identity())
	if !ok || !filepath.IsAbs(origin.Path) {
		return admittedInput{}, StateError("context admission returned no Environment provenance")
	}
	directory := filepath.Dir(origin.Path)
	relative, err := filepath.Rel(sources.Roots[0], directory)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return admittedInput{}, StateError("Environment is outside the admitted input directory")
	}
	effectiveEnvironment, found := state.Effective().Find(api.Environment, environments[0].Name())
	if !found {
		return admittedInput{}, StateError("context admission returned no effective Environment")
	}
	admitted.environment = directory
	admitted.controller = effectiveEnvironment.Spec().Get("controller", "machineRef").Text()
	admitted.report = report
	return admitted, nil
}

func (s Service) validateConfiguration(ctx context.Context, config Configuration) error {
	if s.options.ValidateConfiguration == nil {
		return ConfigurationError("secret store implementation resolver is not configured")
	}
	if err := s.options.ValidateConfiguration(ctx, config); err != nil {
		return err
	}
	return ctx.Err()
}

func underInputFile(err error, path string) error {
	return reshapeFailure(err, func(reported *diagnostics.Diagnostic) {
		if reported.Source == nil {
			reported.Source = &diagnostics.SourceLocation{Path: path}
		}
	})
}
