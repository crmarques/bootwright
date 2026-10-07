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
// to flush what Ansible already wrote; a run whose authorized native
// transaction was left running is given longer before its result is declared
// unproved.
const (
	completedResultDrain  = 5 * time.Second
	authorizedResultDrain = 60 * time.Second
)

// runTimeout bounds one controller Ansible run: setup, its recovery or the base
// of a controller-stage client installation, which adds its native staging and
// each tool source's acquisition deadline up to clientStageCeiling.
const runTimeout = 10 * time.Minute

const clientStageCeiling = 2 * time.Hour

// runDeadline is how long one run may take. A fixed deadline cut short a run
// whose sources alone outlast it; native staging is held within the ceiling,
// and a closure with tools past it is refused before Ansible starts, so the
// clamp never shortens an admitted run.
func runDeadline(request capabilityRequest) time.Duration {
	if len(request.Packages) == 0 && len(request.Tools) == 0 {
		return runTimeout
	}
	return min(runTimeout+nativeStaging(request.Packages)+acquisitionTotal(request), clientStageCeiling)
}

func acquisitionTotal(request capabilityRequest) time.Duration {
	var total time.Duration
	for _, tool := range request.Tools {
		total += acquisitionDeadline(tool.Source.Bytes)
	}
	return total
}

func requireClientStageCeiling(request capabilityRequest) error {
	if len(request.Tools) == 0 || runTimeout+nativeStaging(request.Packages)+acquisitionTotal(request) <= clientStageCeiling {
		return nil
	}
	var bytes int64
	for _, tool := range request.Tools {
		bytes += tool.Source.Bytes
	}
	return diagnostics.NewFailureWithRemediation("controller.unsupported", "the selected target clients total "+strconv.FormatInt(bytes, 10)+" bytes, whose acquisition deadlines exceed the controller stage's 2-hour ceiling", "", "Select fewer target clients for this context.")
}

