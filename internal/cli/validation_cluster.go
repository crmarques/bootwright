package cli

import (
	"strings"

	"github.com/spf13/pflag"
)

func validateClusterNames(flags *pflag.FlagSet, path string) string {
	rawName, rawNode := stringValue(flags, "name"), stringValue(flags, "node")
	if path == "cluster rsh" {
		if strings.Contains(rawName, ",") || strings.Contains(rawNode, ",") {
			return "cluster rsh opens one interactive session; name one cluster and at most one node"
		}
		return ""
	}
	names := normalizeNames(rawName)
	switch {
	case len(names) == 0 && path == "cluster info" && rawName == "":
		if boolValue(flags, "secrets") {
			return "--secrets requires an explicit --name"
		}
		return ""
	case len(names) == 0:
		return "--name must name at least one cluster"
	case path == "cluster info" && boolValue(flags, "secrets") && len(names) > 1:
		return "--secrets reveals one cluster's values; name one cluster"
	}
	normalizeScalar(flags, "name", strings.Join(names, ","))
	if path != "cluster exec" || !flags.Changed("node") {
		return ""
	}
	nodes := normalizeNames(rawNode)
	if len(nodes) == 0 {
		return "--node must name at least one node"
	}
	normalizeScalar(flags, "node", strings.Join(nodes, ","))
	return ""
}

func validateInfoNames(flags *pflag.FlagSet, path string) string {
	if !flags.Changed("name") {
		return ""
	}
	names := normalizeNames(stringValue(flags, "name"))
	if len(names) == 0 {
		return "--name must name at least one " + strings.TrimSuffix(path, " info")
	}
	normalizeScalar(flags, "name", strings.Join(names, ","))
	return ""
}
