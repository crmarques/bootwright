package clients

import (
	"context"
	"slices"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// Capability realizes the controller prerequisites one context adds to the
// host `bootwright setup` prepared: the target clients its graph selects and
// the libvirt client a declared capability or referenced provider selects. It
// owns nothing context-independent, and it never uninstalls: the closure it
// publishes is shared by every context on this host.
type Capability struct {
	tools     ToolCatalog
	native    NativeResolver
	inspector NativeInspector
	installer Installer
}

func New(tools ToolCatalog, native NativeResolver, inspector NativeInspector, installer Installer) Capability {
	return Capability{tools: tools, native: native, inspector: inspector, installer: installer}
}

// Plan contributes one block when the selected graph adds a controller
// prerequisite, and nothing at all when it adds none. Planning is pure: it
// reads no host, contacts no publisher and resolves no release.
func (c Capability) Plan(ctx context.Context, input lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.CapabilityPlan{}, err
	}
	empty := lifecycle.CapabilityPlan{Definitions: []reconciliation.BlockDefinition{}}
	if input.State == nil {
		return empty, refuse("lifecycle.state", "lifecycle planning requires compiled desired state", "")
	}
	catalog := input.State.Effective()
	selection, err := controller.Select(catalog)
	if err != nil {
		return empty, err
	}
	requests, err := controller.SelectTools(catalog)
	if err != nil {
		return empty, err
	}
	if len(requests) == 0 && !selection.LibvirtClient() && !selection.Hypervisor() && !selection.InstallerMedia() {
		return empty, nil
	}
	if selection.MachineName() != input.Controller {
		return empty, refuse("lifecycle.state", "the controller prerequisites block does not name the selected controller Machine", "")
	}
	request := NewRequest(selection, requests)
	canonical, err := request.Canonical()
	if err != nil {
		return empty, err
	}
	return lifecycle.CapabilityPlan{Definitions: []reconciliation.BlockDefinition{{
		ID:             BlockID,
		Description:    description(input.Verb, selection.EnvironmentName(), request.Machine),
		Stage:          reconciliation.StageController,
		Impacts:        impacts(input.Verb, request),
		Groups:         groups(input.Verb, request.Machine),
		Kind:           Kind,
		Object:         selection.EnvironmentName(),
		Implementation: Implementation,
		ContentDigest:  ContentDigest(),
		Request:        canonical,
	}}}, nil
}

// description says what the planned verb does here. A removal retains, so it
// says so rather than borrowing the install wording for a block that takes
// nothing away.
func description(verb reconciliation.Verb, environment, machine string) string {
	if verb == reconciliation.Destroy {
		return "retain the shared clients of " + environment + " on " + machine
	}
	return "install the controller prerequisites of " + environment + " on " + machine
}

// impacts is empty for a removal: this block uninstalls nothing, so a plan that
// listed anything here would promise an effect the destroy never performs.
func impacts(verb reconciliation.Verb, request Request) []string {
	impacts := []string{}
	if verb == reconciliation.Destroy {
		return impacts
	}
	for _, tool := range request.Tools {
		impacts = append(impacts, "publish-target-client "+tool.Kind+" "+tool.Version)
	}
	if request.LibvirtClient {
		impacts = append(impacts, "install-native-client libvirt "+request.Libvirt)
	}
	if request.Hypervisor {
		impacts = append(impacts, "install-native-closure hypervisor "+request.Libvirt)
	}
	if request.InstallerMedia {
		impacts = append(impacts, "install-native-closure installer-media latest")
	}
	slices.Sort(impacts)
	return slices.Compact(impacts)
}

