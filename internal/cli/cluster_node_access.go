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

func shortHostLabel(hostname string) string {
	if i := strings.IndexByte(hostname, '.'); i >= 0 {
		return hostname[:i]
	}
	return hostname
}

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

func nodeSelectorHint(n stateview.ClusterNode) string {
	if n.Kind == stateview.MachineClusterKindContainer && n.Role != "" {
		return fmt.Sprintf("%s-%d", n.Role, n.Ordinal)
	}
	return n.MachineName
}

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
