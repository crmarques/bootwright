package prerequisites

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func (s Service) selectedResolution(current inspection, requirements NativeRequirements, frozen []inspectionResolution) (Definition, bool, error) {
	if s.options.Native == nil || s.options.NativeInspector == nil {
		return Definition{}, false, failure("controller.unsupported", "native dependency resolution and inspection are not configured", "use a compatible executable")
	}
	// A retained resolution serves any context, because setup resolves only the
	// context-independent closure. Target tools are resolved and retained by the
	// controller stage and never take part in this selection.
	matches := func(value Definition) bool {
		return value.Platform == current.platform && value.Versions.Baseline() == current.selection.Versions().Baseline() && value.NativeRequirements == requirements && len(value.Tools) == 0
	}
	var selected *Definition
	if len(frozen) != 0 && frozen[0].Definition != nil {
		selected = frozen[0].Definition
	} else if receipt := current.view.State.Receipt; receipt.ID != "" && receipt.Incomplete() {
		selected = receipt.Definition
		if selected == nil {
			return Definition{}, false, failure("controller.unknown", "pending setup lacks its exact dependency resolution", "restore the original setup evidence")
		}
	} else {
		for index := len(current.view.State.RetainedDefinitions) - 1; index >= 0; index-- {
			value := current.view.State.RetainedDefinitions[index]
			if matches(value) && !supersededLatest(value) {
				selected = &value
				break
			}
		}
		if selected == nil && current.view.State.Receipt.Definition != nil && matches(*current.view.State.Receipt.Definition) && !supersededLatest(*current.view.State.Receipt.Definition) {
			selected = current.view.State.Receipt.Definition
		}
	}
	if selected != nil {
		if err := ValidateResolvedDefinition(*selected); err != nil {
			return Definition{}, false, err
		}
		if len(selected.Tools) != 0 {
			return Definition{}, false, failure("controller.state", "frozen setup contains target tools it does not own", "restore the exact setup evidence")
		}
		if !matches(*selected) {
			return Definition{}, false, failure("controller.unknown", "frozen setup dependencies differ from current intent", "restore the original input before retrying")
		}
		return CloneDefinition(*selected), true, nil
	}
	// Pure platform admission reuses the catalog's supplied-foundation matrix.
	// Its historical artifact pins are not the latest-version product policy.
	// The complete selection is admitted here so an unqualified native client
	// refuses during inspection, before any dependency is acquired.
	if _, err := s.catalog.Select(current.platform, requirements); err != nil {
		return Definition{}, false, err
	}
	versions := current.selection.Versions()
	return Definition{Platform: current.platform, Versions: versions, NativeRequirements: requirements, PythonVersion: versions.Python, AnsibleVersion: versions.Ansible, Runtime: RuntimeRequirement{Version: versions.Podman}}, false, nil
}

// supersededLatest reports a retained latest resolution whose Python or
// Ansible release lies outside the set this build qualifies, older or newer.
// A moved qualified set is the one update setup discovers on its own, so such
// a record is resolved fresh rather than reused or carried forward; it stays
// readable, and an exact intent is never superseded.
func supersededLatest(value Definition) bool {
	return value.Versions.Ansible == "latest" && ValidateQualifiedAnsibleVersion(value.AnsibleVersion) != nil ||
		value.Versions.Python == "latest" && !QualifiedControllerPython(value.PythonVersion)
}

func dependencyIntent(selection controller.Selection) []string {
	versions := selection.Versions()
	result := []string{"python=" + versions.Python, "ansible=" + versions.Ansible, "openssh=" + versions.OpenSSH, "nmstate=" + versions.NMState}
	if selection.ContainerRuntime() {
		result = append(result, "podman="+versions.Podman)
	}
	return result
}

func (s Service) inspectRuntime(ctx context.Context, definition Definition) (RuntimeInspection, error) {
	if definition.Native == nil {
		if s.options.Bootstrap != nil {
			return RuntimeInspection{}, nil
		}
		return s.host.Runtime(ctx, definition.Runtime)
	}
	if s.options.NativeInspector == nil {
		return RuntimeInspection{}, failure("controller.unsupported", "native dependency inspection is unavailable", "use a compatible executable")
	}
	presence, err := s.options.NativeInspector.Check(ctx, *definition.Native)
	inspection := RuntimeInspection{Present: presence.Ready, Ready: presence.Ready}
	for _, root := range presence.Installed {
		if root.Key == "podman" {
			inspection.Version = root.Package.Version + "-" + root.Package.Release
		}
	}
	return inspection, err
}

