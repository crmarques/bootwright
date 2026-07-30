package cli

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	cliout "github.com/crmarques/bootwright/internal/cli/output"
)

const (
	authorizeDataLoss             = "data-loss"
	authorizeProtected            = "protected"
	authorizeInstalledClusterNode = "installed-cluster-node"
	authorizeUnownedVMs           = "unowned-vms"
	authorizeUnownedNetworks      = "unowned-networks"
	authorizeUnreachableNodes     = "unreachable-nodes"
	authorizeUnreadableRecords    = "unreadable-records"
	authorizeSharedInfra          = "shared-infra"
	authorizeStaleInput           = "stale-input"
)

const (
	authorizeVerbApply   = "apply"
	authorizeVerbDestroy = "destroy"
)

type authorizationToken struct {
	name       string
	authorizes string
	inert      string
	verbs      []string
	elsewhere  string
}

var authorizationTokens = []authorizationToken{{
	name:       authorizeDataLoss,
	authorizes: "any disk wipe or Ceph OSD zap, on apply and on destroy",
	inert:      "this run plans no data-destroying action",
	verbs:      []string{authorizeVerbApply, authorizeVerbDestroy},
}, {
	name:       authorizeProtected,
	authorizes: "acting on state whose Environment sets spec.safety.destroyProtection or spec.safety.protectedKinds",
	inert:      "no selected object is protected by an Environment spec.safety rule",
	verbs:      []string{authorizeVerbDestroy},
	elsewhere:  "destroying protected state must cross the destroy boundary, so run `bootwright destroy --authorize protected` for the affected scope and then re-apply",
}, {
	name:       authorizeInstalledClusterNode,
	authorizes: "destroy --machines naming a node of an installed cluster",
	inert:      "no selected machine is a node of an installed cluster",
	verbs:      []string{authorizeVerbDestroy},
	elsewhere:  "apply never tears a node out of an installed cluster, so it carries no such refusal",
}, {
	name:       authorizeUnownedVMs,
	authorizes: "tearing down libvirt/KubeVirt/vSphere VMs that match the Bootwright naming but carry no ownership marker",
	inert:      "this run tears down no machine VMs (their ownership refusals live in the infra stage)",
	verbs:      []string{authorizeVerbDestroy},
	elsewhere:  "apply never adopts an unowned VM in any mode, so remove it with `bootwright destroy --authorize unowned-vms` first",
}, {
	name:       authorizeUnownedNetworks,
	authorizes: "removing an unowned libvirt network or KubeVirt DataVolume, which may still be in use by another context",
	inert:      "this run removes no libvirt network or KubeVirt DataVolume (their ownership refusals live in the infra stage)",
	verbs:      []string{authorizeVerbDestroy},
	elsewhere:  "apply never removes a network or a DataVolume; only destroy does",
}, {
	name:       authorizeUnreachableNodes,
	authorizes: "leaving a cluster partially destroyed by skipping unreachable nodes",
	inert:      "this run contacts no node whose unreachability could be skipped",
	verbs:      []string{authorizeVerbDestroy},
	elsewhere:  "apply needs every selected node reachable and never skips one",
}, {
	name:       authorizeUnreadableRecords,
	authorizes: "proceeding when ownership records cannot be read, leaving their resources standing",
	inert:      "every ownership record of this context was read",
	verbs:      []string{authorizeVerbDestroy},
	elsewhere:  "apply reads records strictly so a corrupt one fails loud instead of leaving a resource standing, so repair the reported file",
}, {
	name:       authorizeSharedInfra,
	authorizes: "storage-consumer conflicts and shared infra components owned or referenced by another context",
	inert:      "no storage-consumer conflict and no cross-context infra-component block was found",
	verbs:      []string{authorizeVerbDestroy},
	elsewhere:  "a scoped apply that would degrade a shared service is resolved by widening --clusters, never by an authorization",
}, {
	name:       authorizeStaleInput,
	authorizes: "planning a teardown from input whose documents no longer decode or validate against this build, skipping exactly those documents",
	inert:      "every input document of this context decoded and validated",
	verbs:      []string{authorizeVerbDestroy},
	elsewhere:  "apply must never build from input it cannot fully read; re-render the input for the current schema and run `bootwright context update`",
}}

