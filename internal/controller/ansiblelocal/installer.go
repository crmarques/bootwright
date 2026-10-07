package ansiblelocal

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/crmarques/bootwright/ansible"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

type Installer struct {
	ExecutionGuard prerequisites.PythonExecutionGuard
	runner         ansibleRunner
}

// ansibleRunner starts one frozen Ansible request with the output its caller
// retains. New always runs the process itself; a test replaces it to prove
// what each operation hands that Ansible.
type ansibleRunner func(context.Context, prerequisites.PythonLaunch, capabilityRequest, func() error, func(context.Context, prerequisites.NativePreparation) error, func(prerequisites.ProgressEvent), prerequisites.RunOutput) (prerequisites.ActionResult, error)

func New(execution prerequisites.PythonExecutionGuard) Installer {
	return Installer{ExecutionGuard: execution, runner: run}
}

const requestVersion = "controller-prerequisites-v5"

type capabilityRequest struct {
	Version           string                            `json:"version"`
	Operation         string                            `json:"operation"`
	Identity          string                            `json:"identity"`
	Platform          prerequisites.Platform            `json:"platform"`
	Bundle            bundleLocation                    `json:"bundle"`
	PublicationBundle bundleLocation                    `json:"publicationBundle"`
	Packages          []prerequisites.NativePackage     `json:"packages"`
	Native            *prerequisites.NativeResolvedPlan `json:"native"`
	Tools             []prerequisites.ToolDefinition    `json:"tools"`
	Acquisition       []toolAcquisition                 `json:"acquisition"`
	NativeStaging     int64                             `json:"nativeStaging"`
	Egress            prerequisites.SetupEgress         `json:"egress"`
	Preparation       *prerequisites.NativePreparation  `json:"preparation,omitempty"`
}

type bundleLocation struct {
	Path     string `json:"path"`
	Device   uint64 `json:"device"`
	Inode    uint64 `json:"inode"`
	Writable bool   `json:"writable"`
	Sealed   bool   `json:"sealed"`
}

func location(value prerequisites.BundleLocation) bundleLocation {
	return bundleLocation{value.Path, value.Device, value.Inode, value.Writable, value.Sealed}
}

func (installer Installer) Prepare(ctx context.Context, area prerequisites.BundleArea, platform prerequisites.Platform, definition prerequisites.Definition, egress prerequisites.SetupEgress, publish func(context.Context, prerequisites.NativePreparation) error, progress func(prerequisites.ProgressEvent), output prerequisites.RunOutput) (prerequisites.ActionResult, error) {
	return installer.invoke(ctx, area, platform, definition, egress, publish, nil, progress, output)
}

func (installer Installer) Recover(ctx context.Context, area prerequisites.BundleArea, platform prerequisites.Platform, definition prerequisites.Definition, egress prerequisites.SetupEgress, preparation prerequisites.NativePreparation, progress func(prerequisites.ProgressEvent), output prerequisites.RunOutput) (prerequisites.ActionResult, error) {
	preparation.AddedSources = slices.Clone(preparation.AddedSources)
	return installer.invoke(ctx, area, platform, definition, egress, nil, &preparation, progress, output)
}

// Clients installs one context's selected client closure inside an execution
// foundation the caller already holds. Setup publishes into the same bundle it
// executes from; a controller stage never may, so the target tools go to their
// own shared area while the automation still runs from the sealed bundle.
func (installer Installer) Clients(ctx context.Context, installation prerequisites.ClientInstallation) (prerequisites.ActionResult, error) {
	result := actionResult("failed", false)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if installer.runner == nil || installation.Target == nil || installation.Publish == nil || installation.Release == nil {
		return result, failure("controller.setup", "the Ansible controller adapter is incomplete")
	}
	request, err := installer.request(ctx, installation.Execution, installation.Target, "setup", installation.Platform, installation.Definition, installation.Egress, nil)
	if err != nil {
		return result, err
	}
	return installer.runner(ctx, installation.Launch, request, installation.Release, installation.Publish, installation.Progress, swallowing(installation.Output))
}

// swallowing hands the runner an output that never fails, whatever the
// output it wraps does: os/exec stops copying from a writer that fails, and
// the Ansible could then block on a full pipe. Retention is troubleshooting
// material only, so a lost write changes nothing else. A nil output, which
// keeps nothing, stays nil.
func swallowing(output prerequisites.RunOutput) prerequisites.RunOutput {
	if output == nil {
		return nil
	}
	return swallowingOutput{output}
}

type swallowingOutput struct{ output prerequisites.RunOutput }

func (s swallowingOutput) Write(value []byte) (int, error) {
	_, _ = s.output.Write(value)
	return len(value), nil
}

// readingRunOutput leads a failure of setup's own Ansible with where that
// Ansible's output is kept and that only root reads it, because what the
// failure does not say is in that file. A failure of any other kind, or one
// whose output no run kept, is returned unchanged.
func readingRunOutput(err error, output prerequisites.RunOutput) error {
	run, kept := output.(interface{ Location() string })
	var scoped *prerequisites.ScopedFailure
	if !kept || run.Location() == "" || !errors.As(err, &scoped) {
		return err
	}
	read := *scoped
	read.Correction = "Read " + run.Location() + "/run.output as root for what the Ansible printed, then " + sentenceCase(scoped.Correction)
	if err != error(scoped) {
		return errors.Join(&read, err)
	}
	return &read
}

// sentenceCase lowers a correction's first letter where it begins a sentence,
// leaving an initialism such as HTTPS_PROXY as written.
func sentenceCase(correction string) string {
	if len(correction) < 2 || correction[0] < 'A' || correction[0] > 'Z' || correction[1] < 'a' || correction[1] > 'z' {
		return correction
	}
	return string(correction[0]+'a'-'A') + correction[1:]
}