func groups(verb reconciliation.Verb, machine string) []reconciliation.Group {
	steps := [][2]string{
		{"resolve-clients", "resolve the exact client releases this context selects"},
		{"publish-clients", "install the native clients and publish the target clients"},
		{"verify-clients", "verify every selected client is present"},
	}
	if verb == reconciliation.Destroy {
		steps = [][2]string{{"retain-clients", "retain the shared clients this host's other contexts may need"}}
	}
	out := make([]reconciliation.Group, 0, len(steps))
	for _, step := range steps {
		out = append(out, reconciliation.Group{ID: step[0], Description: step[1], Machines: []string{machine}})
	}
	return out
}

// Removal reads a frozen controller-stage block as its own removal. The clients
// it installed are shared host state a context never uninstalls, so removing it
// takes back nothing and consumes nothing.
func (c Capability) Removal(ctx context.Context, block reconciliation.Block) (lifecycle.Removal, error) {
	request, err := DecodeRequest(block.Request)
	if err != nil {
		return lifecycle.Removal{}, err
	}
	return lifecycle.Removal{
		Description: description(reconciliation.Destroy, block.Object, request.Machine),
		Impacts:     impacts(reconciliation.Destroy, request),
		Groups:      groups(reconciliation.Destroy, request.Machine),
	}, nil
}

// Destroy removes nothing. Clients are shared host dependencies that outlive
// the context that selected them, exactly as the setup bundle does, so the
// inverse of this block is the explicit decision to retain them.
func (c Capability) Destroy(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}, err
	}
	request, err := DecodeRequest(execution.Block.Request)
	if err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeFailed}, err
	}
	report(ctx, execution, "retain-clients", "running")
	encoded, err := retainedEvidence(request, execution.Block.RequestDigest)
	if err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeFailed}, err
	}
	report(ctx, execution, "retain-clients", "ok")
	return lifecycle.Result{Outcome: reconciliation.OutcomeUnchanged, Evidence: encoded}, nil
}

// ObserveRemoval runs nothing and reads no host state. The removal takes
// nothing back, so resolving it is always its completion, with the evidence
// Destroy publishes.
func (c Capability) ObserveRemoval(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	unknown := lifecycle.Observation{Effect: reconciliation.EffectUnknown}
	if err := ctx.Err(); err != nil {
		return unknown, err
	}
	request, err := DecodeRequest(execution.Block.Request)
	if err != nil {
		return unknown, err
	}
	encoded, err := retainedEvidence(request, execution.Block.RequestDigest)
	if err != nil {
		return unknown, err
	}
	return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Evidence: encoded}, nil
}

// retainedEvidence is the one statement of what removing this block proves,
// that it retained the shared closure, so Destroy and a removal's resolution
// can never publish two different ones.
func retainedEvidence(request Request, digest string) ([]byte, error) {
	return Evidence{Area: "", Libvirt: request.LibvirtClient, Request: digest, Retained: true, Roots: []string{}, Tools: []ToolRecord{}}.encode()
}

// Observe reads live evidence only. The block's effect is the complete
// selected closure, so anything less than all of it is a positive absence
// whose retry is the same idempotent publication; an observation that cannot
// be made at all stays unknown.
func (c Capability) Observe(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	observation, err := c.observe(ctx, execution)
	return observation, inStage(execution, err)
}

func (c Capability) observe(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	unknown := lifecycle.Observation{Effect: reconciliation.EffectUnknown}
	request, err := DecodeRequest(execution.Block.Request)
	if err != nil {
		return unknown, err
	}
	setup, err := foundation(execution)
	if err != nil {
		return unknown, err
	}
	tools, complete, err := c.retained(execution, request)
	if err != nil {
		return unknown, err
	}
	native := retainedNative(execution, setup.Platform, request)
	roots, installed, err := c.nativeRoots(ctx, native)
	if err != nil {
		return unknown, err
	}
	present := complete && (!request.installsNative() || installed)
	if present {
		if present, err = c.published(ctx, execution, tools); err != nil {
			return unknown, err
		}
	}
	evidence, err := newEvidence(execution.Block.RequestDigest, prerequisites.ToolsDigest(tools), request.LibvirtClient, tools, roots).encode()
	if err != nil {
		return unknown, err
	}
	if present {
		return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Evidence: evidence}, nil
	}
	return lifecycle.Observation{Effect: reconciliation.EffectNoEffect, Evidence: evidence}, nil
}

