package prerequisites

import (
	"context"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/internal/controller"
)

func (s Service) selectedResolution(current inspection, requirements NativeRequirements, frozen []inspectionResolution) (Definition, bool, error) {
	if s.options.Native == nil || s.options.NativeInspector == nil {
		return Definition{}, false, failure("controller.unsupported", "native dependency resolution and inspection are not configured", "use a compatible executable")
	}
	matches := func(value Definition) bool {
		return value.Platform == current.platform && value.Versions == current.selection.Versions() && value.NativeRequirements == requirements && slices.Equal(value.ToolRequests, current.toolRequests)
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
		if len(current.toolRequests) != 0 {
			if s.options.Tools == nil {
				return Definition{}, false, failure("controller.unsupported", "target tool inspection is unavailable", "use a compatible executable")
			}
			tools, complete, err := s.options.Tools.Select(current.toolRequests, selected.Sources)
			if err != nil {
				return Definition{}, false, err
			}
			// WithTools canonicalizes tool order by source identity.
			slices.SortFunc(tools, func(a, b ToolDefinition) int { return strings.Compare(a.Source.ID, b.Source.ID) })
			if !complete || !reflect.DeepEqual(tools, selected.Tools) {
				return Definition{}, false, failure("controller.state", "frozen target tools differ from the selected desired state", "restore the exact setup evidence")
			}
		} else if len(selected.Tools) != 0 {
			return Definition{}, false, failure("controller.state", "frozen setup contains unselected target tools", "restore the exact setup evidence")
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
	return Definition{Platform: current.platform, Versions: versions, NativeRequirements: requirements, ToolRequests: slices.Clone(current.toolRequests), PythonVersion: versions.Python, AnsibleVersion: versions.Ansible, Runtime: RuntimeRequirement{Version: versions.Podman}}, false, nil
}

// supersededLatest reports a retained latest resolution whose Ansible release
// no longer meets the collection minimum. A raised minimum is the one update
// setup discovers on its own; a declared release below it refuses instead.
func supersededLatest(value Definition) bool {
	return value.Versions.Ansible == "latest" && ValidateBootstrapAnsibleVersion(value.AnsibleVersion) != nil
}

func dependencyIntent(selection controller.Selection, tools []controller.ToolRequest) []string {
	versions := selection.Versions()
	result := []string{"python=" + versions.Python, "ansible=" + versions.Ansible, "openssh=" + versions.OpenSSH, "nmstate=" + versions.NMState}
	if selection.ContainerRuntime() {
		result = append(result, "podman="+versions.Podman)
	}
	if selection.LibvirtClient() {
		result = append(result, "libvirt="+versions.Libvirt)
	}
	for _, tool := range tools {
		result = append(result, tool.Kind+"="+tool.Version)
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
	ready, err := s.options.NativeInspector.Check(ctx, *definition.Native)
	return RuntimeInspection{Present: ready, Ready: ready}, err
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
func (s Service) resolveDependencies(ctx context.Context, name string, before inspection) (inspection, error) {
	total, step, requests := 2+len(before.toolRequests), 1, before.toolRequests
	var bootstrap BootstrapDefinition
	tools := []ToolDefinition{}
	if before.reusable {
		total, bootstrap, tools, requests = 1, *before.definition.Bootstrap, slices.Clone(before.definition.Tools), nil
	} else {
		err := s.resolutionStep(ctx, &before.report, step, total, "Python and Ansible", func() (string, error) {
			var err error
			bootstrap, err = s.options.Bootstrap.Resolve(ctx, before.platform, before.selection.Versions(), before.route())
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
	for index, request := range requests {
		if s.options.Tools == nil {
			return before, failure("controller.unsupported", "target tool resolution is unavailable", "use a compatible executable")
		}
		err := s.resolutionStep(ctx, &before.report, 3+index, total, "Target tool "+request.Kind, func() (string, error) {
			var resolved []ToolDefinition
			complete := false
			var err error
			if request.Version != "latest" {
				resolved, complete, err = s.options.Tools.Select([]controller.ToolRequest{request}, before.view.State.RetainedSources)
				if err != nil {
					return "", err
				}
			}
			if !complete {
				resolved, err = s.options.Tools.Resolve(ctx, []controller.ToolRequest{request}, before.route())
				if err != nil {
					return "", err
				}
			}
			tools = append(tools, resolved...)
			return toolVersionSummary(resolved), nil
		})
		if err != nil {
			return before, err
		}
	}
	definition, err := NewResolvedDefinition(bootstrap, native, before.toolRequests, tools)
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
	var after inspection
	err = s.storage.ReadController(ctx, name, func(view StorageView) error {
		var err error
		after, err = s.inspect(ctx, view, false, inspectionResolution{Definition: &definition})
		if err != nil {
			return err
		}
		after.report.ProgressPresented = before.report.ProgressPresented
		if before.view.Context != after.view.Context || !before.host.Equal(after.host) || before.selection.Versions() != after.selection.Versions() || !slices.Equal(before.toolRequests, after.toolRequests) || before.view.State.Receipt.ID != after.view.State.Receipt.ID || before.view.State.Receipt.Status != after.view.State.Receipt.Status {
			return failure("controller.conflict", "bastion requirements changed during dependency resolution", setupCommand(name))
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
