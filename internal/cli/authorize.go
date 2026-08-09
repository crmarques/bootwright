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
	authorizeAll                  = "all"
	authorizeDataLoss             = "data-loss"
	authorizeProtected            = "protected"
	authorizeInstalledClusterNode = "installed-cluster-node"
	authorizeUnownedVMs           = "unowned-vms"
	authorizeUnownedNetworks      = "unowned-networks"
	authorizeUnownedDevices       = "unowned-devices"
	authorizeForeignDaemons       = "foreign-daemons"
	authorizeUnreachableNodes     = "unreachable-nodes"
	authorizeUnreadableRecords    = "unreadable-records"
	authorizeSharedInfra          = "shared-infra"
	authorizeStaleInput           = "stale-input"
	authorizeSameSiteArbiter      = "same-site-arbiter"
	authorizeDegradedQuorum       = "degraded-quorum"
)

const (
	authorizeVerbApply          = "apply"
	authorizeVerbDestroy        = "destroy"
	authorizeVerbReplaceArbiter = "replace-arbiter"
)

type authorizationToken struct {
	name       string
	gloss      string
	authorizes string
	inert      string
	verbs      []string
	elsewhere  string
}

var authorizationTokens = []authorizationToken{{
	name:       authorizeAll,
	gloss:      "every other token this verb accepts, in one word; it answers no confirmation prompt",
	authorizes: "every other token this verb accepts, in one word; it clears no refusal that has no token of its own and never answers a confirmation prompt",
	inert:      "this run reached no gate any token of this verb could clear",
	verbs:      []string{authorizeVerbApply, authorizeVerbDestroy, authorizeVerbReplaceArbiter},
}, {
	name:       authorizeDataLoss,
	gloss:      "any disk wipe or Ceph OSD zap, on apply and on destroy",
	authorizes: "any disk wipe or Ceph OSD zap, on apply and on destroy",
	inert:      "this run plans no data-destroying action",
	verbs:      []string{authorizeVerbApply, authorizeVerbDestroy},
	elsewhere:  "replace-arbiter moves a mon-only tiebreaker and wipes no disk, so it carries no data-loss refusal",
}, {
	name:       authorizeProtected,
	gloss:      "state an Environment protects with a spec.safety rule",
	authorizes: "acting on state whose Environment sets spec.safety.destroyProtection or spec.safety.protectedKinds",
	inert:      "no selected object is protected by an Environment spec.safety rule",
	verbs:      []string{authorizeVerbDestroy},
	elsewhere:  "destroying protected state must cross the destroy boundary, so use the destroy verb for the exact affected scope with --authorize protected and then re-apply; the one apply-side exception is apply --reclaim-devices, whose protected-state wipe is authorized by --authorize data-loss",
}, {
	name:       authorizeInstalledClusterNode,
	gloss:      "a --machines teardown naming a node of an installed cluster",
	authorizes: "destroy --machines naming a node of an installed cluster",
	inert:      "no selected machine is a node of an installed cluster",
	verbs:      []string{authorizeVerbDestroy},
	elsewhere:  "apply never tears a node out of an installed cluster, so it carries no such refusal",
}, {
	name:       authorizeUnownedVMs,
	gloss:      "VMs matching the Bootwright naming that carry no ownership marker",
	authorizes: "tearing down libvirt/KubeVirt/vSphere VMs that match the Bootwright naming but carry no ownership marker",
	inert:      "this run tears down no machine VMs (their ownership refusals live in the infra stage)",
	verbs:      []string{authorizeVerbDestroy},
	elsewhere:  "apply never adopts an unowned VM in any mode, so use destroy for the exact affected scope with --authorize unowned-vms first",
}, {
	name:       authorizeUnownedNetworks,
	gloss:      "an unowned libvirt network or KubeVirt DataVolume another context may still use",
	authorizes: "removing an unowned libvirt network or KubeVirt DataVolume, which may still be in use by another context",
	inert:      "this run removes no libvirt network or KubeVirt DataVolume (their ownership refusals live in the infra stage)",
	verbs:      []string{authorizeVerbDestroy},
	elsewhere:  "apply never removes a network or a DataVolume; only destroy does",
}, {
	name:       authorizeUnownedDevices,
	gloss:      "an OSD device holding data with no Bootwright OSD ownership record for this node",
	authorizes: "wiping a declared OSD device that carries data signatures or LVM/dm-crypt holders but no Bootwright OSD ownership record for this node — an orphan left by a destroyed or foreign Ceph install; on apply it needs --reclaim-devices, and it never relaxes the mounted, in-use, or unprobeable refusals",
	inert:      "no selected node refused a declared OSD device for want of a Bootwright OSD ownership record (on apply the gate runs only under --reclaim-devices)",
	verbs:      []string{authorizeVerbApply, authorizeVerbDestroy},
	elsewhere:  "replace-arbiter touches no OSD device, so it carries no device-ownership refusal",
}, {
	name:       authorizeForeignDaemons,
	gloss:      "another Ceph cluster's cephadm daemons and state on a node this apply enrolls",
	authorizes: "removing the cephadm daemons, systemd units and /var/lib/ceph state of another Ceph cluster from a storage node this apply enrolls — fsid-scoped, and it zaps no disk",
	inert:      "no selected node reached the foreign-cephadm gate (it runs where an apply converges a storage cluster)",
	verbs:      []string{authorizeVerbApply},
	elsewhere:  "destroy removes the fsid of the cluster it tears down and deliberately leaves a co-resident one standing, so a leftover is cleared by the apply that enrolls the node or by `cephadm rm-cluster --force --fsid <fsid>` on it",
}, {
	name:       authorizeUnreachableNodes,
	gloss:      "a node this run proved it could not contact; it is skipped, not cleaned up",
	authorizes: "acting on a node the run proves it could not contact — no route, unreachable network, host down, connection timed out or refused. On destroy it skips that node and leaves the cluster partially destroyed; on replace-arbiter it retires the replaced arbiter offline (`ceph orch host rm --offline --force`) with no host-local cleanup. A refusal that does not prove absence (a rejected identity, an address that does not resolve, an empty or unreadable diagnostic) is never skipped, because acting on a node that is in fact running leaves its Ceph daemons up and its OSD devices holding data",
	inert:      "this run contacts no node whose unreachability could be skipped",
	verbs:      []string{authorizeVerbDestroy, authorizeVerbReplaceArbiter},
	elsewhere:  "apply needs every selected node reachable and never skips one",
}, {
	name:       authorizeSameSiteArbiter,
	gloss:      "a tiebreaker sharing its failure domain with the data-site mons",
	authorizes: "promoting a mon that shares a failure domain with the data-site mons to stretch tiebreaker — Ceph's own `--yes-i-really-mean-it` path. It is the emergency fallback for a lost third site: an arbiter inside a data site cannot break a tie between the two, so losing that site drops two votes at once and the survivor is left without quorum",
	inert:      "the replacement arbiter shares its site with no non-tiebreaker mon",
	verbs:      []string{authorizeVerbReplaceArbiter},
	elsewhere:  "apply and destroy never move a stretch tiebreaker; only the storage-cluster replace-arbiter verb does",
}, {
	name:       authorizeDegradedQuorum,
	gloss:      "moving the tiebreaker while declared mons are outside quorum",
	authorizes: "moving the stretch tiebreaker while declared mons are outside quorum. `ceph mon set_new_tiebreaker` needs a quorum to commit, and swapping the arbiter during a site outage removes the vote holding the remaining quorum together",
	inert:      "every declared mon of the selected cluster is in quorum",
	verbs:      []string{authorizeVerbReplaceArbiter},
	elsewhere:  "apply and destroy never move a stretch tiebreaker; only the storage-cluster replace-arbiter verb does",
}, {
	name:       authorizeUnreadableRecords,
	gloss:      "ownership records that cannot be read, leaving their resources standing",
	authorizes: "proceeding when ownership records cannot be read, leaving their resources standing",
	inert:      "every ownership record of this context was read",
	verbs:      []string{authorizeVerbDestroy},
	elsewhere:  "apply reads records strictly so a corrupt one fails loud instead of leaving a resource standing, so repair the reported file",
}, {
	name:       authorizeSharedInfra,
	gloss:      "storage-consumer conflicts and infra another context owns or references",
	authorizes: "storage-consumer conflicts and shared infra components owned or referenced by another context",
	inert:      "no storage-consumer conflict and no cross-context infra-component block was found",
	verbs:      []string{authorizeVerbDestroy},
	elsewhere:  "a scoped apply that would degrade a shared service is resolved by widening --clusters, never by an authorization",
}, {
	name:       authorizeStaleInput,
	gloss:      "input documents that no longer decode or validate, skipping exactly those",
	authorizes: "planning a teardown from input whose documents no longer decode or validate against this build, skipping exactly those documents",
	inert:      "every input document of this context decoded and validated",
	verbs:      []string{authorizeVerbDestroy},
	elsewhere:  "apply must never build from input it cannot fully read; re-render the input for the current schema and run `bootwright context update`",
}}