// Apply installs exactly what is missing. A host that already carries the
// selected closure is proved from retained identities alone, so a repeated
// apply contacts no publisher and reads no repository metadata.
func (c Capability) Apply(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	result, err := c.apply(ctx, execution)
	return result, inStage(execution, err)
}

// inStage names this stage, not setup, as what settles a failure the adapters
// it shares with setup raise, because setup installs nothing a context
// selects.
func inStage(execution lifecycle.Execution, err error) error {
	if err == nil || execution.Stage == nil {
		return err
	}
	return prerequisites.InStage(err, execution.Stage.Setup.Context.Name)
}

func (c Capability) apply(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	failed := lifecycle.Result{Outcome: reconciliation.OutcomeFailed}
	if err := ctx.Err(); err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}, err
	}
	if c.tools == nil || c.native == nil || c.inspector == nil || c.installer == nil {
		return failed, refuse("controller.unsupported", "controller prerequisite installation is not configured", "use a compatible executable")
	}
	if execution.Stage == nil || execution.Stage.ClientArea == nil || execution.Stage.SealClientArea == nil || execution.Stage.RetainDependencies == nil || execution.Stage.Prepare == nil || execution.Stage.ReleaseFoundation == nil {
		return failed, refuse("lifecycle.state", "the controller prerequisites block has no publication capability", "")
	}
	request, err := DecodeRequest(execution.Block.Request)
	if err != nil {
		return failed, err
	}
	setup, err := foundation(execution)
	if err != nil {
		return failed, err
	}
	report(ctx, execution, "resolve-clients", "running")
	tools, complete, err := c.retained(execution, request)
	if err != nil {
		return failed, err
	}
	native := retainedNative(execution, setup.Platform, request)
	roots, installed, err := c.nativeRoots(ctx, native)
	if err != nil {
		return failed, err
	}
	if complete && (!request.installsNative() || installed) {
		present, err := c.published(ctx, execution, tools)
		if err != nil {
			return failed, err
		}
		if present {
			return c.settled(ctx, execution, request, tools, roots)
		}
	}
	if !complete {
		if tools, err = c.tools.Resolve(ctx, request.ToolRequests(), request.Egress); err != nil {
			return failed, err
		}
	}
	// A frozen native transaction binds the exact before-inventory it was
	// solved from, so a missing root is solved again rather than replayed, and
	// roots already installed need no transaction at all.
	var transaction *prerequisites.Definition
	var superseded []string
	if request.installsNative() && !installed {
		value, err := c.resolveNative(ctx, setup, request)
		if err != nil {
			return failed, err
		}
		transaction, native = &value, &value
		superseded = supersededNative(execution, setup.Platform, request, value)
	}
	report(ctx, execution, "resolve-clients", "ok")
	install, err := installDefinition(setup, transaction, tools)
	if err != nil {
		return failed, err
	}
	// Intent precedes acquisition: the exact identities this attempt will
	// acquire are durable before a single byte is downloaded, so a later
	// inspection can always name the closure an interrupted attempt chose.
	if err := execution.Stage.RetainDependencies(ctx, transaction, install.Sources, superseded); err != nil {
		return failed, err
	}
	return c.publish(ctx, execution, request, install, tools, native)
}