func run(ctx context.Context, launch prerequisites.PythonLaunch, request capabilityRequest, release func() error, publish func(context.Context, prerequisites.NativePreparation) error, progress func(prerequisites.ProgressEvent), retain prerequisites.RunOutput) (prerequisites.ActionResult, error) {
	// Invocation state is small and must not survive a reboot, so it lives on
	// the runtime filesystem. Package staging is large and must not, so it goes
	// to durable temporary storage instead.
	return runProcess(ctx, launch, request, release, publish, progress, retain, processBoundary{owner: 0, jobParent: "/run", scratchParent: "/var/tmp", command: exec.Command})
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

func runProcess(ctx context.Context, launch prerequisites.PythonLaunch, request capabilityRequest, release func() error, publish func(context.Context, prerequisites.NativePreparation) error, progress func(prerequisites.ProgressEvent), retain prerequisites.RunOutput, boundary processBoundary) (prerequisites.ActionResult, error) {
	ctx, cancel := context.WithTimeout(ctx, runDeadline(request))
	defer cancel()
	// Each protocol phase names the work Ansible is about to do, so the native
	// transaction and every tool transfer are visible while they run.
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
	if os.Geteuid() != boundary.owner || boundary.command == nil || release == nil || !filepath.IsAbs(launch.Loader) || !filepath.IsAbs(launch.Directory) || len(request.Packages) > 512 || len(request.Tools) > 128 {
		return result, failure("controller.setup", "the authorized Ansible execution boundary is unavailable")
	}
	if err := requireClientStageCeiling(request); err != nil {
		return result, err
	}
	job, err := os.MkdirTemp(boundary.jobParent, "bootwright-controller-")
	if err != nil {
		return result, failure("controller.setup", "private Ansible invocation storage is unavailable")
	}
	defer os.RemoveAll(job)
	scratch, err := os.MkdirTemp(boundary.scratchParent, "bootwright-scratch-")
	if err != nil {
		return result, failure("controller.setup", "private Ansible staging storage is unavailable")
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
	run := &protocolRun{request: request, scratch: boundary.scratchParent, release: release, publish: publish, report: report, result: result, prepared: request.Operation == "recover", preparation: request.Preparation, completedDrain: boundary.completedDrain, authorizedDrain: boundary.authorizedDrain}
	run.published = run.prepared
	// No parent-death signal, unlike a lifecycle adapter: an authorized native
	// transaction must outlive this invocation. Cancellation and the deadline
	// signal the supervisor, which ends its whole tree, until a native
	// acknowledgement is delivered; after it the adapter stops at its next
	// acknowledgement, which fails once this process's ends of both channels
	// have closed.
	ending := adapterprotocol.Run(ctx, adapterprotocol.Invocation{
		Loader: launch.Loader, Arguments: launch.Arguments, Environment: launch.Environment,
		Automation: filepath.Join(request.Bundle.Path, "automation"), Playbook: "controller/setup.yml",
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
	interpreter := filepath.Join(job, "interpreter")
	if err := os.WriteFile(interpreter, []byte(launch.InterpreterScript()), 0700); err != nil {
		return failure("controller.setup", "the pinned module interpreter could not be published")
	}
	inventory := map[string]any{"all": map[string]any{"children": map[string]any{"bootwright_controller": map[string]any{"hosts": map[string]any{"controller": map[string]any{"ansible_connection": "local", "ansible_python_interpreter": interpreter, "ansible_host": "localhost"}}}}}}
	variables := map[string]any{"bootwright_controller_request": request}
	// ansible-core reads an --extra-vars file as trusted templates, and package
	// metadata in this request is remote text, so every string is marked data.
	for name, encode := range map[string]func() ([]byte, error){
		"inventory.json": func() ([]byte, error) { return json.Marshal(inventory) },
		"request.json":   func() ([]byte, error) { return ansible.ExtraVariables(variables) },
	} {
		encoded, err := encode()
		if err != nil || len(encoded) > 4<<20 || os.WriteFile(filepath.Join(job, name), encoded, 0600) != nil {
			return failure("controller.setup", "the frozen Ansible invocation could not be materialized")
		}
	}
	return nil
}

type protocolRun struct {
	request capabilityRequest
	// scratch is the directory a package source stages under, which a
	// storage refusal for it names.
	scratch string
	release func() error
	publish func(context.Context, prerequisites.NativePreparation) error
	report  func(string)
	result  prerequisites.ActionResult

	completedDrain, authorizedDrain time.Duration

	loaded, prepared, completed bool
	// refused marks an internal refusal, which keeps the generic failure but
	// is still the last record the adapter may write.
	refused bool
	// prepared and native are the protocol's position; published and
	// nativeAuthorized are the effects a record read before the failed exit
	// has. One read after it still moves the position, so a later record is
	// judged as if read first, but records and authorizes nothing.
	published                bool
	preparation              *prerequisites.NativePreparation
	continuations            int
	native, nativeAuthorized bool
}

// Spare reports that only an authorized native transaction may outlive
// cancellation. Durable intent alone is not installation, and a recovery run
// starts already prepared.
func (run *protocolRun) Spare() bool { return run.nativeAuthorized }

// Drain is the grace a descendant holding the result channel gets: a prepared
// or cancelled run may still have an authorized native transaction holding it,
// so it drains on the longer grace period.
func (run *protocolRun) Drain(canceled bool) time.Duration {
	if canceled || run.published {
		return run.authorizedDrain
	}
	return run.completedDrain
}

// Judge judges one record. A record read after the failed exit is judged as if
// it had been read first, but its adapter is gone, so nothing is released,
// published, authorized, reported or acknowledged for it. A valid one leaves
// that failure, except the named refusal, which replaces it.
func (run *protocolRun) Judge(ctx context.Context, record adapterprotocol.Record, moment adapterprotocol.Moment) adapterprotocol.Verdict {
	request, exited := run.request, moment.Exited
	valid := !run.completed && !run.refused && moment.Open
	verdict := adapterprotocol.Verdict{Acknowledge: record.Phase != "completed" && record.Phase != "refused"}
	switch record.Phase {
	case "loaded":
		valid = valid && !run.loaded
		if valid && exited {
			run.loaded = true
		} else if valid {
			verdict.Failure = run.release()
			run.loaded = verdict.Failure == nil
		}
		if run.loaded && !exited && request.Operation == "recover" {
			run.report("verifying the recorded native transaction")
		} else if run.loaded && !exited {
			run.report("reading the native package inventory")
		}
	case "prepared":
		var preparation prerequisites.NativePreparation
		valid = valid && run.loaded && !run.prepared && json.Unmarshal(record.Preparation, &preparation) == nil && run.publish != nil && validPreparation(preparation, request)
		if valid && !exited {
			verdict.Failure = run.publish(ctx, preparation)
			run.published = verdict.Failure == nil
		}
		if valid && (exited || run.published) {
			run.prepared = true
			preparation.AddedSources = slices.Clone(preparation.AddedSources)
			run.preparation = &preparation
		}
	case "native":
		valid = valid && run.loaded && run.prepared && !run.native && run.preparation != nil && nativeChanges(request) > 0
		run.native = run.native || valid
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
		valid = valid && run.loaded && run.prepared && (record.Outcome == "changed" || record.Outcome == "unchanged" && !run.native) && run.continuations == len(request.Tools) && (request.Operation == "recover" || run.native == (run.preparation != nil && nativeChanges(request) > 0)) && validEvidence(record.Evidence, request, run.preparation, run.native)
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
		// Any other refusal names its class, and an acquisition the
		// source it was acquiring. An internal native refusal has no
		// remedy of its own, so it leaves the generic failure.
		host, area, acquiring := run.refusedSource(record.Source)
		valid = valid && run.loaded && acquiring
		if valid && record.Reason == "internal" && record.Source == "" {
			run.refused = true
		} else if valid {
			verdict.Failure, valid = prerequisites.AdapterRefusal(record.Reason, host, area, run.nativeAuthorized)
		}
	default:
		valid = false
	}
	verdict.Valid = valid
	return verdict
}

// Acknowledged records what a delivered acknowledgement authorizes: only a
// native record's, which lets the adapter start its transaction. One whose
// delivery failed authorizes nothing.
func (run *protocolRun) Acknowledged(record adapterprotocol.Record) {
	if record.Phase == "native" {
		run.nativeAuthorized = true
		run.report("installing " + countNoun(nativeChanges(run.request), "native package"))
	}
}

// refusedSource is the URL host of the source a classified refusal names and
// the directory that source was being written into: the run's scratch for a
// package, the publication bundle for a tool. Both are empty for a native
// refusal, which names none. An acquisition refusal names
// the native package being staged, between prepared and native, or the tool
// being installed; any other source is not one this run is acquiring.
func (run *protocolRun) refusedSource(source string) (string, string, bool) {
	if source == "" {
		return "", "", true
	}
	address, area := "", ""
	if run.prepared && !run.native && run.continuations == 0 {
		for _, item := range run.request.Packages {
			if item.Source.ID == source {
				address, area = item.Source.URL, run.scratch
			}
		}
	}
	if run.continuations > 0 && run.request.Tools[run.continuations-1].Source.ID == source {
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
		return run.result, failure("controller.setup", "the Ansible result channel could not be opened")
	case adapterprotocol.AuthorizationChannel:
		return run.result, failure("controller.setup", "the Ansible authorization channel could not be opened")
	case adapterprotocol.NotStarted:
		return run.result, failure("controller.setup", "the qualified Ansible process could not start")
	case adapterprotocol.Canceled:
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
		err = failure("controller.setup", "Ansible did not complete the authorized dependency operation")
	case adapterprotocol.Incomplete:
		err = failure("controller.unknown", "the Ansible structured result was incomplete")
	case adapterprotocol.Invalid:
		err = failure("controller.unknown", "the Ansible capability protocol was invalid")
	case adapterprotocol.Uncertain:
		err = failure("controller.unknown", "Ansible authorization delivery was uncertain")
	default:
		err = failure("controller.unknown", "the Ansible operation has no complete result")
	}
	// Setup's own run that failed before Go acknowledged a native record
	// authorized nothing: the adapter cannot start its transaction before
	// that acknowledgement, so the preparation it published records a
	// failure the next setup replaces.
	if run.published && !run.nativeAuthorized && ownSetup(run.request) {
		return actionResult("failed", true), err
	}
	if run.published {
		return actionResult("unknown", true), err
	}
	return run.result, err
}

// ownSetup is setup's own run: it publishes into the bundle it executes and
// carries no tools. A recovery is recover, and a client installation passes a
// separate area.
func ownSetup(request capabilityRequest) bool {
	return request.Operation == "setup" && len(request.Tools) == 0 && request.PublicationBundle == request.Bundle
}

func countNoun(count int, noun string) string {
	if count == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(count) + " " + noun + "s"
}

// requireScratchCapacity refuses before any effect when the staging filesystem
// cannot hold the approved payloads. A package transaction that runs out of
// space mid-apply is an unknown outcome, so this is checked up front.
func requireScratchCapacity(scratch string, request capabilityRequest) error {
	var staged int64
	for _, pkg := range request.Packages {
		staged += pkg.Source.Bytes
	}
	for _, tool := range request.Tools {
		staged += tool.Source.Bytes
	}
	if staged == 0 {
		return nil
	}
	var statistics syscall.Statfs_t
	if err := syscall.Statfs(scratch, &statistics); err != nil {
		return failure("controller.setup", "Ansible staging storage cannot be measured")
	}
	// Keep headroom for expansion and the package manager's own metadata.
	available := int64(statistics.Bavail) * statistics.Bsize
	if available < staged+staged/4+(64<<20) {
		return failure("controller.unsupported", "the staging filesystem has too little free space for the approved dependency payloads")
	}
	return nil
}
