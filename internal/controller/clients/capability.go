package clients

import (
	"context"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// Capability realizes the controller prerequisites one context adds to the
// host `bootwright setup` prepared: the target clients its graph selects, and
// the host packages of the libvirt client a declared capability, a hosted
// provider or a Machine on a libvirt provider selects, of the hypervisor
// closure a hosted provider selects and of the installer-media tooling an
// artifact server here selects. It owns nothing context-independent, and it
// never uninstalls: what it installs is shared by every context on this host.
type Capability struct {
	tools     ToolCatalog
	packages  HostPackages
	setup     SetupLock
	installer Installer
}

func New(tools ToolCatalog, packages HostPackages, setup SetupLock, installer Installer) Capability {
	return Capability{tools: tools, packages: packages, setup: setup, installer: installer}
}

// Plan contributes one block when the selected graph adds a controller
// prerequisite, and nothing at all when it adds none. Planning is pure: it
// reads no host, contacts no publisher and resolves no release, so it presents
// the host package names the selected closures require, not which are missing.
func (c Capability) Plan(ctx context.Context, input lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.CapabilityPlan{}, err
	}
	empty := lifecycle.CapabilityPlan{Definitions: []reconciliation.BlockDefinition{}}
	if input.State == nil {
		return empty, refuse("lifecycle.state", "lifecycle planning requires compiled desired state", "use a compatible executable")
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
	if !controller.HasStage(selection, requests) {
		return empty, nil
	}
	if selection.MachineName() != input.Controller {
		return empty, refuse("lifecycle.state", "the controller prerequisites block does not name the selected controller Machine", "use a compatible executable")
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

// Unsupported refuses, before registration, every controller shape this
// executable cannot realize. Each refusal names the Environment this
// capability plans for, with the shape's own reason and remedy, which name the
// Machine or Proxy that declares it. It is pure and reads no host.
func (Capability) Unsupported(state *compilation.State) []lifecycle.Refusal {
	if state == nil {
		return nil
	}
	catalog := state.Effective()
	environments := catalog.OfKind(api.Environment)
	if len(environments) != 1 {
		return nil
	}
	var refusals []lifecycle.Refusal
	for _, shape := range controller.Unsupported(catalog) {
		refusals = append(refusals, lifecycle.RefusalOf(environments[0], shape.Reason, shape.Remediation))
	}
	return refusals
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
// Each host package is named as its requirement; one already present is left
// as it is.
func impacts(verb reconciliation.Verb, request Request) []string {
	impacts := []string{}
	if verb == reconciliation.Destroy {
		return impacts
	}
	for _, tool := range request.Tools {
		impacts = append(impacts, "publish-target-client "+tool.Kind+" "+tool.Version)
	}
	for _, requirement := range request.Packages {
		impacts = append(impacts, "install-host-package "+requirement)
	}
	slices.Sort(impacts)
	return slices.Compact(impacts)
}

func groups(verb reconciliation.Verb, machine string) []reconciliation.Group {
	steps := [][2]string{
		{"resolve-clients", "read the missing host packages and resolve the exact client releases this context selects"},
		{"publish-clients", "install the missing host packages and publish the target clients"},
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
// the context that selected them, exactly as setup's packages do, so the
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
	return Evidence{Area: "", Libvirt: request.LibvirtClient, Packages: []PackageRecord{}, Request: digest, Retained: true, Tools: []ToolRecord{}}.encode()
}

// Observe reads live evidence only. The block's effect is the complete
// selected closure, so anything less than all of it is a positive absence
// whose retry is the same idempotent installation; an observation that cannot
// be made at all stays unknown.
func (c Capability) Observe(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	observation, err := c.observe(ctx, execution)
	return observation, inStage(execution, err)
}

func (c Capability) observe(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	unknown := lifecycle.Observation{Effect: reconciliation.EffectUnknown}
	if execution.Stage == nil {
		return unknown, refuse("lifecycle.state", "the controller prerequisites block has no publication capability", "use a compatible executable")
	}
	request, err := admitted(execution)
	if err != nil {
		return unknown, err
	}
	host, err := c.host(request)
	if err != nil {
		return unknown, err
	}
	installed, missing, err := present(ctx, host, request.Packages, false)
	if err != nil {
		return unknown, err
	}
	tools, complete, err := c.retained(execution, request)
	if err != nil {
		return unknown, err
	}
	ready := complete && len(missing) == 0
	if ready {
		if ready, err = c.published(ctx, execution, tools); err != nil {
			return unknown, err
		}
	}
	evidence, err := newEvidence(execution.Block.RequestDigest, prerequisites.ToolsDigest(tools), request.LibvirtClient, tools, installed).encode()
	if err != nil {
		return unknown, err
	}
	if ready {
		return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Evidence: evidence}, nil
	}
	return lifecycle.Observation{Effect: reconciliation.EffectNoEffect, Evidence: evidence}, nil
}

// Apply installs exactly what is missing. A host that already carries the
// selected closure is proved from presence and retained identities alone, so
// a repeated apply contacts no publisher and runs no dnf.
func (c Capability) Apply(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	result, err := c.apply(ctx, execution)
	return result, inStage(execution, err)
}

// liveResolution is the remedy of an installation whose outcome is unknown:
// the exact continuation of its apply, which resolves it read-only first, or
// the operation's repetition when the attempt carries none.
func liveResolution(execution lifecycle.Execution) string {
	if execution.Continuation == "" {
		return "repeat the operation to resolve it from live evidence"
	}
	return "resolve it from live evidence with " + execution.Continuation
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
	if c.tools == nil || c.installer == nil {
		return failed, refuse("controller.unsupported", "controller prerequisite installation is not configured", "use a compatible executable")
	}
	if execution.Stage == nil || execution.Stage.ClientArea == nil || execution.Stage.SealClientArea == nil || execution.Stage.RetireToolAreas == nil || execution.Stage.RetainDependencies == nil || execution.Stage.Prepare == nil {
		return failed, refuse("lifecycle.state", "the controller prerequisites block has no publication capability", "use a compatible executable")
	}
	request, err := admitted(execution)
	if err != nil {
		return failed, err
	}
	report(ctx, execution, "resolve-clients", "running")
	host, err := c.host(request)
	if err != nil {
		return failed, err
	}
	installed, missing, err := present(ctx, host, request.Packages, false)
	if err != nil {
		return failed, err
	}
	tools, complete, err := c.retained(execution, request)
	if err != nil {
		return failed, err
	}
	published := false
	if complete {
		if published, err = c.published(ctx, execution, tools); err != nil {
			return failed, err
		}
	} else if tools, err = c.tools.Resolve(ctx, request.ToolRequests(), request.Egress); err != nil {
		return failed, err
	}
	report(ctx, execution, "resolve-clients", "ok")
	if published && len(missing) == 0 {
		return c.settled(ctx, execution, request, tools, installed)
	}
	preparation := prerequisites.StagePreparation{Packages: slices.Clone(missing), AddedSources: []string{}}
	if !published {
		preparation.AddedSources = toolSources(tools)
	}
	// Intent precedes every effect: the host packages this attempt will ask
	// dnf for and the exact tool identities it will acquire are durable before
	// dnf runs or a single byte is downloaded.
	if len(preparation.AddedSources) != 0 {
		if err := execution.Stage.RetainDependencies(ctx, sources(tools)); err != nil {
			return failed, err
		}
	}
	if err := execution.Stage.Prepare(ctx, preparation); err != nil {
		return failed, err
	}
	report(ctx, execution, "publish-clients", "running")
	if len(missing) != 0 {
		if result, err := c.install(ctx, execution, host, missing); err != nil {
			return result, err
		}
	}
	area := prerequisites.ToolsDigest(tools)
	var target prerequisites.BundleArea
	if len(preparation.AddedSources) != 0 {
		if target, err = execution.Stage.ClientArea(ctx, area); err != nil {
			return failed, err
		}
		if result, err := c.publish(ctx, execution, request, target, tools, preparation.AddedSources); err != nil {
			return result, err
		}
	}
	report(ctx, execution, "publish-clients", "ok")
	return c.verify(ctx, execution, request, host, tools, area, target)
}

// admitted decodes the frozen request and refuses a libvirt release intent
// the host's own repositories decide.
func admitted(execution lifecycle.Execution) (Request, error) {
	request, err := DecodeRequest(execution.Block.Request)
	if err != nil {
		return Request{}, err
	}
	if request.Libvirt != "" && request.Libvirt != controller.DefaultDependencyVersions().Libvirt {
		return Request{}, refuse("controller.unsupported", "the controller stage installs the libvirt release this host's repositories provide, and "+execution.Block.Object+" pins spec.dependencyVersions.libvirt to "+request.Libvirt,
			"remove spec.dependencyVersions.libvirt from "+execution.Block.Object)
	}
	return request, nil
}

// host is the host's package manager for this request, or nil when it selects
// no host package and nothing is asked of dnf.
func (c Capability) host(request Request) (prerequisites.HostPackages, error) {
	if len(request.Packages) == 0 {
		return nil, nil
	}
	if c.packages == nil {
		return nil, refuse("controller.unsupported", "host package installation is not configured", "use a compatible executable")
	}
	return c.packages(request.Egress)
}

// present reads which requirements the host provides and names the others.
// Only a read after an install asks dnf for the repositories; before one, rpm
// alone answers, so a ready host runs no dnf.
func present(ctx context.Context, host prerequisites.HostPackages, requirements []string, installed bool) ([]prerequisites.HostPackageEvidence, []string, error) {
	if len(requirements) == 0 {
		return nil, []string{}, nil
	}
	read := host.Installed
	if installed {
		read = host.Present
	}
	providers, err := read(ctx, requirements)
	if err != nil {
		return nil, nil, err
	}
	missing := []string{}
	for _, requirement := range requirements {
		if !slices.ContainsFunc(providers, func(found prerequisites.HostPackageEvidence) bool { return found.Requirement == requirement }) {
			missing = append(missing, requirement)
		}
	}
	return providers, missing, nil
}

// install runs the host's dnf as root under setup/lock: presence is read again
// under the lock, the preview refuses what the repositories cannot provide
// before anything changes, and the install's output belongs to this attempt.
func (c Capability) install(ctx context.Context, execution lifecycle.Execution, host prerequisites.HostPackages, missing []string) (lifecycle.Result, error) {
	failed := lifecycle.Result{Outcome: reconciliation.OutcomeFailed}
	if c.setup == nil {
		return failed, refuse("controller.unsupported", "host package installation is not configured", "use a compatible executable")
	}
	started := false
	err := c.setup.MutateSetup(ctx, func(tx prerequisites.SetupTransaction) error {
		if err := prerequisites.SetupLaunchRefusal(tx.View()); err != nil {
			return err
		}
		still, err := host.Missing(ctx, missing)
		if err != nil || len(still) == 0 {
			return err
		}
		preview, err := host.Preview(ctx, still)
		if err != nil {
			return err
		}
		if execution.Output != nil {
			_, _ = execution.Output.Write([]byte(preview.Text))
		}
		started = true
		_, err = host.Install(ctx, still, execution.Output)
		return err
	})
	if err == nil {
		return lifecycle.Result{}, nil
	}
	report(ctx, execution, "publish-clients", "failed")
	if started {
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}, err
	}
	return failed, err
}

// publish opens the shared area under its durable reservation and runs the
// fixed controller automation, which acquires only the sources this attempt
// already recorded.
func (c Capability) publish(ctx context.Context, execution lifecycle.Execution, request Request, target prerequisites.BundleArea, tools []prerequisites.ToolDefinition, recorded []string) (lifecycle.Result, error) {
	failed := lifecycle.Result{Outcome: reconciliation.OutcomeFailed}
	unknown := lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}
	result, err := c.installer.Clients(ctx, prerequisites.ClientInstallation{
		Target:   target,
		Identity: execution.Block.RequestDigest,
		Tools:    tools,
		Egress:   request.Egress,
		Launch:   execution.Launch,
		Publish: func(_ context.Context, added []string) error {
			if !slices.Equal(added, recorded) {
				return refuse("controller.unknown", "the controller automation announced sources this attempt did not record", liveResolution(execution))
			}
			return nil
		},
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
		return unknown, refuse("controller.unknown", "the controller prerequisites adapter reported no usable outcome", liveResolution(execution))
	}
	return lifecycle.Result{}, nil
}

// verify proves the whole closure after its effects: every host package
// requirement provided, with the NEVRAs and repositories that are its
// evidence, and the tools this attempt published, when it published any,
// present in their sealed area.
func (c Capability) verify(ctx context.Context, execution lifecycle.Execution, request Request, host prerequisites.HostPackages, tools []prerequisites.ToolDefinition, area string, target prerequisites.BundleArea) (lifecycle.Result, error) {
	unknown := lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}
	report(ctx, execution, "verify-clients", "running")
	installed, missing, err := present(ctx, host, request.Packages, true)
	if err != nil {
		return unknown, err
	}
	if len(missing) != 0 {
		return unknown, refuse("controller.unknown", "the selected host packages are not installed after their install", liveResolution(execution))
	}
	if target != nil {
		present, err := c.tools.Present(ctx, target, tools)
		if err != nil {
			return unknown, err
		}
		if !present {
			return unknown, refuse("controller.unknown", "the published target clients could not be verified", liveResolution(execution))
		}
		if err := execution.Stage.SealClientArea(ctx, area); err != nil {
			return unknown, err
		}
	}
	if err := execution.Stage.RetireToolAreas(ctx, current(tools, area)); err != nil {
		return unknown, err
	}
	report(ctx, execution, "verify-clients", "ok")
	evidence, err := newEvidence(execution.Block.RequestDigest, area, request.LibvirtClient, tools, installed).encode()
	if err != nil {
		return unknown, err
	}
	return lifecycle.Result{Outcome: reconciliation.OutcomeChanged, Evidence: evidence}, nil
}

// settled is the proved no-op: every selected client is already published and
// installed, so the block completes without acquisition or host effect; its
// binding names the area it proved and the unnamed areas are retired.
func (c Capability) settled(ctx context.Context, execution lifecycle.Execution, request Request, tools []prerequisites.ToolDefinition, installed []prerequisites.HostPackageEvidence) (lifecycle.Result, error) {
	if err := execution.Stage.RetireToolAreas(ctx, current(tools, prerequisites.ToolsDigest(tools))); err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeFailed}, err
	}
	report(ctx, execution, "verify-clients", "ok")
	evidence, err := newEvidence(execution.Block.RequestDigest, prerequisites.ToolsDigest(tools), request.LibvirtClient, tools, installed).encode()
	if err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeFailed}, err
	}
	return lifecycle.Result{Outcome: reconciliation.OutcomeUnchanged, Evidence: evidence}, nil
}

