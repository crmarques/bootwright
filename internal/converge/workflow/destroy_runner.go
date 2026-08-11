package workflow

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/internal/converge/ansible"
	"github.com/crmarques/bootwright/internal/converge/remedy"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/roles"
)

func runOneDestroyTask(ctx context.Context, stdout io.Writer, stderr io.Writer, runsDir, runID string, opts RunOptions, task ApplyTask, runnerFactory ApplyTaskRunnerFactory) applyTaskResult {
	taskRoot := filepath.Join(runsDir, "history", runID, "tasks", task.Entry.ID)
	taskOpts := opts
	taskOpts.State = task.State
	taskOpts.RenderDir = filepath.Join(taskRoot, "rendered")
	taskOpts.Playbook = task.Playbook
	taskOpts.Limit = task.Limit
	taskOpts.ExtraVarPairs = append(append([]string(nil), opts.ExtraVarPairs...), task.ExtraVarPairs...)
	taskOpts.ArtifactsBaseName = ""
	taskOpts.ResolveInstaller = false
	taskOpts.Label = task.Entry.Label
	taskOpts.Forks = task.Forks
	taskOpts.ArtifactsRoot = filepath.Join(taskRoot, "artifacts")
	taskOpts.OutputLogPath = TaskLogPath(runsDir, runID, task.Entry)
	taskOpts.AcquireRunLease = false
	taskOpts.SkipNoHostsBeforeRender = true
	taskOpts.ClassifyUnreachable = task.Entry.Kind != DestroyTaskKindStorageCluster
	if runnerFactory == nil {
		runnerFactory = func(stdout io.Writer, stderr io.Writer) ansible.Runner {
			return ansible.CommandRunner{Stdout: stdout, Stderr: stderr}
		}
	}
	if task.Entry.Kind == DestroyTaskKindStorageCluster {
		expected := StorageDestroyExpectedNodes(task.State, task.Entry.ResourceKeys)
		if len(expected) > 0 {
			return runOneStorageDestroyTask(ctx, stdout, stderr, taskRoot, taskOpts, task, expected, runnerFactory)
		}
	}
	expectedHosts, expectedErr := expectedNonStorageDestroyHosts(taskOpts, task)
	if expectedErr != nil {
		return failedDestroyTaskResult(taskOpts, task, false, expectedErr)
	}
	result, err := Run(ctx, taskOpts, runnerFactory(stdout, stderr), nil)
	if err != nil {
		return failedDestroyTaskResult(taskOpts, task, result.Skipped, err)
	}
	if err := requireNonStorageDestroyCompletion(taskOpts, task, result.Skipped, expectedHosts); err != nil {
		return failedDestroyTaskResult(taskOpts, task, result.Skipped, err)
	}
	return applyTaskResult{id: task.Entry.ID, skipped: result.Skipped}
}

type PartialDestroyOutcomeError struct {
	TaskID       string
	Hosts        []string
	EvidencePath string
	Cause        error
}

func (e *PartialDestroyOutcomeError) Error() string {
	if len(e.Hosts) > 0 {
		return fmt.Sprintf("destroy task %s remains partial because unreachable host(s) %s were skipped; retaining convergence, install, ownership, access, and substrate evidence for an exact retry", e.TaskID, strings.Join(e.Hosts, ", "))
	}
	return fmt.Sprintf("cannot prove destroy task %s completed every selected host: %v (evidence: %s); retaining convergence, install, ownership, access, and substrate evidence for an exact retry", e.TaskID, e.Cause, e.EvidencePath)
}

func (e *PartialDestroyOutcomeError) Unwrap() error {
	return e.Cause
}

func (e *PartialDestroyOutcomeError) Remedy() remedy.Request {
	return remedy.Request{Action: remedy.ActionRetrySameInvocation}
}

func requireNonStorageDestroyCompletion(opts RunOptions, task ApplyTask, skipped bool, expectedHosts []string) error {
	if task.Entry.Kind == DestroyTaskKindStorageCluster {
		return nil
	}
	if skipped && len(expectedHosts) == 0 && len(task.Entry.ResourceKeys) == 0 {
		return nil
	}
	path := filepath.Join(opts.ArtifactsRoot, ansible.RunResultName)
	return RequireDestroyCompletionEvidence(path, task.Entry.ID, expectedHosts)
}

