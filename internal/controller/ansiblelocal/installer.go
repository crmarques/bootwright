package ansiblelocal

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/crmarques/bootwright/ansible"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

type Installer struct {
	ExecutionGuard prerequisites.PythonExecutionGuard
}

func New(execution prerequisites.PythonExecutionGuard) Installer {
	return Installer{ExecutionGuard: execution}
}

const requestVersion = "controller-prerequisites-v3"

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

func (installer Installer) Prepare(ctx context.Context, area prerequisites.BundleArea, platform prerequisites.Platform, definition prerequisites.Definition, egress prerequisites.SetupEgress, publish func(context.Context, prerequisites.NativePreparation) error, progress func(prerequisites.ProgressEvent)) (prerequisites.ActionResult, error) {
	return installer.invoke(ctx, area, platform, definition, egress, publish, nil, progress)
}

func (installer Installer) Recover(ctx context.Context, area prerequisites.BundleArea, platform prerequisites.Platform, definition prerequisites.Definition, egress prerequisites.SetupEgress, preparation prerequisites.NativePreparation, progress func(prerequisites.ProgressEvent)) (prerequisites.ActionResult, error) {
	preparation.AddedSources = slices.Clone(preparation.AddedSources)
	return installer.invoke(ctx, area, platform, definition, egress, nil, &preparation, progress)
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
	if installation.Target == nil || installation.Publish == nil || installation.Release == nil {
		return result, failure("controller.setup", "the Ansible controller adapter is incomplete")
	}
	request, err := installer.request(ctx, installation.Execution, installation.Target, "setup", installation.Platform, installation.Definition, installation.Egress, nil)
	if err != nil {
		return result, err
	}
	return run(ctx, installation.Launch, request, installation.Release, installation.Publish, installation.Progress)
}

func (installer Installer) invoke(ctx context.Context, area prerequisites.BundleArea, platform prerequisites.Platform, definition prerequisites.Definition, egress prerequisites.SetupEgress, publish func(context.Context, prerequisites.NativePreparation) error, preparation *prerequisites.NativePreparation, progress func(prerequisites.ProgressEvent)) (prerequisites.ActionResult, error) {
	result := actionResult("failed", false)
	if preparation != nil {
		result = actionResult("unknown", true)
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if installer.ExecutionGuard == nil || publish == nil && preparation == nil {
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
	err = installer.ExecutionGuard.WithPython(ctx, area, definition.Execution, func(launch prerequisites.PythonLaunch, release func() error) error {
		var runErr error
		result, runErr = run(ctx, launch, request, release, publish, progress)
		return runErr
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
	for name, expected := range ansible.Assets() {
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
	if !validSHA(request.Identity) || preparation != nil && !validPreparation(*preparation, request) {
		return capabilityRequest{}, failure("controller.identity", "the frozen Ansible request or recovery proof is invalid")
	}
	return request, nil
}

func actionResult(outcome string, intentRecorded bool) prerequisites.ActionResult {
	evidence, _ := json.Marshal(map[string]bool{"intentRecorded": intentRecorded, "postcondition": false})
	return prerequisites.ActionResult{Outcome: outcome, Evidence: evidence}
}

func failure(code, message string) error {
	return diagnostics.NewFailureWithRemediation(code, message, "", "Preserve the exact dependency receipt and restore the qualified execution environment before retrying setup.")
}