func describeNativeAction(action NativeAction) string {
	identity := func(value NativeIdentity) string {
		version := value.Version
		if value.Epoch != 0 {
			version = strconv.Itoa(value.Epoch) + ":" + version
		}
		return value.Name + " " + version + "-" + value.Release + "." + value.Architecture
	}
	if action.Before == nil {
		return action.Kind + " " + identity(action.After)
	}
	return action.Kind + " " + identity(*action.Before) + " -> " + identity(action.After)
}

// resolutionStep brackets one dependency family with progress rows, so the
// operator sees which publisher or solver is being waited on and what it
// settled. A failed step reports before its error propagates.
func (s Service) resolutionStep(ctx context.Context, report *Report, position, total int, label string, resolve func() (string, error)) error {
	s.report(ctx, report, ProgressEvent{Phase: ResolutionPhase, Action: label, Status: "running", Step: position, Steps: total})
	detail, err := resolve()
	if err != nil {
		s.report(ctx, report, ProgressEvent{Phase: ResolutionPhase, Action: label, Status: "failed", Step: position, Steps: total})
		return err
	}
	s.report(ctx, report, ProgressEvent{Phase: ResolutionPhase, Action: label, Status: "ok", Detail: detail, Step: position, Steps: total})
	return nil
}

func nativeChangeSummary(plan NativeResolvedPlan) string {
	switch len(plan.Actions) {
	case 0:
		return "no changes"
	case 1:
		return "1 change"
	}
	return strconv.Itoa(len(plan.Actions)) + " changes"
}

func toolVersionSummary(tools []ToolDefinition) string {
	versions := make([]string, 0, len(tools))
	for _, tool := range tools {
		versions = append(versions, tool.Kind+" "+tool.Version)
	}
	return strings.Join(versions, ", ")
}

// resolveDependencies is the only application path that discovers latest.
// It runs outside shared-state locks; a second inspection binds its exact
// result to unchanged host/input/receipt evidence before plan presentation.
// A reusable retained resolution keeps its Python, Ansible and target tools
// frozen, so only the native transaction is solved and reported, against the
// host's current inventory.
func (s Service) resolveDependencies(ctx context.Context, before inspection) (inspection, error) {
	total, step := 2, 1
	var bootstrap BootstrapDefinition
	if before.frozenBootstrap() {
		total, bootstrap = 1, *before.definition.Bootstrap
	} else {
		err := s.resolutionStep(ctx, &before.report, step, total, "Python and Ansible", func() (string, error) {
			var err error
			var warnings []diagnostics.Diagnostic
			bootstrap, warnings, err = s.options.Bootstrap.Resolve(ctx, before.platform, before.selection.Versions(), before.route())
			before.report.Warnings = append(before.report.Warnings, warnings...)
			return "Python " + bootstrap.PythonVersion + ", Ansible " + bootstrap.AnsibleVersion, err
		})
		if err != nil {
			return before, err
		}
		step++
	}
	var native NativeResolvedPlan
	err := s.resolutionStep(ctx, &before.report, step, total, "Native packages", func() (string, error) {
		var err error
		native, err = s.options.Native.Resolve(ctx, before.platform, before.definition.NativeRequirements, before.selection.Versions(), before.route())
		return nativeChangeSummary(native), err
	})
	if err != nil {
		return before, err
	}
	definition, err := NewResolvedDefinition(bootstrap, native)
	if err != nil {
		return before, err
	}
	for _, source := range definition.Sources {
		for _, retained := range before.view.State.RetainedSources {
			if retained.ID == source.ID && retained != source {
				return before, failure("controller.identity", "publisher metadata changed an immutable retained dependency source", "restore consistent publisher metadata before repeating setup")
			}
		}
	}
	if before.definition.Native != nil {
		for _, previous := range nativeRootArtifacts(*before.definition.Native) {
			for _, current := range nativeRootArtifacts(native) {
				if previous.Name == current.Name && previous.Epoch == current.Epoch && previous.Version == current.Version && previous.Release == current.Release && previous.Architecture == current.Architecture && (previous.Source.SHA256 != current.Source.SHA256 || previous.Signer != current.Signer || previous.Source.Bytes != current.Source.Bytes) {
					return before, failure("controller.identity", "publisher metadata changed the bytes or signer of a retained native release", "restore consistent publisher metadata before repeating setup")
				}
			}
		}
	}
	// A fresh solve may omit already satisfied transitive packages. Reuse the
	// existing full source closure when the desired releases are unchanged and
	// the native solver proposes no action, preserving sealed-bundle identity.
	if before.definition.Bootstrap != nil && unchangedResolvedDependencies(before.definition, definition) {
		definition = CloneDefinition(before.definition)
	}
	return s.rebind(ctx, before, definition)
}