// invoke runs setup's own Ansible, whose output goes to the setup run the
// caller opened for it, or nowhere when it could open none.
func (installer Installer) invoke(ctx context.Context, area prerequisites.BundleArea, platform prerequisites.Platform, definition prerequisites.Definition, egress prerequisites.SetupEgress, publish func(context.Context, prerequisites.NativePreparation) error, preparation *prerequisites.NativePreparation, progress func(prerequisites.ProgressEvent), output prerequisites.RunOutput) (prerequisites.ActionResult, error) {
	result := actionResult("failed", false)
	if preparation != nil {
		result = actionResult("unknown", true)
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if installer.ExecutionGuard == nil || installer.runner == nil || publish == nil && preparation == nil {
		return result, failure("controller.setup", "the Ansible controller adapter is incomplete")
	}
	operation := "setup"
	if preparation != nil {
		operation = "recover"
	}
	request, err := installer.request(ctx, area, nil, operation, platform, definition, egress, preparation)
	if err != nil {
		return result, err
	}
	err = installer.ExecutionGuard.WithPython(ctx, area, prerequisites.LaunchRequirementFor(ctx, definition.Execution), func(launch prerequisites.PythonLaunch, release func() error) error {
		var runErr error
		result, runErr = installer.runner(ctx, launch, request, release, publish, progress, swallowing(output))
		return readingRunOutput(runErr, output)
	})
	return result, err
}

// request freezes the complete Ansible invocation. The execution area supplies
// the approved automation; the target area is the only one publication may
// write, so its writability is what the request has to prove. A nil target
// publishes into the execution area itself, which is what setup does.
func (installer Installer) request(ctx context.Context, execution, target prerequisites.BundleArea, operation string, platform prerequisites.Platform, definition prerequisites.Definition, egress prerequisites.SetupEgress, preparation *prerequisites.NativePreparation) (capabilityRequest, error) {
	if execution == nil {
		return capabilityRequest{}, failure("controller.setup", "the Ansible controller adapter is incomplete")
	}
	for name, expected := range ansible.Automation() {
		actual, err := execution.Read(ctx, "automation/"+name, len(expected)+1)
		if err != nil || !slices.Equal(actual, expected) {
			return capabilityRequest{}, failure("controller.identity", "the embedded controller automation does not match the approved execution bundle")
		}
	}
	source, err := execution.Location(ctx)
	if err != nil {
		return capabilityRequest{}, err
	}
	destination := source
	if target != nil {
		destination, err = target.Location(ctx)
		if err != nil {
			return capabilityRequest{}, err
		}
	}
	if !destination.Writable || destination.Sealed {
		return capabilityRequest{}, failure("controller.identity", "dependency installation requires the writable pending bundle capability")
	}
	packages := []prerequisites.NativePackage{}
	if definition.Native != nil {
		if prerequisites.ValidateNativePlan(*definition.Native) != nil || definition.Native.Platform != platform || definition.Native.Requirements != definition.NativeRequirements {
			return capabilityRequest{}, failure("controller.identity", "the frozen native dependency plan is invalid or targets another platform")
		}
		plan, err := prerequisites.CanonicalNativePlan(*definition.Native)
		if err != nil {
			return capabilityRequest{}, failure("controller.identity", "the frozen native dependency plan could not be copied")
		}
		definition.Native = &plan
		packages = slices.Clone(plan.Packages)
	}
	request := capabilityRequest{
		Version: requestVersion, Operation: operation, Identity: definition.CatalogDigest,
		Platform: platform, Bundle: location(source), PublicationBundle: location(destination),
		Packages: packages, Native: definition.Native, Tools: slices.Clone(definition.Tools), Egress: egress, Preparation: preparation,
	}
	request.Egress.NoProxy = slices.Clone(egress.NoProxy)
	for index := range request.Tools {
		request.Tools[index].Files = slices.Clone(request.Tools[index].Files)
	}
	if request.Tools == nil {
		request.Tools = []prerequisites.ToolDefinition{}
	}
	// One acquisition deadline per native package, in request order, then
	// one per tool; native staging bounds the packages together.
	request.Acquisition = make([]toolAcquisition, 0, len(request.Packages)+len(request.Tools))
	for _, item := range request.Packages {
		request.Acquisition = append(request.Acquisition, toolAcquisition{Source: item.Source.ID, Seconds: int64(acquisitionDeadline(item.Source.Bytes).Seconds())})
	}
	for _, tool := range request.Tools {
		request.Acquisition = append(request.Acquisition, toolAcquisition{Source: tool.Source.ID, Seconds: int64(acquisitionDeadline(tool.Source.Bytes).Seconds())})
	}
	request.NativeStaging = int64(nativeStaging(request.Packages).Seconds())
	if !validSHA(request.Identity) || preparation != nil && !validPreparation(*preparation, request) {
		return capabilityRequest{}, failure("controller.identity", "the frozen Ansible request or recovery proof is invalid")
	}
	return request, nil
}

func actionResult(outcome string, intentRecorded bool) prerequisites.ActionResult {
	evidence, _ := json.Marshal(map[string]bool{"intentRecorded": intentRecorded, "postcondition": false})
	return prerequisites.ActionResult{Outcome: outcome, Evidence: evidence}
}

// failure keeps its code in every scope. Setup and a context's controller
// stage share this adapter, so the scope that met it names what settles it.
func failure(code, message string) error {
	return &prerequisites.ScopedFailure{Code: code, Message: message, Correction: "Preserve the controller state and restore the qualified controller execution environment"}
}
