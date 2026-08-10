package cli

import (
	"fmt"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/host/shellquote"
)

type runSelection struct {
	stage    string
	through  string
	clusters string
	machines string
}

func (s runSelection) narrowFlag() string {
	if strings.TrimSpace(s.machines) != "" {
		return "--machines"
	}
	return "--clusters"
}

type invocationVerb string

const (
	invocationApply          invocationVerb = authorizeVerbApply
	invocationDestroy        invocationVerb = authorizeVerbDestroy
	invocationReplaceArbiter invocationVerb = authorizeVerbReplaceArbiter
)

type invocationFlags struct {
	mode                      workflow.ApplyMode
	selection                 runSelection
	reclaimDevices            string
	recoverCephOwnership      string
	purgeHistory              bool
	authorizations            []string
	dryRun                    bool
	output                    string
	yes                       bool
	askBecomePass             bool
	trustOnFirstUse           bool
	verbose                   bool
	clusterInstallParallelism int
	clusterName               string
	newArbiterMachine         string
}

type resolvedInvocation struct {
	verb                  invocationVerb
	contextName           string
	sshIdentityFile       string
	sshUser               string
	sshAskSudoPassword    bool
	sshUserForProvisioned bool
	flags                 invocationFlags
}

type retryIntent struct {
	mode                   workflow.ApplyMode
	requiredAuthorizations []string
	excludedAuthorization  string
}

type retryCommand struct {
	args []string
}

func newResolvedInvocation(verb invocationVerb, contextName string, flags invocationFlags) (resolvedInvocation, error) {
	if verb != invocationApply && verb != invocationDestroy && verb != invocationReplaceArbiter {
		return resolvedInvocation{}, fmt.Errorf("unsupported mutating invocation verb %q", verb)
	}
	identityFile, err := resolveSSHIDFile()
	if err != nil {
		return resolvedInvocation{}, err
	}
	flags.authorizations = append([]string(nil), flags.authorizations...)
	contextIdentity := strings.TrimSpace(contextOverride)
	if contextIdentity == "" {
		contextIdentity = strings.TrimSpace(contextName)
	}
	return resolvedInvocation{
		verb:                  verb,
		contextName:           contextIdentity,
		sshIdentityFile:       identityFile,
		sshUser:               strings.TrimSpace(sshUserOverride),
		sshAskSudoPassword:    sshAskSudoPassword,
		sshUserForProvisioned: sshUserForProvisioned,
		flags:                 flags,
	}, nil
}

func (i resolvedInvocation) retry(intent retryIntent) (retryCommand, error) {
	next := i
	if next.verb != invocationApply && next.verb != invocationDestroy && next.verb != invocationReplaceArbiter {
		return retryCommand{}, fmt.Errorf("unsupported mutating invocation verb %q", next.verb)
	}
	if intent.mode != "" {
		if next.verb != invocationApply || !intent.mode.Valid() {
			return retryCommand{}, fmt.Errorf("invalid retry mode %q for %s", intent.mode, next.verb)
		}
		next.flags.mode = intent.mode
	}
	accepted := authorizationTokenNamesForVerb(string(next.verb))
	if name := intent.excludedAuthorization; name != "" {
		if name == authorizeAll || !slices.Contains(accepted, name) {
			return retryCommand{}, fmt.Errorf("authorization token %q cannot be excluded from a %s retry", name, next.verb)
		}
		if slices.Contains(intent.requiredAuthorizations, name) {
			return retryCommand{}, fmt.Errorf("authorization token %q cannot be both required and excluded", name)
		}
		if !slices.Contains(next.flags.authorizations, name) && !slices.Contains(next.flags.authorizations, authorizeAll) {
			return retryCommand{}, fmt.Errorf("authorization token %q is not present in the %s invocation", name, next.verb)
		}
		next.flags.authorizations = authorizationsWithout(next.flags.authorizations, accepted, name)
	}
	for _, name := range intent.requiredAuthorizations {
		if slices.Contains(next.flags.authorizations, authorizeAll) || slices.Contains(next.flags.authorizations, name) {
			continue
		}
		if !slices.Contains(authorizationTokenNamesForVerb(string(next.verb)), name) {
			return retryCommand{}, fmt.Errorf("authorization token %q is not accepted by %s", name, next.verb)
		}
		next.flags.authorizations = append(next.flags.authorizations, name)
	}
	return retryCommand{args: next.args()}, nil
}