// publish opens the shared area under its durable reservation, runs the fixed
// controller automation, then proves and seals what it published.
func (c Capability) publish(ctx context.Context, execution lifecycle.Execution, request Request, install prerequisites.Definition, tools []prerequisites.ToolDefinition, native *prerequisites.Definition) (lifecycle.Result, error) {
	failed := lifecycle.Result{Outcome: reconciliation.OutcomeFailed}
	unknown := lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}
	area := prerequisites.ToolsDigest(tools)
	target, err := execution.Stage.ClientArea(ctx, area)
	if err != nil {
		return failed, err
	}
	report(ctx, execution, "publish-clients", "running")
	// The execution foundation this block runs under holds the native package
	// read lock. A native client transaction needs the write lock, so the
	// installer takes over that coordination from here.
	result, err := c.installer.Clients(ctx, prerequisites.ClientInstallation{
		Execution:  execution.Area,
		Target:     target,
		Platform:   install.Platform,
		Definition: install,
		Egress:     request.Egress,
		Launch:     execution.Launch,
		Release:    execution.Stage.ReleaseFoundation,
		Publish:    execution.Stage.Prepare,
		Progress: func(event prerequisites.ProgressEvent) {
			report(ctx, execution, "publish-clients", event.Status)
		},
		Output: execution.Output,
	})
	if err != nil {
		if result.Outcome == "failed" {
			report(ctx, execution, "publish-clients", "failed")
			return failed, err
		}
		return unknown, err
	}
	if result.Outcome != "changed" && result.Outcome != "unchanged" {
		return unknown, refuse("controller.unknown", "the controller prerequisites adapter reported no usable outcome", "repeat the operation to resolve it from live evidence")
	}
	report(ctx, execution, "publish-clients", "ok")
	report(ctx, execution, "verify-clients", "running")
	present, err := c.tools.Present(ctx, target, tools)
	if err != nil {
		return unknown, err
	}
	roots, installed, err := c.nativeRoots(ctx, native)
	if err != nil {
		return unknown, err
	}
	if request.installsNative() && !installed {
		return unknown, refuse("controller.unknown", "the selected native clients are not installed after their transaction", "repeat the operation to resolve it from live evidence")
	}
	if !present {
		return unknown, refuse("controller.unknown", "the published target clients could not be verified", "repeat the operation to resolve it from live evidence")
	}
	if err := execution.Stage.SealClientArea(ctx, area); err != nil {
		return unknown, err
	}
	report(ctx, execution, "verify-clients", "ok")
	evidence, err := newEvidence(execution.Block.RequestDigest, area, request.LibvirtClient, tools, roots).encode()
	if err != nil {
		return unknown, err
	}
	return lifecycle.Result{Outcome: reconciliation.OutcomeChanged, Evidence: evidence}, nil
}

// settled is the proved no-op: every selected client is already published and
// installed, so the block completes without acquisition or effect.
func (c Capability) settled(ctx context.Context, execution lifecycle.Execution, request Request, tools []prerequisites.ToolDefinition, roots []prerequisites.NativeRootPresence) (lifecycle.Result, error) {
	report(ctx, execution, "resolve-clients", "ok")
	report(ctx, execution, "verify-clients", "ok")
	evidence, err := newEvidence(execution.Block.RequestDigest, prerequisites.ToolsDigest(tools), request.LibvirtClient, tools, roots).encode()
	if err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeFailed}, err
	}
	return lifecycle.Result{Outcome: reconciliation.OutcomeUnchanged, Evidence: evidence}, nil
}

// retained recovers the exact closure from identities this host already holds.
// It reads no publisher metadata, so an inspection never acquires anything.
func (c Capability) retained(execution lifecycle.Execution, request Request) ([]prerequisites.ToolDefinition, bool, error) {
	if len(request.Tools) == 0 {
		return []prerequisites.ToolDefinition{}, true, nil
	}
	return c.tools.Select(request.ToolRequests(), execution.Stage.Setup.State.RetainedSources)
}