// current is the area a stage's binding names: none when it selects no tool.
func current(tools []prerequisites.ToolDefinition, area string) string {
	if len(tools) == 0 {
		return ""
	}
	return area
}

func sources(tools []prerequisites.ToolDefinition) []prerequisites.DependencySource {
	out := make([]prerequisites.DependencySource, 0, len(tools))
	for _, tool := range tools {
		out = append(out, tool.Source)
	}
	return out
}

func toolSources(tools []prerequisites.ToolDefinition) []string {
	out := make([]string, 0, len(tools))
	for _, tool := range tools {
		out = append(out, tool.Source.ID)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// retained recovers the exact closure from identities this host already holds.
// It reads no publisher metadata, so an inspection never acquires anything.
func (c Capability) retained(execution lifecycle.Execution, request Request) ([]prerequisites.ToolDefinition, bool, error) {
	if len(request.Tools) == 0 {
		return []prerequisites.ToolDefinition{}, true, nil
	}
	if c.tools == nil {
		return nil, false, refuse("controller.unsupported", "controller prerequisite installation is not configured", "use a compatible executable")
	}
	return c.tools.Select(request.ToolRequests(), execution.Stage.Setup.State.ToolSources)
}

// LocateTool answers, for every other block of the plan this frozen stage
// block belongs to, where the closure that block proved published one
// executable. proved is what it recorded in the apply the asking operation
// runs over, so a consumer runs the file its own context's stage installed.
// The closure is recovered from retained identities, but only from those the
// proof names: the host's sources are shared, and the newest release under a
// latest intent may be one another context retained and this one never
// published.
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
	tools, err := c.provedClosure(request, block, proved, view.State.ToolSources)
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
