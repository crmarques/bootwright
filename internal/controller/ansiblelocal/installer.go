package ansiblelocal

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"

	"github.com/crmarques/bootwright/ansible"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

type Installer struct {
	Stager prerequisites.AutomationStager
	runner ansibleRunner
}

// ansibleRunner starts one frozen Ansible request from a staged automation
// tree with the output its caller retains. New always runs the process
// itself; a test replaces it to prove what each operation hands that Ansible.
type ansibleRunner func(context.Context, prerequisites.PythonLaunch, stagedTree, capabilityRequest, func(context.Context, []string) error, func(prerequisites.ProgressEvent), prerequisites.RunOutput) (prerequisites.ActionResult, error)

// stagedTree is the extracted automation a run executes from and the lock
// every adapter process inherits so no sweep removes the tree under it.
type stagedTree struct {
	Path string
	Lock *os.File
}

func New(stager prerequisites.AutomationStager) Installer {
	return Installer{Stager: stager, runner: run}
}

const requestVersion = "controller-prerequisites-v6"

// capabilityRequest is what the role acquires: target tools only. Host
// packages are the stage's own, through the host's dnf.
type capabilityRequest struct {
	Version           string                         `json:"version"`
	Identity          string                         `json:"identity"`
	PublicationBundle bundleLocation                 `json:"publicationBundle"`
	Tools             []prerequisites.ToolDefinition `json:"tools"`
	Acquisition       []toolAcquisition              `json:"acquisition"`
	Egress            prerequisites.SetupEgress      `json:"egress"`
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

// Clients publishes one context's selected target tools inside a runtime the
// caller already holds. The automation runs from this installation's own
// staged extraction, released once the run ends.
func (installer Installer) Clients(ctx context.Context, installation prerequisites.ClientInstallation) (prerequisites.ActionResult, error) {
	result := actionResult("failed", false)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if installer.runner == nil || installer.Stager == nil || installation.Target == nil || installation.Publish == nil || len(installation.Tools) == 0 {
		return result, failure("controller.setup", "the Ansible controller adapter is incomplete")
	}
	request, err := installer.request(ctx, installation.Target, installation.Identity, installation.Tools, installation.Egress)
	if err != nil {
		return result, err
	}
	tree, release, err := installer.stage(ctx)
	if err != nil {
		return result, err
	}
	defer release()
	return installer.runner(ctx, installation.Launch, tree, request, installation.Publish, installation.Progress, swallowing(installation.Output))
}

// stage extracts the embedded automation and verifies what it wrote: the
// stager reads the tree back against the embedded files, and the digest it
// reports must be this executable's.
func (installer Installer) stage(ctx context.Context) (stagedTree, func(), error) {
	staged, err := installer.Stager.Stage(ctx)
	if err != nil {
		return stagedTree{}, nil, err
	}
	release := staged.Release
	if release == nil {
		release = func() {}
	}
	lock, held := staged.Lock.(*os.File)
	if !held || lock == nil || staged.Digest != ansible.Digest() || !filepath.IsAbs(staged.Path) {
		release()
		return stagedTree{}, nil, failure("controller.identity", "the staged controller automation does not match this executable's automation")
	}
	return stagedTree{Path: staged.Path, Lock: lock}, release, nil
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

// request freezes the complete Ansible invocation. The target area is the
// only one publication may write, so its writability is what the request has
// to prove.
func (installer Installer) request(ctx context.Context, target prerequisites.BundleArea, identity string, tools []prerequisites.ToolDefinition, egress prerequisites.SetupEgress) (capabilityRequest, error) {
	if target == nil {
		return capabilityRequest{}, failure("controller.setup", "the Ansible controller adapter is incomplete")
	}
	destination, err := target.Location(ctx)
	if err != nil {
		return capabilityRequest{}, err
	}
	if !destination.Writable || destination.Sealed {
		return capabilityRequest{}, failure("controller.identity", "dependency installation requires the writable pending bundle capability")
	}
	request := capabilityRequest{
		Version: requestVersion, Identity: identity, PublicationBundle: location(destination),
		Tools: slices.Clone(tools), Egress: egress,
	}
	request.Egress.NoProxy = slices.Clone(egress.NoProxy)
	if request.Egress.NoProxy == nil {
		request.Egress.NoProxy = []string{}
	}
	request.Acquisition = make([]toolAcquisition, 0, len(request.Tools))
	for index, tool := range request.Tools {
		request.Tools[index].Files = slices.Clone(tool.Files)
		request.Acquisition = append(request.Acquisition, toolAcquisition{Source: tool.Source.ID, Seconds: int64(acquisitionDeadline(tool.Source.Bytes).Seconds())})
	}
	if !validSHA(request.Identity) {
		return capabilityRequest{}, failure("controller.identity", "the frozen Ansible request is invalid")
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