// LocateTool answers, for every other block of the plan this frozen stage
// block belongs to, where the closure that block proved published one
// executable. proved is what it recorded in the apply the asking operation
// runs over, so a consumer runs the file its own context's stage installed.
// No retained resolution names a client closure, so the closure is recovered
// from retained identities, but only from those the proof names: the host's
// sources are shared, and the newest release under a latest intent may be one
// another context retained and this one never published.
func (c Capability) LocateTool(ctx context.Context, view prerequisites.StorageView, block reconciliation.Block, proved lifecycle.BlockEvidence, tool controller.InstalledTool) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if c.tools == nil {
		return "", refuse("controller.unsupported", "controller prerequisite installation is not configured", "use a compatible executable")
	}
	request, err := DecodeRequest(block.Request)
	if err != nil {
		return "", err
	}
	tools, err := c.provedClosure(request, block, proved, view.State.RetainedSources)
	if err != nil {
		return "", err
	}
	return prerequisites.LocateInstalledTool(ctx, view, tools, tool)
}

// provedClosure is the exact closure a completed apply of this block proved,
// or none. Only the retained sources whose bytes that proof names are read, so
// each client is recovered at the release the proof names; a recovery that is
// not whole, or that names an area other than the proved one, is no closure.
func (c Capability) provedClosure(request Request, block reconciliation.Block, proved lifecycle.BlockEvidence, retained []prerequisites.DependencySource) ([]prerequisites.ToolDefinition, error) {
	if proved.State != reconciliation.BlockDone {
		return nil, nil
	}
	evidence, err := reconciliation.DecodeEvidence[Evidence](proved.Evidence, maxEvidenceBytes, "controller prerequisites")
	if err != nil || evidence.Retained || evidence.Area == "" || evidence.Request != block.RequestDigest {
		return nil, nil
	}
	named := map[string]bool{}
	for _, record := range evidence.Tools {
		named[record.SHA256] = true
	}
	sources := []prerequisites.DependencySource{}
	for _, source := range retained {
		if named[source.SHA256] {
			sources = append(sources, source)
		}
	}
	tools, complete, err := c.tools.Select(request.ToolRequests(), sources)
	if err != nil {
		return nil, err
	}
	if !complete || prerequisites.ToolsDigest(tools) != evidence.Area {
		return nil, nil
	}
	return tools, nil
}

// nativeRoots reports each selected native client root that is installed, by
// name. Without a resolution nothing installed them, so their absence is
// definite rather than unverifiable.
func (c Capability) nativeRoots(ctx context.Context, native *prerequisites.Definition) ([]prerequisites.NativeRootPresence, bool, error) {
	if native == nil {
		return []prerequisites.NativeRootPresence{}, false, nil
	}
	presence, err := c.inspector.Check(ctx, *native.Native)
	if err != nil {
		return []prerequisites.NativeRootPresence{}, false, err
	}
	return presence.Installed, presence.Ready, nil
}

// published proves the target clients by presence alone. It opens the shared
// area read-only through the setup view, so an inspection can neither reserve
// nor create one.
func (c Capability) published(ctx context.Context, execution lifecycle.Execution, tools []prerequisites.ToolDefinition) (bool, error) {
	if len(tools) == 0 {
		return true, nil
	}
	if execution.Stage.Setup.OpenBundle == nil {
		return false, refuse("controller.state", "the retained controller areas are unavailable", "run bootwright setup")
	}
	area, err := execution.Stage.Setup.OpenBundle(ctx, prerequisites.ToolsDigest(tools))
	if err != nil || area == nil {
		return false, err
	}
	return c.tools.Present(ctx, area, tools)
}

// resolveNative solves the native client transaction against this host's
// current inventory. A frozen transaction binds the exact before-inventory it
// was solved from, so it is solved again whenever a root is missing.
func (c Capability) resolveNative(ctx context.Context, setup prerequisites.Definition, request Request) (prerequisites.Definition, error) {
	requirements := prerequisites.NativeRequirements{
		ContainerRuntime: true, LibvirtClient: true,
		Hypervisor: request.Hypervisor, InstallerMedia: request.InstallerMedia,
	}
	plan, err := c.native.Resolve(ctx, setup.Platform, requirements, request.Versions(), request.Egress)
	if err != nil {
		return prerequisites.Definition{}, err
	}
	return prerequisites.NewResolvedDefinition(*setup.Bootstrap, plan)
}

