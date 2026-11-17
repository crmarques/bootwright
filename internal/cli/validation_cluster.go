package cli

import (
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/spf13/cobra"
)

func validateClusterAdminName(command *cobra.Command) string {
	if !api.ValidLexical(api.LexicalName, stringValue(command.Flags(), "name")) {
		return "--name must name exactly one cluster"
	}
	return ""
}

func validateClusterRsh(command *cobra.Command) string {
	flags := command.Flags()
	if strings.Contains(stringValue(flags, "name"), ",") || strings.Contains(stringValue(flags, "node"), ",") {
		return "cluster rsh opens one interactive session; name one cluster and at most one node"
	}
	return ""
}

func validateClusterInfo(command *cobra.Command) string {
	return validateClusterNames(command, true)
}

func validateClusterExec(command *cobra.Command) string {
	return validateClusterNames(command, false)
}

func validateClusterNames(command *cobra.Command, info bool) string {
	flags := command.Flags()
	rawName, rawNode := stringValue(flags, "name"), stringValue(flags, "node")
	names := normalizeNames(rawName)
	sensitive := ""
	if info {
		switch user := stringValue(flags, "object-user"); {
		case flags.Changed("object-user") && !api.ValidLexical(api.LexicalName, user):
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
	case len(names) == 0 && info && rawName == "":
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
	if info || !flags.Changed("node") {
		return ""
	}
	nodes := normalizeNames(rawNode)
	if len(nodes) == 0 {
		return "--node must name at least one node"
	}
	normalizeScalar(flags, "node", strings.Join(nodes, ","))
	return ""
}
