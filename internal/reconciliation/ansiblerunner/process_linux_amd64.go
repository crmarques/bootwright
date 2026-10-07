//go:build linux && amd64

package ansiblerunner

import (
	"context"
	"encoding/json"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/crmarques/bootwright/ansible"
	"github.com/crmarques/bootwright/internal/adapterprotocol"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

const (
	// invocationTimeout bounds a run whose request states no deadline of its
	// own: every block but the ones whose requests freeze the waits they
	// perform, which derive a deadline from those budgets instead.
	invocationTimeout = 2 * time.Hour
	resultDrain       = 5 * time.Second
	maxVariableBytes  = 4 << 20
)

// Runner performs one authorized lifecycle operation through the fixed entrypoint
// inside the controller's private Ansible runtime.
type Runner struct {
	jobParent     string
	scratchParent string
	// owner is the only identity whose run directories a sweep considers:
	// root, which every lifecycle adapter runs as.
	owner uint32
	// treeEntries bounds the entries a sweep reads from one run directory
	// tree; zero is maxRunTreeEntries.
	treeEntries int
	command     func(string, ...string) *exec.Cmd
	drain       time.Duration
	playbooks   map[string]string
}

// New binds the entrypoints composition authorizes, keyed by implementation
// identity and operation. A request naming anything else refuses.
func New(playbooks map[string]string) Runner {
	// Invocation state is small and must not survive a reboot; staging is
	// larger and must not either, so both live outside the context store.
	return Runner{jobParent: "/run", scratchParent: "/var/tmp", owner: 0, command: exec.Command, playbooks: maps.Clone(playbooks)}
}

func (r Runner) Run(ctx context.Context, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
	playbook, ok := r.playbookFor(request)
	if !ok {
		return lifecycle.RunResult{}, failure("lifecycle.state", "the lifecycle adapter operation is not recognized", "")
	}
	if r.command == nil || !filepath.IsAbs(request.Launch.Loader) || request.Bundle.Path == "" {
		return lifecycle.RunResult{}, failure("lifecycle.state", "the authorized adapter execution boundary is unavailable", setupRemediation)
	}
	if err := ctx.Err(); err != nil {
		return lifecycle.RunResult{}, err
	}
	if err := checkOutputs(request); err != nil {
		return lifecycle.RunResult{}, err
	}
	if err := checkMaterials(request); err != nil {
		return lifecycle.RunResult{}, err
	}
	if err := verifyAutomation(ctx, request); err != nil {
		return lifecycle.RunResult{}, err
	}
	if err := checkIdentity(request); err != nil {
		return lifecycle.RunResult{}, err
	}
	// Nothing starts while an earlier adapter of this context still holds its
	// job, and what a dead one of any context left, with the material in it,
	// goes first.
	if err := r.sweep(request.Context); err != nil {
		return lifecycle.RunResult{}, err
	}
	job, err := os.MkdirTemp(r.jobParent, jobPrefix)
	if err != nil {
		return lifecycle.RunResult{}, failure("lifecycle.state", "private adapter invocation storage is unavailable", "")
	}
	if err := os.Chmod(job, 0700); err != nil {
		_ = os.RemoveAll(job)
		return lifecycle.RunResult{}, failure("lifecycle.state", "private adapter invocation storage is unsafe", "")
	}
	lock, holder, err := claim(job, request)
	if err != nil {
		// Nothing but its locks was written, and no adapter ever held them.
		_ = os.RemoveAll(job)
		return lifecycle.RunResult{}, err
	}
	// Operation-scoped material never outlives the adapter processes that
	// hold the job lock.
	scratch := ""
	defer func() { r.release(job, scratch, lock, holder) }()
	scratch, err = os.MkdirTemp(r.scratchParent, scratchPrefix+strings.TrimPrefix(filepath.Base(job), jobPrefix)+"-")
	if err != nil {
		return lifecycle.RunResult{}, failure("lifecycle.state", "private adapter staging storage is unavailable", "")
	}
	paths, err := r.materialize(job, request)
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	if err := writeSSHConfig(job, request, paths); err != nil {
		return lifecycle.RunResult{}, err
	}
	values, err := variables(request, paths)
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	outputs, err := prepareOutputs(job, request)
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	if outputs != nil {
		values[request.Variable+"_output"] = outputs
	}
	interpreter := filepath.Join(job, interpreterName)
	if err := os.WriteFile(interpreter, []byte(request.Launch.InterpreterScript()), 0700); err != nil {
		return lifecycle.RunResult{}, failure("lifecycle.state", "the pinned module interpreter could not be published", "")
	}
	if err := writeJSON(job, inventoryName, inventory(request.Placement, interpreter, paths)); err != nil {
		return lifecycle.RunResult{}, err
	}
	if err := writeVariables(job, values); err != nil {
		return lifecycle.RunResult{}, err
	}
	result, err := r.execute(ctx, job, scratch, lock, playbook, request)
	if err != nil || len(request.Outputs) == 0 {
		return result, err
	}
	// What a completed run left is read before the deferred release removes
	// the job, and with it every output.
	produced, err := r.readOutputs(job, request.Outputs, request.OutputRemediation)
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	result.Produced = produced
	return result, nil
}

// The entries the runner writes into a job beside its lock, its record and its
// outputs directory, and the longest name one Linux path segment takes.
const (
	interpreterName = "interpreter"
	inventoryName   = "inventory.json"
	requestName     = "request.json"
	localTemp       = "local"
	remoteTemp      = "remote"
	sshConfigName   = "ssh_config"
	maxMaterialName = 255
	// cryptoPolicyConfig is the host crypto-policy backend the SSH arm's
	// generated client configuration includes, and the only one it reads.
	cryptoPolicyConfig = "/etc/crypto-policies/back-ends/openssh.config"
)

// jobEntries are every name the runner itself gives an entry of a job. A
// material file is written into the job too, so it may take none of them.
var jobEntries = []string{lockName, holderName, recordName, outputsDirectory, interpreterName, inventoryName, requestName, localTemp, remoteTemp, sshConfigName}

// checkMaterials refuses a material list before anything is written: each file
// is written once and its value cleared, so a name listed twice would be
// rewritten with the cleared bytes, and a variable bound twice would name only
// one of its files. A file is written at its name joined to the job, so the
// name must be one safe segment the runner does not write itself; and the
// adapter finds the files' paths and the material values in one variable map,
// where a value named as a file's variable would replace that file's path.
func checkMaterials(request lifecycle.RunRequest) error {
	names := make(map[string]bool, len(request.Materials))
	bound := make(map[string]bool, len(request.Materials))
	for _, file := range request.Materials {
		if !materialName(file.Name) {
			return failure("lifecycle.state", "a material file is named by an unsafe name, or by one the runner's job already uses", "")
		}
		if names[file.Name] || bound[file.Variable] {
			return failure("lifecycle.state", "a material file is named twice, or two material files bind one variable", "")
		}
		names[file.Name], bound[file.Variable] = true, true
	}
	for name := range request.MaterialValues {
		if bound[name] {
			return failure("lifecycle.state", "a material value is named as a material file's variable", "")
		}
	}
	return nil
}

// materialName admits one segment of lowercase letters, digits, dots,
// underscores and hyphens that does not begin with a dot: never a separator, a
// dot segment or a hidden name, and never a name in jobEntries.
func materialName(name string) bool {
	if name == "" || len(name) > maxMaterialName || name[0] == '.' || slices.Contains(jobEntries, name) {
		return false
	}
	for _, c := range []byte(name) {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func (r Runner) materialize(job string, request lifecycle.RunRequest) (map[string]string, error) {
	contents, err := materialBytes(request)
	if err != nil {
		return nil, err
	}
	paths := map[string]string{}
	for _, file := range request.Materials {
		value, ok := contents[file.Name]
		if !ok {
			continue
		}
		target := filepath.Join(job, file.Name)
		// Every bound part is private to this invocation and to root alone.
		if err := os.WriteFile(target, value, 0600); err != nil {
			clear(value)
			return nil, failure("lifecycle.state", "bound Secret material could not be prepared for the adapter", bindingRemediation)
		}
		clear(value)
		paths[file.Name] = target
	}
	return paths, nil
}

// writeSSHConfig gives the SSH arm a client configuration of its own, so ssh
// reads neither the system nor a personal configuration and keeps only the
// host crypto policy. A local placement runs no ssh and gets none.
func writeSSHConfig(job string, request lifecycle.RunRequest, paths map[string]string) error {
	if request.Placement.Local() {
		return nil
	}
	target := filepath.Join(job, sshConfigName)
	if err := os.WriteFile(target, []byte("Include "+cryptoPolicyConfig+"\n"), 0600); err != nil {
		return failure("lifecycle.state", "the frozen adapter invocation could not be materialized", "")
	}
	paths[sshConfigName] = target
	return nil
}

func writeJSON(job, name string, value any) error {
	encoded, err := json.Marshal(value)
	return writeEncoded(job, name, encoded, err)
}

// writeVariables writes the adapter's variables with every string marked as
// data, because ansible-core reads an --extra-vars file as trusted templates.
func writeVariables(job string, values map[string]any) error {
	encoded, err := ansible.ExtraVariables(values)
	return writeEncoded(job, requestName, encoded, err)
}

func writeEncoded(job, name string, encoded []byte, err error) error {
	if err != nil || len(encoded) > maxVariableBytes {
		return failure("lifecycle.state", "the frozen adapter invocation could not be materialized", "")
	}
	if err := os.WriteFile(filepath.Join(job, name), encoded, 0600); err != nil {
		return failure("lifecycle.state", "the frozen adapter invocation could not be materialized", "")
	}
	return nil
}

func (r Runner) execute(ctx context.Context, job, scratch string, lock *os.File, playbook string, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
	ctx, cancel := context.WithTimeout(ctx, runDeadline(request.Deadline))
	defer cancel()
	grace := r.drain
	if grace <= 0 {
		grace = resultDrain
	}
	judge := &lifecycleJudge{request: request, grace: grace}
	// The adapter inherits the job lock, and so does every process its
	// supervisor forks: the ansible-playbook child and each Ansible worker.
	// Its own streams are retained as they are produced, so a run that
	// completes is as readable afterwards as one that failed, and it dies with
	// this invocation: its supervisor ends the whole tree it owns on the
	// parent-death signal. No adapter effect outlives cancellation.
	ending := adapterprotocol.Run(ctx, adapterprotocol.Invocation{
		Loader: request.Launch.Loader, Arguments: request.Launch.Arguments, Environment: request.Launch.Environment,
		Automation: filepath.Join(request.Bundle.Path, "automation"), Playbook: playbook,
		Inventory: filepath.Join(job, inventoryName), Variables: filepath.Join(job, requestName),
		LocalTemp: filepath.Join(job, localTemp), RemoteTemp: filepath.Join(job, remoteTemp), Scratch: scratch,
		Lifecycle: true, Lock: lock, Output: request.Output, StopOnBreach: true,
		Admission: adapterprotocol.Lifecycle, Command: r.command,
	}, judge)
	return judge.result(ctx, ending)
}

// runDeadline is how long one run may take: the deadline its request states,
// which the capability derived from the waits that request froze, held to the
// ceiling every request shares; or the default for a request that states none.
// A fixed deadline cut short a block whose budgets alone outlast it.
func runDeadline(requested time.Duration) time.Duration {
	if requested <= 0 {
		return invocationTimeout
	}
	return min(requested, lifecycle.MaxDeadline)
}

// lifecycleJudge is the lifecycle runner's reading of the protocol: what the
// adapter reported so far.
type lifecycleJudge struct {
	request   lifecycle.RunRequest
	grace     time.Duration
	loaded    bool
	completed bool
}

// Judge judges one record and applies what a valid one reports. A record read
// after the failed exit is judged as if it had been read first, but reports
// nothing. A valid one leaves that failure, except the named refusal, which
// replaces it.
func (j *lifecycleJudge) Judge(ctx context.Context, record adapterprotocol.Record, moment adapterprotocol.Moment) adapterprotocol.Verdict {
	valid := !j.completed && moment.Open
	verdict := adapterprotocol.Verdict{}
	switch record.Phase {
	case "loaded":
		valid = valid && !j.loaded
		j.loaded = valid
		verdict.Acknowledge = true
	case "group":
		valid = valid && j.loaded
		if valid && !moment.Exited && j.request.Progress != nil {
			j.request.Progress(ctx, record.Group, record.Status)
		}
		// A record the log could not keep is the engine's log fault: its
		// callback latches it and cancels this run, which then ends.
		if valid && !moment.Exited && j.request.Log != nil {
			_ = j.request.Log(ctx, operationstore.LogRecord{Event: "group", Group: record.Group, Detail: record.Status})
		}
	case "completed":
		valid = valid && j.loaded
		j.completed = j.completed || valid
	case "refused":
		// The adapter names a refusal its caller remedies by name, then fails.
		// The caller's own failure for it replaces the adapter's.
		named := j.request.Refusals[record.Reason]
		valid = valid && j.loaded && named != nil
		if valid {
			verdict.Failure = named
		}
	default:
		valid = false
	}
	verdict.Valid = valid
	return verdict
}

func (j *lifecycleJudge) Spare() bool { return false }

// Acknowledged records nothing: a lifecycle acknowledgement authorizes no
// effect the runner tracks.
func (j *lifecycleJudge) Acknowledged(adapterprotocol.Record) {}

func (j *lifecycleJudge) Drain(bool) time.Duration { return j.grace }

func (j *lifecycleJudge) result(ctx context.Context, ending adapterprotocol.Ending) (lifecycle.RunResult, error) {
	remediation := j.request.OutputRemediation
	switch ending.Kind {
	case adapterprotocol.Completed:
		return lifecycle.RunResult{Outcome: ending.Outcome, Evidence: slices.Clone(ending.Evidence)}, nil
	case adapterprotocol.Canceled:
		return lifecycle.RunResult{}, ctx.Err()
	case adapterprotocol.Named:
		return lifecycle.RunResult{}, ending.Failure
	case adapterprotocol.ResultChannel:
		return lifecycle.RunResult{}, failure("lifecycle.state", "the adapter result channel could not be opened", "")
	case adapterprotocol.AuthorizationChannel:
		return lifecycle.RunResult{}, failure("lifecycle.state", "the adapter authorization channel could not be opened", "")
	case adapterprotocol.NotStarted:
		return lifecycle.RunResult{}, failure("lifecycle.state", "the qualified adapter process could not start", setupRemediation)
	case adapterprotocol.Retained:
		return lifecycle.RunResult{}, failure("lifecycle.unknown", "adapter descendants retained the result channel after completion", remediation)
	case adapterprotocol.RetainedOutput:
		return lifecycle.RunResult{}, failure("lifecycle.unknown", "adapter descendants retained the adapter's output after it exited", remediation)
	case adapterprotocol.FailedExit:
		return lifecycle.RunResult{}, failure("lifecycle.state", "the adapter operation did not complete", remediation)
	case adapterprotocol.Incomplete:
		return lifecycle.RunResult{}, failure("lifecycle.unknown", "the adapter structured result was incomplete", remediation)
	case adapterprotocol.Invalid:
		return lifecycle.RunResult{}, failure("lifecycle.unknown", "the adapter capability protocol was invalid", remediation)
	case adapterprotocol.Uncertain:
		return lifecycle.RunResult{}, failure("lifecycle.unknown", "adapter authorization delivery was uncertain", remediation)
	}
	return lifecycle.RunResult{}, failure("lifecycle.unknown", "the adapter operation has no complete result", remediation)
}

// verifyAutomation proves the approved bundle carries exactly the automation
// this executable's digest names before any of it runs. Documentation runs
// nothing and leaves the digest, so it is not compared.
func verifyAutomation(ctx context.Context, request lifecycle.RunRequest) error {
	if request.Area == nil {
		return failure("controller.identity", "the approved execution bundle is unavailable", setupRemediation)
	}
	for name, expected := range ansible.Automation() {
		actual, err := request.Area.Read(ctx, "automation/"+name, len(expected)+1)
		if err != nil || !slices.Equal(actual, expected) {
			return failure("controller.identity", "the embedded automation does not match the approved execution bundle", setupRemediation)
		}
	}
	return nil
}
