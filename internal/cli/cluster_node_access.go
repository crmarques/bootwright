package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/clusteraccess"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

// clusterNodeMachine resolves --name/--node to the Machine name backing the
// selected node. An empty selector connects to the only node of a single-node
// cluster; otherwise it errors with the node roster so the operator can pick
// one. A no-match / ambiguous selector likewise errors with the roster. Callers
// treat every error here as a usage error (exit 2).
func clusterNodeMachine(state v1alpha1.State, clusterName, selector string) (string, error) {
	nodes, ok := stateview.ClusterNodes(state, clusterName)
	if !ok || len(nodes) == 0 {
		return "", fmt.Errorf("cluster %q has no nodes to connect to", clusterName)
	}
	if selector == "" {
		if len(nodes) == 1 {
			return nodes[0].MachineName, nil
		}
		return "", fmt.Errorf("cluster %q has %d nodes; select one with --node:\n%s", clusterName, len(nodes), clusterNodeRoster(nodes))
	}
	machine, err := resolveClusterNode(nodes, selector)
	if err != nil {
		return "", fmt.Errorf("%w:\n%s", err, clusterNodeRoster(nodes))
	}
	return machine, nil
}

// resolveClusterNode maps a --node selector to a backing Machine name, trying in
// order: exact Machine name, node hostname (full FQDN or its short left-most
// label), then <role>-<ordinal> (ordinal counted among same-role nodes in
// declaration order). A selector matching more than one node by hostname is
// rejected as ambiguous.
func resolveClusterNode(nodes []stateview.ClusterNode, selector string) (string, error) {
	for _, n := range nodes {
		if n.MachineName == selector {
			return n.MachineName, nil
		}
	}
	var byHost []string
	for _, n := range nodes {
		if n.Hostname != "" && (n.Hostname == selector || shortHostLabel(n.Hostname) == selector) {
			byHost = append(byHost, n.MachineName)
		}
	}
	if len(byHost) == 1 {
		return byHost[0], nil
	}
	if len(byHost) > 1 {
		return "", fmt.Errorf("node %q is ambiguous in this cluster (matches machines %s)", selector, strings.Join(byHost, ", "))
	}
	if role, ordinal, ok := parseRoleOrdinal(selector); ok {
		count := 0
		for _, n := range nodes {
			if !n.HasRole(role) {
				continue
			}
			if count == ordinal {
				return n.MachineName, nil
			}
			count++
		}
	}
	return "", fmt.Errorf("no node %q in this cluster", selector)
}

// shortHostLabel returns a hostname's left-most label — the short name before
// the first dot, or the whole string when it carries no domain.
func shortHostLabel(hostname string) string {
	if i := strings.IndexByte(hostname, '.'); i >= 0 {
		return hostname[:i]
	}
	return hostname
}

// parseRoleOrdinal splits a "<role>-<ordinal>" selector (master-0, worker-2)
// into its role and zero-based index. It requires a non-empty role prefix and a
// trailing "-<digits>".
func parseRoleOrdinal(selector string) (string, int, bool) {
	i := strings.LastIndexByte(selector, '-')
	if i <= 0 || i == len(selector)-1 {
		return "", 0, false
	}
	ordinal, err := strconv.Atoi(selector[i+1:])
	if err != nil || ordinal < 0 {
		return "", 0, false
	}
	return selector[:i], ordinal, true
}

// clusterNodeRoster formats the cluster's nodes for a selection error or the
// info cross-link: one line per node with its selector hint, backing Machine,
// and hostname.
func clusterNodeRoster(nodes []stateview.ClusterNode) string {
	var b strings.Builder
	for _, n := range nodes {
		b.WriteString("  ")
		b.WriteString(nodeSelectorHint(n))
		b.WriteString("  machine=")
		b.WriteString(n.MachineName)
		if n.Hostname != "" {
			b.WriteString("  ")
			b.WriteString(n.Hostname)
		}
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

// nodeSelectorHint is the friendliest stable --node value for a node: its
// <role>-<ordinal> for a container node, else its Machine name (Ceph hosts fill
// several roles, so role-ordinal is not a stable single selector there).
func nodeSelectorHint(n stateview.ClusterNode) string {
	if n.Kind == stateview.MachineClusterKindContainer && n.Role != "" {
		return fmt.Sprintf("%s-%d", n.Role, n.Ordinal)
	}
	return n.MachineName
}

// printClusterNodeAccess prints the ready-to-run `cluster rsh` command for each
// node of a cluster, so the command operators run to see access details also
// tells them how to shell into any node.
func printClusterNodeAccess(p *cliout.Printer, state v1alpha1.State, clusterName string) {
	nodes, ok := stateview.ClusterNodes(state, clusterName)
	if !ok || len(nodes) == 0 {
		return
	}
	fields := make([]cliout.Field, 0, len(nodes))
	for _, n := range nodes {
		hint := nodeSelectorHint(n)
		fields = append(fields, cliout.Field{
			Key:   "Node " + hint,
			Value: "bootwright cluster rsh --name " + clusterName + " --node " + hint,
		})
	}
	p.Fields(fields)
}

// revealValue returns a captured secret's cleartext for --secrets output, or a
// short note when the file is missing or unreadable (the paths and 'sudo cat'
// hint the default output already carries remain the fallback).
func revealValue(path string, artifact clusteraccess.Artifact) string {
	if !artifact.Present {
		return "(missing)"
	}
	value, err := clusteraccess.RevealSecretFile(path)
	if err != nil {
		return "(unreadable; try: sudo cat " + path + ")"
	}
	return value
}
