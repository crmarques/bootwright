package ansiblelocal

import (
	"context"
	"encoding/json"
	"slices"

	automation "github.com/crmarques/bootwright/ansible"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate"
)

type Installer struct {
	ExecutionGuard prerequisites.PythonExecutionGuard
}

func New(execution prerequisites.PythonExecutionGuard) Installer {
	return Installer{ExecutionGuard: execution}
}

type capabilityRequest struct {
	Version     string                            `json:"version"`
	Operation   string                            `json:"operation"`
	Identity    string                            `json:"identity"`
	Platform    prerequisites.Platform            `json:"platform"`
	Bundle      bundleLocation                    `json:"bundle"`
	Packages    []prerequisites.NativePackage     `json:"packages"`
	Native      *prerequisites.NativeResolvedPlan `json:"native"`
	Tools       []prerequisites.ToolDefinition    `json:"tools"`
	Egress      prerequisites.SetupEgress         `json:"egress"`
	Preparation *prerequisites.NativePreparation  `json:"preparation,omitempty"`
}

type bundleLocation struct {
	Path     string `json:"path"`
	Device   uint64 `json:"device"`
	Inode    uint64 `json:"inode"`
	Writable bool   `json:"writable"`
	Sealed   bool   `json:"sealed"`
}

func (installer Installer) Prepare(ctx context.Context, area prerequisites.BundleArea, platform prerequisites.Platform, definition prerequisites.Definition, egress prerequisites.SetupEgress, publish func(context.Context, prerequisites.NativePreparation) error) (prerequisites.ActionResult, error) {
	return installer.invoke(ctx, area, platform, definition, egress, publish, nil)
}

func (installer Installer) Recover(ctx context.Context, area prerequisites.BundleArea, platform prerequisites.Platform, definition prerequisites.Definition, egress prerequisites.SetupEgress, preparation prerequisites.NativePreparation) (prerequisites.ActionResult, error) {
	preparation.AddedSources = slices.Clone(preparation.AddedSources)
	return installer.invoke(ctx, area, platform, definition, egress, nil, &preparation)
}

func (installer Installer) invoke(ctx context.Context, area prerequisites.BundleArea, platform prerequisites.Platform, definition prerequisites.Definition, egress prerequisites.SetupEgress, publish func(context.Context, prerequisites.NativePreparation) error, preparation *prerequisites.NativePreparation) (prerequisites.ActionResult, error) {
	result := actionResult("failed", false)
	if preparation != nil {
		result = actionResult("unknown", true)
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if installer.ExecutionGuard == nil || area == nil || publish == nil && preparation == nil {
		return result, failure("controller.setup", "the Ansible controller adapter is incomplete")
	}
	for name, expected := range automation.Assets() {
		actual, err := area.Read(ctx, "automation/"+name, len(expected)+1)
		if err != nil || !slices.Equal(actual, expected) {
			return result, failure("controller.identity", "the embedded controller automation does not match the approved execution bundle")
		}
	}
	location, err := area.Location(ctx)
	if err != nil {
		return result, err
	}
	if !location.Writable || location.Sealed {
		return result, failure("controller.identity", "dependency installation requires the writable pending bundle capability")
	}
	packages := []prerequisites.NativePackage{}
	if definition.Native != nil {
		if prerequisites.ValidateNativePlan(*definition.Native) != nil || definition.Native.Platform != platform || definition.Native.Requirements != definition.NativeRequirements {
			return result, failure("controller.identity", "the frozen native dependency plan is invalid or targets another platform")
		}
		plan, err := prerequisites.CanonicalNativePlan(*definition.Native)
		if err != nil {
			return result, failure("controller.identity", "the frozen native dependency plan could not be copied")
		}
		definition.Native = &plan
		packages = slices.Clone(plan.Packages)
	}
	operation := "setup"
	if preparation != nil {
		operation = "recover"
	}
	request := capabilityRequest{
		Version: "controller-prerequisites-v2", Operation: operation, Identity: definition.CatalogDigest,
		Platform: platform, Bundle: bundleLocation{location.Path, location.Device, location.Inode, location.Writable, location.Sealed},
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
		return result, failure("controller.identity", "the frozen Ansible request or recovery proof is invalid")
	}
	err = installer.ExecutionGuard.WithPython(ctx, area, definition.Execution, func(launch prerequisites.PythonLaunch, release func() error) error {
		var runErr error
		result, runErr = run(ctx, launch, request, release, publish)
		return runErr
	})
	return result, err
}

func actionResult(outcome string, intentRecorded bool) prerequisites.ActionResult {
	evidence, _ := json.Marshal(map[string]bool{"intentRecorded": intentRecorded, "postcondition": false})
	return prerequisites.ActionResult{Outcome: outcome, Evidence: evidence}
}

func failure(code, message string) error {
	return desiredstate.NewFailureWithRemediation(code, message, "", "Preserve the exact dependency receipt and restore the qualified execution environment before retrying setup.")
}
