package cli

import (
	"fmt"
	"io"
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
)

type authorizationToken struct {
	name       string
	authorizes string
	inert      string
}

var authorizationTokens = []authorizationToken{{
	name:       authorizeDataLoss,
	authorizes: "any disk wipe or Ceph OSD zap, on apply and on destroy",
	inert:      "this run plans no data-destroying action",
}, {
	name:       authorizeProtected,
	authorizes: "acting on state whose Environment sets spec.safety.destroyProtection or spec.safety.protectedKinds",
	inert:      "no selected object is protected by an Environment spec.safety rule",
}, {
	name:       authorizeInstalledClusterNode,
	authorizes: "destroy --machines naming a node of an installed cluster",
	inert:      "no selected machine is a node of an installed cluster",
}, {
	name:       authorizeUnownedVMs,
	authorizes: "tearing down libvirt/KubeVirt/vSphere VMs that match the Bootwright naming but carry no ownership marker",
	inert:      "this run tears down no machine VMs (their ownership refusals live in the infra stage)",
}, {
	name:       authorizeUnownedNetworks,
	authorizes: "removing an unowned libvirt network or KubeVirt DataVolume, which may still be in use by another context",
	inert:      "this run removes no libvirt network or KubeVirt DataVolume (their ownership refusals live in the infra stage)",
}, {
	name:       authorizeUnreachableNodes,
	authorizes: "leaving a cluster partially destroyed by skipping unreachable nodes",
	inert:      "this run contacts no node whose unreachability could be skipped",
}, {
	name:       authorizeUnreadableRecords,
	authorizes: "proceeding when ownership records cannot be read, leaving their resources standing",
	inert:      "every ownership record of this context was read",
}, {
	name:       authorizeSharedInfra,
	authorizes: "storage-consumer conflicts and shared infra components owned or referenced by another context",
	inert:      "no storage-consumer conflict and no cross-context infra-component block was found",
}}

func authorizationTokenNames() []string {
	out := make([]string, 0, len(authorizationTokens))
	for _, token := range authorizationTokens {
		out = append(out, token.name)
	}
	return out
}

func authorizationInertReason(name string) string {
	for _, token := range authorizationTokens {
		if token.name == name {
			return token.inert
		}
	}
	return "this run has no gate for it"
}

func flagAuthorizeUsage() string {
	lines := make([]string, 0, len(authorizationTokens))
	for _, token := range authorizationTokens {
		lines = append(lines, token.name+" = "+token.authorizes)
	}
	return "authorize a named risk; repeatable and comma-separated. Each token unblocks exactly one refusal and nothing else; --yes never authorizes any of them. Tokens: " + strings.Join(lines, "; ")
}

func addAuthorizeFlag(cmd *cobra.Command, p *[]string) {
	cmd.Flags().StringSliceVar(p, "authorize", nil, flagAuthorizeUsage())
	registerFlagCompletion(cmd, "authorize", authorizationTokenNames())
}

type authorizations struct {
	given   map[string]bool
	applied map[string]bool
	order   []string
}

func parseAuthorizations(values []string) (*authorizations, error) {
	out := &authorizations{given: map[string]bool{}, applied: map[string]bool{}}
	valid := map[string]bool{}
	for _, token := range authorizationTokens {
		valid[token.name] = true
	}
	for _, value := range values {
		name := strings.TrimSpace(value)
		if name == "" {
			return nil, fmt.Errorf("--authorize takes a non-empty token; valid tokens: %s", strings.Join(authorizationTokenNames(), ", "))
		}
		if !valid[name] {
			return nil, fmt.Errorf("--authorize %q is not an authorization token; valid tokens: %s", name, strings.Join(authorizationTokenNames(), ", "))
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