func authorizationVerbs() []string {
	return []string{authorizeVerbApply, authorizeVerbDestroy, authorizeVerbReplaceArbiter}
}

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
	return "authorize a named risk; repeatable and comma-separated, and --yes authorizes none of them. Tokens " + verb + " accepts: " + strings.Join(authorizationTokenNamesForVerb(verb), ", ") + ". What each one unblocks is in the Authorizations section of --help"
}

func authorizationsHelpSection(verb string, approvable bool) string {
	accepted := authorizationTokenNamesForVerb(verb)
	width := 0
	for _, name := range accepted {
		if len(name) > width {
			width = len(name)
		}
	}
	scope := "  for the whole list."
	if approvable {
		scope = "  for the whole list, and --yes authorizes no named risk at all."
	}
	lines := []string{
		"Authorizations:",
		"  Each token unblocks exactly one refusal and nothing else; \"" + authorizeAll + "\" stands in",
		scope,
		"  Tokens " + verb + " accepts:",
		"",
	}
	for _, name := range accepted {
		token, _ := authorizationTokenByName(name)
		lines = append(lines, "    "+name+strings.Repeat(" ", width-len(name))+"  "+token.gloss)
	}
	return strings.Join(lines, "\n")
}

func longHelpWithAuthorizations(cmd *cobra.Command, verb string) string {
	section := authorizationsHelpSection(verb, cmd.Flags().Lookup("yes") != nil)
	base := strings.TrimRight(cmd.Long, "\n")
	if base == "" {
		base = strings.TrimRight(cmd.Short, "\n")
	}
	if base == "" {
		return section
	}
	return base + "\n\n" + section
}