func RequireDestroyCompletionEvidence(path, taskID string, expectedHosts []string) error {
	records, err := ansible.ReadRunResultForHosts(path, expectedHosts)
	if err != nil {
		return &PartialDestroyOutcomeError{TaskID: taskID, EvidencePath: path, Cause: err}
	}
	hostSet := map[string]bool{}
	completed := map[string]bool{}
	for _, record := range records {
		if record.Completion {
			completed[record.Host] = true
		}
	}
	for _, record := range records {
		if record.Status == "unreachable" || (record.Status == "probe-unreachable" && !completed[record.Host]) {
			hostSet[record.Host] = true
		}
	}
	hosts := make([]string, 0, len(hostSet))
	for host := range hostSet {
		hosts = append(hosts, host)
	}
	sort.Strings(hosts)
	if len(hosts) > 0 {
		return &PartialDestroyOutcomeError{TaskID: taskID, Hosts: hosts, EvidencePath: path}
	}
	return nil
}

func expectedNonStorageDestroyHosts(opts RunOptions, task ApplyTask) ([]string, error) {
	if task.Entry.Kind == DestroyTaskKindStorageCluster {
		return nil, nil
	}
	records, err := loadOwnershipRecordsForRun(task.Playbook, opts.OwnershipDir, opts.ContextName)
	if err != nil {
		return nil, &PartialDestroyOutcomeError{TaskID: task.Entry.ID, Cause: err}
	}
	members := render.HostGroupMembersWithOwnershipRecords(task.State, records)
	hostSet := map[string]bool{}
	for _, hosts := range members {
		for _, host := range hosts {
			hostSet[host] = true
		}
	}
	completionLimit := strings.TrimSpace(task.CompletionHostLimit)
	if completionLimit == "" {
		return nil, &PartialDestroyOutcomeError{TaskID: task.Entry.ID, Cause: errors.New("selected destroy task has no exact completion play-host limit for completion proof")}
	}
	completionHosts, err := exactDestroyLimitHosts(completionLimit, members, hostSet)
	if err != nil {
		return nil, &PartialDestroyOutcomeError{TaskID: task.Entry.ID, Cause: fmt.Errorf("destroy completion play-host limit: %w", err)}
	}
	selected := completionHosts
	limit := strings.TrimSpace(task.Limit)
	if limit != "" {
		limitedHosts, err := exactDestroyLimitHosts(limit, members, hostSet)
		if err != nil {
			return nil, &PartialDestroyOutcomeError{TaskID: task.Entry.ID, Cause: fmt.Errorf("destroy inventory limit: %w", err)}
		}
		selected = map[string]bool{}
		for host := range completionHosts {
			if limitedHosts[host] {
				selected[host] = true
			}
		}
	}
	hosts := make([]string, 0, len(selected))
	for host := range selected {
		hosts = append(hosts, host)
	}
	sort.Strings(hosts)
	if len(hosts) == 0 && len(task.Entry.ResourceKeys) > 0 {
		return nil, &PartialDestroyOutcomeError{TaskID: task.Entry.ID, Cause: errors.New("selected destroy task resolved no exact play hosts for completion proof")}
	}
	return hosts, nil
}

func exactDestroyLimitHosts(limit string, members map[string][]string, hostSet map[string]bool) (map[string]bool, error) {
	selected := map[string]bool{}
	for _, token := range strings.Split(limit, ":") {
		token = strings.TrimSpace(token)
		if token == "" || strings.ContainsAny(token, "!,*&?[]") {
			return nil, fmt.Errorf("%q is not an exact host/group union", limit)
		}
		if hosts, ok := members[token]; ok {
			for _, host := range hosts {
				selected[host] = true
			}
			continue
		}
		if hostSet[token] {
			selected[token] = true
			continue
		}
		return nil, fmt.Errorf("identity %q has no exact selected host or group", token)
	}
	return selected, nil
}