// retainedNative recovers a native client resolution this host already froze.
// Without one nothing installed those roots, so their absence is definite.
func retainedNative(execution lifecycle.Execution, platform prerequisites.Platform, request Request) *prerequisites.Definition {
	if !request.installsNative() {
		return nil
	}
	retained := execution.Stage.Setup.State.RetainedDefinitions
	for index := len(retained) - 1; index >= 0; index-- {
		if !sameNativeSelection(retained[index], platform, request) {
			continue
		}
		definition := prerequisites.CloneDefinition(retained[index])
		return &definition
	}
	return nil
}

// sameNativeSelection reports a retained native client resolution this
// request would read. Only the latest of them is ever read.
func sameNativeSelection(value prerequisites.Definition, platform prerequisites.Platform, request Request) bool {
	return value.Native != nil && value.NativeRequirements.LibvirtClient && value.Platform == platform &&
		value.NativeRequirements.Hypervisor == request.Hypervisor && value.NativeRequirements.InstallerMedia == request.InstallerMedia &&
		value.Versions.Libvirt == request.Versions().Libvirt
}

// supersededNative names every retained resolution of this request's selection
// but the one it now retains. Once that one is retained it is the latest, so no
// stage reads the others again, and a stage that solves again whenever a root
// is missing would otherwise fill the host's bound of retained resolutions.
func supersededNative(execution lifecycle.Execution, platform prerequisites.Platform, request Request, retained prerequisites.Definition) []string {
	var superseded []string
	for _, value := range execution.Stage.Setup.State.RetainedDefinitions {
		if sameNativeSelection(value, platform, request) && value.ResolutionDigest != retained.ResolutionDigest {
			superseded = append(superseded, value.ResolutionDigest)
		}
	}
	return superseded
}

// foundation is the host resolution setup froze. This stage extends it; it
// never resolves the Python, Ansible or baseline native closure again.
func foundation(execution lifecycle.Execution) (prerequisites.Definition, error) {
	view := execution.Stage.Setup
	if !view.Exists || !view.Initialized {
		return prerequisites.Definition{}, refuse("controller.identity", "this host has no completed controller setup", "run bootwright setup")
	}
	receipt := view.State.Receipt
	if receipt.Status != "complete" || receipt.Definition == nil || receipt.Definition.Bootstrap == nil {
		return prerequisites.Definition{}, refuse("controller.state", "the retained controller setup has no execution definition", "run bootwright setup")
	}
	return prerequisites.CloneDefinition(*receipt.Definition), nil
}

// installDefinition is the exact closure the automation realizes: the host's
// own execution foundation, the native client transaction when one is
// selected, and the resolved target clients.
func installDefinition(setup prerequisites.Definition, native *prerequisites.Definition, tools []prerequisites.ToolDefinition) (prerequisites.Definition, error) {
	base := setup
	if native != nil {
		base = prerequisites.CloneDefinition(*native)
	} else {
		base.Native, base.NativeRequirements = nil, prerequisites.NativeRequirements{}
	}
	base.Tools, base.BaseCatalogDigest = nil, ""
	return prerequisites.WithTools(base, tools)
}

func report(ctx context.Context, execution lifecycle.Execution, group, status string) {
	if execution.Progress != nil && status != "" {
		execution.Progress(ctx, group, status)
	}
}

// Quiescent is always settled: this block's removal retains the shared client
// closure it published rather than deleting it, so nothing it owns can be
// taken away from anything still using it.
func (Capability) Quiescent(context.Context, lifecycle.Probe) (lifecycle.Quiescence, error) {
	return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: "its removal retains the shared closure"}, nil
}