func authorizationTokenNames() []string {
	out := make([]string, 0, len(authorizationTokens))
	for _, token := range authorizationTokens {
		out = append(out, token.name)
	}
	return out
}

func authorizationTokenNamesForVerb(verb string) []string {
	var out []string
	for _, token := range authorizationTokens {
		if slices.Contains(token.verbs, verb) {
			out = append(out, token.name)
		}
	}
	return out
}

func authorizationTokenByName(name string) (authorizationToken, bool) {
	for _, token := range authorizationTokens {
		if token.name == name {
			return token, true
		}
	}
	return authorizationToken{}, false
}

func authorizationInertReason(name string) string {
	if token, ok := authorizationTokenByName(name); ok {
		return token.inert
	}
	return "this run has no gate for it"
}

func flagAuthorizeUsage(verb string) string {
	accepted := authorizationTokenNamesForVerb(verb)
	lines := make([]string, 0, len(accepted))
	for _, name := range accepted {
		token, _ := authorizationTokenByName(name)
		lines = append(lines, token.name+" = "+token.authorizes)
	}
	return "authorize a named risk; repeatable and comma-separated. Each token unblocks exactly one refusal and nothing else; --yes never authorizes any of them. Tokens " + verb + " accepts: " + strings.Join(lines, "; ")
}

func addAuthorizeFlag(cmd *cobra.Command, p *[]string, verb string) {
	cmd.Flags().StringSliceVar(p, "authorize", nil, flagAuthorizeUsage(verb))
	registerFlagCompletion(cmd, "authorize", authorizationTokenNamesForVerb(verb))
}

type authorizations struct {
	given   map[string]bool
	applied map[string]bool
	order   []string
}

func parseAuthorizations(values []string, verb string) (*authorizations, error) {
	out := &authorizations{given: map[string]bool{}, applied: map[string]bool{}}
	for _, value := range values {
		name := strings.TrimSpace(value)
		if name == "" {
			return nil, fmt.Errorf("--authorize takes a non-empty token; tokens %s accepts: %s", verb, strings.Join(authorizationTokenNamesForVerb(verb), ", "))
		}
		token, known := authorizationTokenByName(name)
		if !known {
			return nil, fmt.Errorf("--authorize %q is not an authorization token; tokens %s accepts: %s", name, verb, strings.Join(authorizationTokenNamesForVerb(verb), ", "))
		}
		if !slices.Contains(token.verbs, verb) {
			return nil, fmt.Errorf("--authorize %s is not a risk %s can authorize — it gates %s only: %s. Tokens %s accepts: %s", name, verb, strings.Join(token.verbs, "/"), token.elsewhere, verb, strings.Join(authorizationTokenNamesForVerb(verb), ", "))
		}
		if out.given[name] {
			continue
		}
		out.given[name] = true
		out.order = append(out.order, name)
	}
	return out, nil
}

func (a *authorizations) has(name string) bool {
	return a != nil && a.given[name]
}

func (a *authorizations) note(name string) {
	if a != nil {
		a.applied[name] = true
	}
}

func (a *authorizations) allows(name string) bool {
	a.note(name)
	return a.has(name)
}

func (a *authorizations) all() []string {
	if a == nil {
		return nil
	}
	return a.order
}

func (a *authorizations) unused() []string {
	if a == nil {
		return nil
	}
	var out []string
	for _, name := range a.order {
		if !a.applied[name] {
			out = append(out, name)
		}
	}
	return out
}

func warnUnusedAuthorizations(stdout io.Writer, a *authorizations, dryRun bool) {
	if dryRun {
		for _, name := range a.all() {
			cliout.NewContinuation(stdout).Warning("dry-run", "--authorize "+name+" is not consumed by a dry-run; an authorization applies only to a real run")
		}
		return
	}
	for _, name := range a.unused() {
		cliout.NewContinuation(stdout).Warning("authorize", "--authorize "+name+" had no effect: "+authorizationInertReason(name))
	}
}