func runOneStorageDestroyTask(ctx context.Context, stdout io.Writer, stderr io.Writer, taskRoot string, taskOpts RunOptions, task ApplyTask, expected map[string][]string, runnerFactory ApplyTaskRunnerFactory) applyTaskResult {
	expectedSeedHosts := StorageDestroyExpectedSeedHosts(task.State, task.Entry.ResourceKeys)
	recovered, err := RecoverStorageDestroyResults(taskOpts.OwnershipDir, taskOpts.ContextName, expected, expectedSeedHosts)
	if err != nil {
		return failedDestroyTaskResult(taskOpts, task, false, err)
	}
	results := map[string]StorageDestroyClusterResult{}
	for name, result := range recovered {
		results[name] = result
	}
	pending := pendingStorageDestroyNames(expected, recovered)
	skipped := false
	if len(pending) > 0 {
		pendingExpected := StorageDestroyExpectedNodes(task.State, pending)
		pendingOpts := taskOpts
		pendingOpts.ExtraVarPairs = replaceDestroyExtraVar(pendingOpts.ExtraVarPairs, DestroyStorageScopeExtraVar, strings.Join(pending, ","))
		runResult, runErr := Run(ctx, pendingOpts, runnerFactory(stdout, stderr), nil)
		skipped = runResult.Skipped
		if runErr != nil {
			return failedDestroyTaskResult(taskOpts, task, skipped, runErr)
		}
		fresh, validateErr := validateStorageDestroyTaskReport(pendingOpts, pendingExpected)
		if validateErr != nil {
			return failedDestroyTaskResult(taskOpts, task, skipped, validateErr)
		}
		for name, result := range fresh {
			results[name] = result
		}
	}
	allowSkipped := extraVarValue(taskOpts.ExtraVarPairs, storageDestroySkipUnreachableExtraVar) == "true"
	validated, err := ValidateStorageDestroyResults([]StorageDestroyResult{{SchemaVersion: 1, Clusters: storageDestroyClusters(results)}}, expected, allowSkipped)
	if err != nil {
		return failedDestroyTaskResult(taskOpts, task, skipped, err)
	}
	resultPath := filepath.Join(taskOpts.ArtifactsRoot, StorageDestroyResultFileName)
	if err := writeStorageDestroyResult(resultPath, validated); err != nil {
		return failedDestroyTaskResult(taskOpts, task, skipped, err)
	}
	manifest, err := PrepareStorageDestroyOwnershipRelease(taskOpts.OwnershipDir, taskOpts.ContextName, validated, expectedSeedHosts)
	if err != nil {
		return failedDestroyTaskResult(taskOpts, task, skipped, err)
	}
	if err := writeStorageDestroyResult(resultPath, validated); err != nil {
		return failedDestroyTaskResult(taskOpts, task, skipped, err)
	}
	if partial := partialStorageDestroyNames(validated); len(partial) > 0 {
		return failedDestroyTaskResult(taskOpts, task, false, fmt.Errorf(
			"storage teardown remains partial for %s; retaining ownership, access, and substrate for an exact retry",
			strings.Join(partial, ", "),
		))
	}
	if len(manifest.Clusters) == 0 {
		return applyTaskResult{id: task.Entry.ID}
	}
	manifestPath := filepath.Join(taskRoot, "storage-destroy-release.json")
	if err := WriteStorageDestroyReleaseManifest(manifestPath, manifest); err != nil {
		return failedDestroyTaskResult(taskOpts, task, skipped, err)
	}
	if err := finalizeStorageDestroyTask(ctx, stdout, stderr, taskRoot, taskOpts, validated, expectedSeedHosts, manifest, manifestPath, runnerFactory); err != nil {
		return failedDestroyTaskResult(taskOpts, task, skipped, err)
	}
	return applyTaskResult{id: task.Entry.ID}
}