// carryForward keeps a retained resolution whose only incompatibility is the
// automation this executable embeds. Every release, byte count and signer it
// froze still stands, so the closure is reprojected from the sources its own
// retained bundle holds rather than solved again: no publisher or repository is
// consulted, and the native transaction is reused whenever its roots remain
// installed. Only the projection identity, and with it the bundle this setup
// publishes, is new. A retained bundle that can no longer serve one of those
// sources returns ErrRetainedSourceUnavailable, and setup resolves afresh.
func (s Service) carryForward(ctx context.Context, before inspection) (inspection, error) {
	retained := before.definition
	if retained.Bootstrap == nil || retained.Native == nil || retained.CatalogDigest == "" {
		return before, failure("controller.state", "superseded automation has no retained resolution to carry forward", setupCommand())
	}
	var rebased BootstrapDefinition
	err := s.resolutionStep(ctx, &before.report, 1, 1, "Retained Python and Ansible", func() (string, error) {
		err := s.storage.ReadController(ctx, "", func(view StorageView) error {
			if view.OpenBundle == nil {
				return failure("controller.unsupported", "retained bundle inspection is not configured", "use a compatible executable")
			}
			area, err := view.OpenBundle(ctx, retained.CatalogDigest)
			if err != nil {
				return err
			}
			if area == nil {
				return errors.Join(ErrRetainedSourceUnavailable, failure("controller.state", "the retained bundle this resolution is carried from is missing", setupCommand()))
			}
			rebased, err = s.bundle.Rebase(ctx, area, *retained.Bootstrap)
			return err
		})
		return "Python " + retained.PythonVersion + ", Ansible " + retained.AnsibleVersion, err
	})
	if err != nil {
		return before, err
	}
	definition, err := NewResolvedDefinition(rebased, *retained.Native)
	if err != nil {
		return before, err
	}
	return s.rebind(ctx, before, definition, retained.CatalogDigest)
}

// rebind binds a definition produced outside shared-state locks to unchanged
// host, input and receipt evidence, and returns the inspection whose plan is
// presented for confirmation. Retained, when given, names the bundle
// whose sources preparation may read instead of acquiring them again.
func (s Service) rebind(ctx context.Context, before inspection, definition Definition, retained ...string) (inspection, error) {
	frozen := inspectionResolution{Definition: &definition}
	if len(retained) != 0 {
		frozen.Retained = retained[0]
	} else {
		frozen.Retained = before.retainedDigest
	}
	var after inspection
	err := s.storage.ReadController(ctx, "", func(view StorageView) error {
		var err error
		after, err = s.inspect(ctx, view, false, "", frozen)
		if err != nil {
			return err
		}
		after.report.ProgressPresented = before.report.ProgressPresented
		after.report.Warnings = before.report.Warnings
		if !before.host.Equal(after.host) || before.selection.Versions() != after.selection.Versions() || before.view.State.Receipt.ID != after.view.State.Receipt.ID || before.view.State.Receipt.Status != after.view.State.Receipt.Status {
			return failure("controller.conflict", "controller requirements changed during dependency resolution", setupCommand())
		}
		return nil
	})
	if err != nil {
		return before, err
	}
	return after, nil
}

func unchangedResolvedDependencies(before, after Definition) bool {
	if before.Bootstrap == nil || before.Native == nil || after.Bootstrap == nil || after.Native == nil || len(after.Native.Actions) != 0 || before.Platform != after.Platform || before.Versions != after.Versions || before.NativeRequirements != after.NativeRequirements || !slices.Equal(before.ToolRequests, after.ToolRequests) || !reflect.DeepEqual(before.Tools, after.Tools) || !reflect.DeepEqual(before.Native.Roots, after.Native.Roots) {
		return false
	}
	if !slices.Equal(nativeRootArtifacts(*before.Native), nativeRootArtifacts(*after.Native)) {
		return false
	}
	a, b := before.Bootstrap, after.Bootstrap
	return a.PythonVersion == b.PythonVersion && a.AnsibleVersion == b.AnsibleVersion && a.AutomationDigest == b.AutomationDigest && a.ProjectionSHA256 == b.ProjectionSHA256 && slices.Equal(a.Sources, b.Sources) && slices.Equal(a.ExecutionPackages, b.ExecutionPackages) && reflect.DeepEqual(a.Execution, b.Execution)
}

func nativeRootArtifacts(plan NativeResolvedPlan) []NativePackage {
	result := make([]NativePackage, 0, len(plan.Roots))
	for _, root := range plan.Roots {
		for _, candidate := range plan.Packages {
			if root.Package == (NativeIdentity{Name: candidate.Name, Epoch: candidate.Epoch, Version: candidate.Version, Release: candidate.Release, Architecture: candidate.Architecture}) {
				result = append(result, candidate)
				break
			}
		}
	}
	return result
}