func authorizationsWithout(names, accepted []string, excluded string) []string {
	var out []string
	for _, name := range names {
		if name == authorizeAll {
			for _, expanded := range accepted {
				if expanded != authorizeAll && expanded != excluded && !slices.Contains(out, expanded) {
					out = append(out, expanded)
				}
			}
			continue
		}
		if name != excluded && !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	return out
}

func destroyContextRetry(contextName string) (retryCommand, error) {
	contextName = strings.TrimSpace(contextName)
	if contextName == "" {
		return retryCommand{}, fmt.Errorf("cannot construct a destroy retry without a context")
	}
	return retryCommand{args: []string{"bootwright", "destroy", "--context", contextName}}, nil
}

func (i resolvedInvocation) destroyClustersRetry(clusters []string) (retryCommand, error) {
	if len(clusters) == 0 {
		return retryCommand{}, fmt.Errorf("cannot construct a destroy retry without clusters")
	}
	next := i
	next.verb = invocationDestroy
	next.flags.mode = ""
	next.flags.selection = runSelection{clusters: strings.Join(clusters, ",")}
	next.flags.reclaimDevices = ""
	next.flags.clusterInstallParallelism = 0
	next.flags.recoverCephOwnership = ""
	next.flags.purgeHistory = false
	next.flags.trustOnFirstUse = false
	next.flags.clusterName = ""
	next.flags.newArbiterMachine = ""
	next.flags.authorizations = authorizationsAcceptedByVerb(next.flags.authorizations, i.verb, invocationDestroy)
	return retryCommand{args: next.args()}, nil
}

func (i resolvedInvocation) destroyMachinesRetry(machines []string, requiredAuthorizations ...string) (retryCommand, error) {
	if len(machines) == 0 {
		return retryCommand{}, fmt.Errorf("cannot construct a destroy retry without machines")
	}
	next := i
	next.verb = invocationDestroy
	next.flags.mode = ""
	next.flags.selection = runSelection{machines: strings.Join(machines, ",")}
	next.flags.reclaimDevices = ""
	next.flags.clusterInstallParallelism = 0
	next.flags.recoverCephOwnership = ""
	next.flags.purgeHistory = false
	next.flags.trustOnFirstUse = false
	next.flags.clusterName = ""
	next.flags.newArbiterMachine = ""
	next.flags.authorizations = authorizationsAcceptedByVerb(next.flags.authorizations, i.verb, invocationDestroy)
	return next.retry(retryIntent{requiredAuthorizations: requiredAuthorizations})
}

func (i resolvedInvocation) applyClustersRetry(clusters []string, requiredAuthorizations ...string) (retryCommand, error) {
	if len(clusters) == 0 {
		return retryCommand{}, fmt.Errorf("cannot construct an apply retry without clusters")
	}
	next := i
	next.verb = invocationApply
	next.flags.mode = workflow.ApplyModeReconcile
	next.flags.selection = runSelection{clusters: strings.Join(clusters, ",")}
	next.flags.reclaimDevices = ""
	next.flags.recoverCephOwnership = ""
	next.flags.purgeHistory = false
	next.flags.clusterName = ""
	next.flags.newArbiterMachine = ""
	next.flags.authorizations = authorizationsAcceptedByVerb(next.flags.authorizations, i.verb, invocationApply)
	return next.retry(retryIntent{requiredAuthorizations: requiredAuthorizations})
}

func (i resolvedInvocation) regenerateClusterISORetry(cluster string) (retryCommand, error) {
	return i.clusterLifecycleRetry(invocationApply, cluster, converge.PhaseDeps, workflow.ApplyModeRebuild)
}

func (i resolvedInvocation) reconcileContainerClusterRetry(cluster string) (retryCommand, error) {
	return i.clusterLifecycleRetry(invocationApply, cluster, converge.ClustersScope.Name, workflow.ApplyModeReconcile)
}

func (i resolvedInvocation) destroyIncompleteClusterRetry(cluster string) (retryCommand, error) {
	return i.clusterLifecycleRetry(invocationDestroy, cluster, converge.ClustersScope.Name, "", authorizeProtected, authorizeDataLoss)
}

func (i resolvedInvocation) reapplyDestroyedClusterRetry(cluster string) (retryCommand, error) {
	return i.clusterLifecycleRetry(invocationApply, cluster, converge.ClustersScope.Name, workflow.ApplyModeReconcile, authorizeDataLoss)
}

func (i resolvedInvocation) rebuildInstalledClusterRetry(cluster string) (retryCommand, error) {
	return i.clusterLifecycleRetry(invocationApply, cluster, converge.ClustersScope.Name, workflow.ApplyModeRebuild, authorizeDataLoss)
}

func (i resolvedInvocation) destroySelectedMachineLayerRetry() (retryCommand, error) {
	return i.destroySelectedLayerRetry(converge.InfraScope.Name, authorizeProtected)
}

func (i resolvedInvocation) destroySelectedClusterLayerRetry() (retryCommand, error) {
	return i.destroySelectedLayerRetry(converge.ClustersScope.Name, authorizeProtected, authorizeDataLoss)
}

func (i resolvedInvocation) destroySelectedLayerRetry(stage string, requiredAuthorizations ...string) (retryCommand, error) {
	next := i
	next.verb = invocationDestroy
	next.flags.mode = ""
	next.flags.selection.stage = stage
	next.flags.selection.through = ""
	next.flags.reclaimDevices = ""
	next.flags.clusterInstallParallelism = 0
	next.flags.recoverCephOwnership = ""
	next.flags.purgeHistory = false
	next.flags.trustOnFirstUse = false
	next.flags.clusterName = ""
	next.flags.newArbiterMachine = ""
	next.flags.authorizations = authorizationsAcceptedByVerb(next.flags.authorizations, i.verb, invocationDestroy)
	return next.retry(retryIntent{requiredAuthorizations: requiredAuthorizations})
}

func (i resolvedInvocation) clusterLifecycleRetry(verb invocationVerb, cluster, stage string, mode workflow.ApplyMode, requiredAuthorizations ...string) (retryCommand, error) {
	cluster = strings.TrimSpace(cluster)
	if cluster == "" {
		return retryCommand{}, fmt.Errorf("cannot construct a cluster lifecycle retry without a cluster")
	}
	next := i
	next.verb = verb
	next.flags.mode = mode
	next.flags.selection = runSelection{stage: stage, clusters: cluster}
	next.flags.reclaimDevices = ""
	if verb != invocationApply {
		next.flags.clusterInstallParallelism = 0
	}
	next.flags.recoverCephOwnership = ""
	next.flags.purgeHistory = false
	next.flags.clusterName = ""
	next.flags.newArbiterMachine = ""
	next.flags.authorizations = authorizationsAcceptedByVerb(next.flags.authorizations, i.verb, verb)
	return next.retry(retryIntent{requiredAuthorizations: requiredAuthorizations})
}

func (i resolvedInvocation) replaceArbiterRetry(cluster string) (retryCommand, error) {
	cluster = strings.TrimSpace(cluster)
	if cluster == "" {
		return retryCommand{}, fmt.Errorf("cannot construct a replace-arbiter retry without a cluster")
	}
	next := i
	next.verb = invocationReplaceArbiter
	next.flags.mode = ""
	next.flags.selection = runSelection{}
	next.flags.reclaimDevices = ""
	next.flags.clusterInstallParallelism = 0
	next.flags.recoverCephOwnership = ""
	next.flags.purgeHistory = false
	next.flags.trustOnFirstUse = false
	next.flags.clusterName = cluster
	next.flags.newArbiterMachine = ""
	next.flags.authorizations = authorizationsAcceptedByVerb(next.flags.authorizations, i.verb, invocationReplaceArbiter)
	return retryCommand{args: next.args()}, nil
}

func authorizationsAcceptedByVerb(names []string, sourceVerb, targetVerb invocationVerb) []string {
	accepted := authorizationTokenNamesForVerb(string(targetVerb))
	var out []string
	for _, name := range names {
		if name == authorizeAll && sourceVerb != targetVerb {
			for _, expanded := range authorizationTokenNamesForVerb(string(sourceVerb)) {
				if expanded != authorizeAll && slices.Contains(accepted, expanded) && !slices.Contains(out, expanded) {
					out = append(out, expanded)
				}
			}
			continue
		}
		if slices.Contains(accepted, name) && !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	return out
}

func (i resolvedInvocation) args() []string {
	args := []string{"bootwright", string(i.verb)}
	if i.verb == invocationReplaceArbiter {
		args = []string{"bootwright", "storage-cluster", "replace-arbiter"}
		if value := strings.TrimSpace(i.flags.clusterName); value != "" {
			args = append(args, "--name", value)
		}
		if value := strings.TrimSpace(i.flags.newArbiterMachine); value != "" {
			args = append(args, "--new-arbiter-machine", value)
		}
	}
	if i.verb == invocationApply {
		args = append(args, "--mode", string(i.flags.mode))
		if i.flags.clusterInstallParallelism > 0 {
			args = append(args, "--cluster-install-parallelism", fmt.Sprintf("%d", i.flags.clusterInstallParallelism))
		}
	}
	if len(i.flags.authorizations) > 0 {
		args = append(args, "--authorize", strings.Join(i.flags.authorizations, ","))
	}
	if i.flags.yes {
		args = append(args, "--yes")
	}
	if value := strings.TrimSpace(i.flags.selection.stage); value != "" {
		args = append(args, "--stage", value)
	}
	if value := strings.TrimSpace(i.flags.selection.through); value != "" {
		args = append(args, "--through", value)
	}
	if strings.TrimSpace(i.flags.selection.clusters) != "" {
		args = append(args, "--clusters", strings.TrimSpace(i.flags.selection.clusters))
	}
	if strings.TrimSpace(i.flags.selection.machines) != "" {
		args = append(args, "--machines", strings.TrimSpace(i.flags.selection.machines))
	}
	if value := strings.TrimSpace(i.flags.reclaimDevices); value != "" {
		args = append(args, "--reclaim-devices", value)
	}
	if value := strings.TrimSpace(i.flags.recoverCephOwnership); value != "" {
		args = append(args, "--recover-ceph-ownership", value)
	}
	if i.flags.purgeHistory {
		args = append(args, "--purge-history")
	}
	if i.flags.dryRun {
		args = append(args, "--dry-run")
	}
	if i.flags.output != "" && i.flags.output != outputText {
		args = append(args, "--output", i.flags.output)
	}
	args = append(args, fmt.Sprintf("--ask-become-pass=%t", i.flags.askBecomePass))
	if i.verb == invocationApply {
		args = append(args, fmt.Sprintf("--trust-on-first-use=%t", i.flags.trustOnFirstUse))
	}
	if i.flags.verbose {
		args = append(args, "--verbose")
	}
	if i.contextName != "" {
		args = append(args, "--context", i.contextName)
	}
	if i.sshIdentityFile != "" {
		args = append(args, "--ssh-id-file", i.sshIdentityFile)
	}
	if i.sshUser != "" {
		args = append(args, "--ssh-user", i.sshUser)
	}
	if i.sshAskSudoPassword {
		args = append(args, "--ssh-ask-sudo-password")
	}
	if i.sshUserForProvisioned {
		args = append(args, "--ssh-user-for-provisioned")
	}
	return args
}

func (c retryCommand) String() string {
	return shellquote.QuoteWords(c.args)
}

func (c retryCommand) Args() []string {
	return append([]string(nil), c.args...)
}
