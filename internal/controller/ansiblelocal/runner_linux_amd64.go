//go:build linux && amd64

package ansiblelocal

import (
	"context"
	"encoding/json"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"syscall"
	"time"

	"github.com/crmarques/bootwright/ansible"
	"github.com/crmarques/bootwright/internal/adapterprotocol"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// Bounded drains for the structured result channel. A completed run only has
// to flush what Ansible already wrote; a run that published its intent is
// given longer before its result is declared unproved.
const (
	completedResultDrain  = 5 * time.Second
	authorizedResultDrain = 60 * time.Second
)

// runTimeout is the base of one controller-stage tool publication, which adds
// each tool source's acquisition deadline up to clientStageCeiling.
const runTimeout = 10 * time.Minute

const clientStageCeiling = 2 * time.Hour

// runDeadline is how long one run may take. A closure past the ceiling is
// refused before Ansible starts, so the clamp never shortens an admitted run.
func runDeadline(request capabilityRequest) time.Duration {
	if len(request.Tools) == 0 {
		return runTimeout
	}
	return min(runTimeout+acquisitionTotal(request), clientStageCeiling)
}

func acquisitionTotal(request capabilityRequest) time.Duration {
	var total time.Duration
	for _, tool := range request.Tools {
		total += acquisitionDeadline(tool.Source.Bytes)
	}
	return total
}

func requireClientStageCeiling(request capabilityRequest) error {
	if len(request.Tools) == 0 || runTimeout+acquisitionTotal(request) <= clientStageCeiling {
		return nil
	}
	var bytes int64
	for _, tool := range request.Tools {
		bytes += tool.Source.Bytes
	}
	return diagnostics.NewFailureWithRemediation("controller.unsupported", "the selected target clients total "+strconv.FormatInt(bytes, 10)+" bytes, whose acquisition deadlines exceed the controller stage's 2-hour ceiling", "", "Select fewer target clients for this context.")
}

func run(ctx context.Context, launch prerequisites.PythonLaunch, tree stagedTree, request capabilityRequest, publish func(context.Context, []string) error, progress func(prerequisites.ProgressEvent), retain prerequisites.RunOutput) (prerequisites.ActionResult, error) {
	// Invocation state is small and must not survive a reboot, so it lives on
	// the runtime filesystem. Tool staging is large and must not, so it goes
	// to durable temporary storage instead.
	return runProcess(ctx, launch, tree, request, publish, progress, retain, processBoundary{owner: 0, jobParent: "/run", scratchParent: "/var/tmp", command: exec.Command})
}

type processBoundary struct {
	owner         int
	jobParent     string
	scratchParent string
	command       func(string, ...string) *exec.Cmd
	// Drain bounds are injectable so the retained-channel path can be proved
	// without waiting out the production grace periods.
	completedDrain  time.Duration
	authorizedDrain time.Duration
}

func runProcess(ctx context.Context, launch prerequisites.PythonLaunch, tree stagedTree, request capabilityRequest, publish func(context.Context, []string) error, progress func(prerequisites.ProgressEvent), retain prerequisites.RunOutput, boundary processBoundary) (prerequisites.ActionResult, error) {
	ctx, cancel := context.WithTimeout(ctx, runDeadline(request))
	defer cancel()
	// Each protocol phase names the work Ansible is about to do, so every tool
	// transfer is visible while it runs.
	report := func(detail string) {
		if progress != nil {
			progress(prerequisites.ProgressEvent{Status: "running", Detail: detail})
		}
	}
	if boundary.completedDrain <= 0 {
		boundary.completedDrain = completedResultDrain
	}
	if boundary.authorizedDrain <= 0 {
		boundary.authorizedDrain = authorizedResultDrain
	}
	result := actionResult("failed", false)
	if os.Geteuid() != boundary.owner || boundary.command == nil || !filepath.IsAbs(launch.Program) || !filepath.IsAbs(launch.Shim) || !filepath.IsAbs(tree.Path) || len(request.Tools) == 0 || len(request.Tools) > 128 {
		return result, failure("controller.install", "the authorized Ansible execution boundary is unavailable")
	}
	if err := requireClientStageCeiling(request); err != nil {
		return result, err
	}
	job, err := os.MkdirTemp(boundary.jobParent, "bootwright-controller-")
	if err != nil {
		return result, failure("controller.install", "private Ansible invocation storage is unavailable")
	}
	defer os.RemoveAll(job)
	scratch, err := os.MkdirTemp(boundary.scratchParent, "bootwright-scratch-")
	if err != nil {
		return result, failure("controller.install", "private Ansible staging storage is unavailable")
	}
	defer os.RemoveAll(scratch)
	if err := requireScratchCapacity(scratch, request); err != nil {
		return result, err
	}
	if err := writeInvocation(job, launch, request); err != nil {
		return result, err
	}
	// A run that retains its output is readable afterwards; one that does not
	// discards it rather than letting it reach the operator's terminal.
	var output io.Writer = io.Discard
	if retain != nil {
		output = retain
	}
	run := &protocolRun{request: request, publish: publish, report: report, result: result, completedDrain: boundary.completedDrain, authorizedDrain: boundary.authorizedDrain}
	// Cancellation and the deadline signal the supervisor, which ends its whole
	// tree; the adapter otherwise stops at its next acknowledgement, which fails
	// once this process's ends of both channels have closed.
	ending := adapterprotocol.Run(ctx, adapterprotocol.Invocation{
		Program: launch.Program, Arguments: launch.Arguments, Environment: launch.Environment,
		Automation: tree.Path, AutomationLock: tree.Lock, Playbook: "controller/setup.yml",
		Inventory: filepath.Join(job, "inventory.json"), Variables: filepath.Join(job, "request.json"),
		LocalTemp: filepath.Join(job, "local"), RemoteTemp: filepath.Join(job, "remote"), Scratch: scratch,
		Output: output, Admission: adapterprotocol.Controller, Shape: preparationShape, Command: boundary.command,
		Starting: func() error {
			if err := ctx.Err(); err != nil {
				return err
			}
			report("starting the private Ansible runtime")
			return nil
		},
	}, run)
	return run.outcome(ctx, ending)
}

func writeInvocation(job string, launch prerequisites.PythonLaunch, request capabilityRequest) error {
	inventory := map[string]any{"all": map[string]any{"children": map[string]any{"bootwright_controller": map[string]any{"hosts": map[string]any{"controller": map[string]any{"ansible_connection": "local", "ansible_python_interpreter": launch.Shim, "ansible_host": "localhost"}}}}}}
	variables := map[string]any{"bootwright_controller_request": request}
	// ansible-core reads an --extra-vars file as trusted templates, and publisher
	// metadata in this request is remote text, so every string is marked data.
	for name, encode := range map[string]func() ([]byte, error){
		"inventory.json": func() ([]byte, error) { return json.Marshal(inventory) },
		"request.json":   func() ([]byte, error) { return ansible.ExtraVariables(variables) },
	} {
		encoded, err := encode()
		if err != nil || len(encoded) > 4<<20 || os.WriteFile(filepath.Join(job, name), encoded, 0600) != nil {
			return failure("controller.install", "the frozen Ansible invocation could not be materialized")
		}
	}
	return nil
}

type protocolRun struct {
	request capabilityRequest
	publish func(context.Context, []string) error
	report  func(string)
	result  prerequisites.ActionResult

	completedDrain, authorizedDrain time.Duration

	loaded, prepared, completed bool
	// refused marks an internal refusal, which keeps the generic failure but
	// is still the last record the adapter may write.
	refused bool
	// prepared is the protocol's position and published the effect a record
	// read before the failed exit has. One read after it still moves the
	// position, so a later record is judged as if read first, but records and
	// authorizes nothing.
	published     bool
	continuations int
	// effectRequested marks a continue record judged for an acknowledgement
	// that lets the adapter install a tool. It is set before the write;
	// effectAuthorized marks that write delivered.
	effectRequested, effectAuthorized bool
}

// Spare reports that nothing a tool publication starts may outlive
// cancellation.
func (run *protocolRun) Spare() bool { return false }

// Drain is the grace a descendant holding the result channel gets: a prepared
// or cancelled run drains on the longer grace period.
func (run *protocolRun) Drain(canceled bool) time.Duration {
	if canceled || run.published {
		return run.authorizedDrain
	}
	return run.completedDrain
}

// Judge judges one record. A record read after the failed exit is judged as if
// it had been read first, but its adapter is gone, so nothing is published,
// authorized, reported or acknowledged for it. A valid one leaves
// that failure, except the named refusal, which replaces it.
func (run *protocolRun) Judge(ctx context.Context, record adapterprotocol.Record, moment adapterprotocol.Moment) adapterprotocol.Verdict {
	request, exited := run.request, moment.Exited
	valid := !run.completed && !run.refused && moment.Open
	verdict := adapterprotocol.Verdict{Acknowledge: record.Phase != "completed" && record.Phase != "refused"}
	switch record.Phase {
	case "loaded":
		valid = valid && !run.loaded
		run.loaded = valid
		if run.loaded && !exited {
			run.report("preparing the target clients")
		}
	case "prepared":
		var preparation toolPreparation
		valid = valid && run.loaded && !run.prepared && json.Unmarshal(record.Preparation, &preparation) == nil && run.publish != nil && validPreparation(preparation, request)
		if valid && !exited {
			verdict.Failure = run.publish(ctx, slices.Clone(preparation.AddedSources))
			run.published = verdict.Failure == nil
		}
		run.prepared = run.prepared || valid && (exited || run.published)
	case "continue":
		valid = valid && run.loaded && run.prepared && run.continuations < len(request.Tools)
		if valid {
			run.continuations++
		}
		if valid && !exited {
			tool := request.Tools[run.continuations-1]
			run.report("installing " + tool.Kind + " " + tool.Version + ", tool " + strconv.Itoa(run.continuations) + " of " + strconv.Itoa(len(request.Tools)))
		}
	case "completed":
		valid = valid && run.loaded && run.prepared && (record.Outcome == "changed" || record.Outcome == "unchanged") && run.continuations == len(request.Tools) && validEvidence(record.Evidence, request)
		run.completed = run.completed || valid
	case "refused":
		// The adapter names the one refusal with a remedy of its own
		// before it fails, for the tool it is installing.
		if record.Reason == "release-stamp" {
			valid = valid && run.loaded && run.prepared && run.continuations > 0 && request.Tools[run.continuations-1].Kind == "openshift-clients"
			if valid {
				verdict.Failure = prerequisites.UnreleasedClient(request.Tools[run.continuations-1])
			}
			break
		}
		// Any other refusal names its class and the source it was
		// acquiring. An internal refusal has no remedy of its own, so it
		// leaves the generic failure.
		if record.Reason == "internal" && record.Source == "" {
			valid = valid && run.loaded
			run.refused = run.refused || valid
			break
		}
		host, area, acquiring := run.refusedSource(record.Source)
		valid = valid && run.loaded && acquiring
		if valid {
			verdict.Failure, valid = prerequisites.AdapterRefusal(record.Reason, host, area, false)
		}
	default:
		valid = false
	}
	verdict.Valid = valid
	if valid && !exited && record.Phase == "continue" {
		run.effectRequested = true
	}
	return verdict
}

// Acknowledged records that a delivered continue acknowledgement lets the
// adapter install a tool.
func (run *protocolRun) Acknowledged(record adapterprotocol.Record) {
	if record.Phase == "continue" {
		run.effectAuthorized = true
	}
}

// refusedSource is the URL host of the tool being installed, which a
// classified refusal names, and the publication bundle it was being written
// into; any other source is not one this run is acquiring.
func (run *protocolRun) refusedSource(source string) (string, string, bool) {
	address, area := "", ""
	if source != "" && run.continuations > 0 && run.request.Tools[run.continuations-1].Source.ID == source {
		address, area = run.request.Tools[run.continuations-1].Source.URL, run.request.PublicationBundle.Path
	}
	parsed, err := url.Parse(address)
	if address == "" || err != nil || parsed.Hostname() == "" {
		return "", "", false
	}
	return parsed.Hostname(), area, true
}

func (run *protocolRun) outcome(ctx context.Context, ending adapterprotocol.Ending) (prerequisites.ActionResult, error) {
	var err error
	switch ending.Kind {
	case adapterprotocol.Aborted:
		return run.result, ending.Failure
	case adapterprotocol.ResultChannel:
		return run.result, failure("controller.install", "the Ansible result channel could not be opened")
	case adapterprotocol.AuthorizationChannel:
		return run.result, failure("controller.install", "the Ansible authorization channel could not be opened")
	case adapterprotocol.NotStarted:
		return run.result, failure("controller.install", "the qualified Ansible process could not start")
	case adapterprotocol.Canceled:
		if run.unauthorized(ending.Kind) {
			return actionResult("failed", true), ctx.Err()
		}
		return actionResult("unknown", run.published), ctx.Err()
	case adapterprotocol.Completed:
		return prerequisites.ActionResult{Outcome: ending.Outcome, Evidence: slices.Clone(ending.Evidence)}, nil
	case adapterprotocol.Named:
		err = ending.Failure
	case adapterprotocol.Retained:
		err = failure("controller.unknown", "Ansible descendants retained the result channel after completion")
	case adapterprotocol.RetainedOutput:
		err = failure("controller.unknown", "Ansible descendants retained its output after it exited")
	case adapterprotocol.FailedExit:
		err = failure("controller.install", "Ansible did not complete the authorized dependency operation")
	case adapterprotocol.Incomplete:
		err = failure("controller.unknown", "the Ansible structured result was incomplete")
	case adapterprotocol.Invalid:
		err = failure("controller.unknown", "the Ansible capability protocol was invalid")
	case adapterprotocol.Uncertain:
		err = failure("controller.unknown", "Ansible authorization delivery was uncertain")
	default:
		err = failure("controller.unknown", "the Ansible operation has no complete result")
	}
	// A run that failed before Go delivered any acknowledgement authorizing
	// an effect performed none: the adapter waits for that acknowledgement
	// before a tool installation, so the preparation it published records a
	// failure the next run replaces.
	if run.unauthorized(ending.Kind) {
		return actionResult("failed", true), err
	}
	if run.published {
		return actionResult("unknown", true), err
	}
	return run.result, err
}

// unauthorized is a run that published its preparation and was never
// delivered an acknowledgement of a record authorizing an effect. Only EPIPE
// proves such an acknowledgement undelivered; any other failed write ends the
// session Uncertain, which a cancellation reported first hides, so under
// either ending a requested one may have reached the adapter.
func (run *protocolRun) unauthorized(ending adapterprotocol.Kind) bool {
	uncertain := run.effectRequested && (ending == adapterprotocol.Uncertain || ending == adapterprotocol.Canceled)
	return run.published && !run.effectAuthorized && !uncertain
}

// requireScratchCapacity refuses before any effect when the staging filesystem
// cannot hold the approved payloads. A tool publication that runs out of
// space mid-apply is an unknown outcome, so this is checked up front.
func requireScratchCapacity(scratch string, request capabilityRequest) error {
	var staged int64
	for _, tool := range request.Tools {
		staged += tool.Source.Bytes
	}
	if staged == 0 {
		return nil
	}
	var statistics syscall.Statfs_t
	if err := syscall.Statfs(scratch, &statistics); err != nil {
		return failure("controller.install", "Ansible staging storage cannot be measured")
	}
	// Keep headroom for archive expansion.
	available := int64(statistics.Bavail) * statistics.Bsize
	if available < staged+staged/4+(64<<20) {
		return failure("controller.unsupported", "the staging filesystem has too little free space for the approved dependency payloads")
	}
	return nil
}