func addAuthorizeFlag(cmd *cobra.Command, p *[]string, verb string) {
	cmd.Long = longHelpWithAuthorizations(cmd, verb)
	cmd.Flags().StringSliceVar(p, "authorize", nil, flagAuthorizeUsage(verb))
	registerFlagCompletion(cmd, "authorize", authorizationTokenNamesForVerb(verb))
}

type authorizations struct {
	verb     string
	given    map[string]bool
	applied  map[string]bool
	order    []string
	expanded []string
}

func parseAuthorizations(values []string, verb string) (*authorizations, error) {
	out := &authorizations{verb: verb, given: map[string]bool{}, applied: map[string]bool{}}
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
	if a == nil {
		return false
	}
	return a.given[name] || a.coveredByAll(name)
}

func (a *authorizations) coveredByAll(name string) bool {
	if a == nil || name == authorizeAll || a.given[name] || !a.given[authorizeAll] {
		return false
	}
	token, known := authorizationTokenByName(name)
	return known && slices.Contains(token.verbs, a.verb)
}

func (a *authorizations) note(name string) {
	if a == nil {
		return
	}
	a.applied[name] = true
	if !a.coveredByAll(name) {
		return
	}
	a.applied[authorizeAll] = true
	if !slices.Contains(a.expanded, name) {
		a.expanded = append(a.expanded, name)
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

func (a *authorizations) expandedByAll() []string {
	if a == nil {
		return nil
	}
	return a.expanded
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
	if expanded := a.expandedByAll(); len(expanded) > 0 {
		cliout.NewContinuation(stdout).Warning("authorize "+authorizeAll, "stood in for "+strings.Join(expanded, ", ")+" — this run consulted those gate(s) and none of them was named on the command line")
	}
	for _, name := range a.unused() {
		cliout.NewContinuation(stdout).Warning("authorize", "--authorize "+name+" had no effect: "+authorizationInertReason(name))
	}
}