func partialStorageDestroyNames(results map[string]StorageDestroyClusterResult) []string {
	var names []string
	for name, result := range results {
		if len(result.SkippedNodes()) > 0 {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func validateStorageDestroyTaskReport(opts RunOptions, expected map[string][]string) (map[string]StorageDestroyClusterResult, error) {
	path := filepath.Join(opts.ArtifactsRoot, StorageDestroyResultFileName)
	report, found, err := ReadStorageDestroyResult(path)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("storage teardown produced no completion attestation at %s", path)
	}
	allowSkipped := extraVarValue(opts.ExtraVarPairs, storageDestroySkipUnreachableExtraVar) == "true"
	return ValidateStorageDestroyResults([]StorageDestroyResult{report}, expected, allowSkipped)
}

func finalizeStorageDestroyTask(ctx context.Context, stdout io.Writer, stderr io.Writer, taskRoot string, taskOpts RunOptions, results map[string]StorageDestroyClusterResult, expectedSeedHosts map[string]string, manifest StorageDestroyReleaseManifest, manifestPath string, runnerFactory ApplyTaskRunnerFactory) error {
	releaseOpts := taskOpts
	releaseOpts.Playbook = roles.PlaybookTaskStorageClusterDestroyRelease
	releaseOpts.RenderDir = filepath.Join(taskRoot, "release-rendered")
	releaseOpts.ArtifactsRoot = filepath.Join(taskRoot, "release-artifacts")
	releaseOpts.OutputLogPath = filepath.Join(taskRoot, "storage-release.log")
	releaseOpts.Label = taskOpts.Label + " ownership release"
	releaseOpts.ExtraVarPairs = replaceDestroyExtraVar(releaseOpts.ExtraVarPairs, StorageDestroyReleaseManifestExtraVar, manifestPath)
	validationPath := filepath.Join(releaseOpts.ArtifactsRoot, StorageDestroyReleaseValidationFileName)
	releaseOpts.ExtraVarPairs = replaceDestroyExtraVar(releaseOpts.ExtraVarPairs, StorageDestroyReleaseValidationExtraVar, validationPath)
	releaseOpts.SkipNoHostsBeforeRender = false
	releaseResult, err := Run(ctx, releaseOpts, runnerFactory(stdout, stderr), nil)
	if err != nil {
		if _, validationErr := os.Stat(validationPath); errors.Is(validationErr, os.ErrNotExist) {
			return errors.Join(err, ResetStorageDestroyOwnershipProof(taskOpts.OwnershipDir, taskOpts.ContextName, results, expectedSeedHosts, manifest))
		}
		return err
	}
	if releaseResult.Skipped {
		return errors.Join(
			errors.New("validated storage ownership release matched no hosts"),
			ResetStorageDestroyOwnershipProof(taskOpts.OwnershipDir, taskOpts.ContextName, results, expectedSeedHosts, manifest),
		)
	}
	if info, err := os.Stat(validationPath); err != nil || !info.Mode().IsRegular() {
		resetErr := ResetStorageDestroyOwnershipProof(taskOpts.OwnershipDir, taskOpts.ContextName, results, expectedSeedHosts, manifest)
		return errors.Join(fmt.Errorf("storage ownership release produced no validation boundary at %s", validationPath), resetErr)
	}
	return MarkStorageDestroyOwnershipReleased(taskOpts.OwnershipDir, taskOpts.ContextName, results, expectedSeedHosts, manifest)
}

func failedDestroyTaskResult(opts RunOptions, task ApplyTask, skipped bool, err error) applyTaskResult {
	failure := conciseApplyTaskFailure(err)
	return applyTaskResult{
		id:      task.Entry.ID,
		skipped: skipped,
		failure: failure,
		err:     &destroyTaskFailureError{message: fmt.Sprintf("%s failed: %s (log: %s)", task.Entry.Label, failure, opts.OutputLogPath), cause: err},
	}
}

type destroyTaskFailureError struct {
	message string
	cause   error
}

func (e *destroyTaskFailureError) Error() string {
	return e.message
}

func (e *destroyTaskFailureError) Unwrap() error {
	return e.cause
}

func pendingStorageDestroyNames(expected map[string][]string, recovered map[string]StorageDestroyClusterResult) []string {
	var out []string
	for name := range expected {
		if _, found := recovered[name]; !found {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func storageDestroyClusters(results map[string]StorageDestroyClusterResult) []StorageDestroyClusterResult {
	names := sortedStorageDestroyMapKeys(results)
	out := make([]StorageDestroyClusterResult, 0, len(names))
	for _, name := range names {
		out = append(out, results[name])
	}
	return out
}

func replaceDestroyExtraVar(pairs []string, name, value string) []string {
	out := make([]string, 0, len(pairs)+1)
	for _, pair := range pairs {
		key, _, _ := strings.Cut(pair, "=")
		if key != name {
			out = append(out, pair)
		}
	}
	return append(out, name+"="+value)
}
