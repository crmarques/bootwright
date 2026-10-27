package cli

import (
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
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
	sensitive := ""
	if path == "cluster info" {
		switch user := stringValue(flags, "object-user"); {
		case flags.Changed("object-user") && !api.ValidLexical("name", user):
			return "--object-user must name one CephObjectUser"
		case user != "" && boolValue(flags, "secrets"):
			return "--secrets and --object-user each reveal one credential; use one of them"
		case user != "":
			sensitive = "--object-user"
		case boolValue(flags, "secrets"):
			sensitive = "--secrets"
		}
	}
	switch {
	case len(names) == 0 && path == "cluster info" && rawName == "":
		if sensitive != "" {
			return sensitive + " requires an explicit --name"
		}
		return ""
	case len(names) == 0:
		return "--name must name at least one cluster"
	case sensitive != "" && len(names) > 1:
		return sensitive + " reveals one cluster's values; name one cluster"
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
