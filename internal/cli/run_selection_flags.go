package cli

import (
	"fmt"
	"slices"
	"strings"

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
	invocationApply   invocationVerb = authorizeVerbApply
	invocationDestroy invocationVerb = authorizeVerbDestroy
)

type invocationFlags struct {
	mode                 workflow.ApplyMode
	selection            runSelection
	reclaimDevices       string
	recoverCephOwnership string
	purgeHistory         bool
	authorizations       []string
	dryRun               bool
	output               string
	yes                  bool
	askBecomePass        bool
	trustOnFirstUse      bool
	verbose              bool
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
}

type retryCommand struct {
	args []string
}

func newResolvedInvocation(verb invocationVerb, contextName string, flags invocationFlags) (resolvedInvocation, error) {
	if verb != invocationApply && verb != invocationDestroy {
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
	if next.verb != invocationApply && next.verb != invocationDestroy {
		return retryCommand{}, fmt.Errorf("unsupported mutating invocation verb %q", next.verb)
	}
	if intent.mode != "" {
		if next.verb != invocationApply || !intent.mode.Valid() {
			return retryCommand{}, fmt.Errorf("invalid retry mode %q for %s", intent.mode, next.verb)
		}
		next.flags.mode = intent.mode
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

func (i resolvedInvocation) args() []string {
	args := []string{"bootwright", string(i.verb)}
	if i.verb == invocationApply {
		args = append(args, "--mode", string(i.flags.mode))
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
