package cli

import (
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	contextFreeAnnotation = "bootwright.contextFree"
	sshFlagsAnnotation    = "bootwright.sshFlags"
	helpAnnotation        = "bootwright.help"
)

var (
	allSSHFlags     = []string{"ssh-id-file", "ssh-user", "ssh-ask-sudo-password", "ssh-user-for-provisioned"}
	accountSSHFlags = []string{"ssh-id-file", "ssh-user"}
	sessionSSHFlags = []string{"ssh-id-file", "ssh-user", "ssh-ask-password"}
)

func inheritedFlagScope(path string) (contextFree bool, sshFlags []string) {
	spec := specFor(path)
	return spec.contextFree, spec.sshFlags
}

func isHelpCommand(command *cobra.Command) bool {
	return command != nil && command.Annotations[helpAnnotation] == "true"
}

func acceptsFlag(commandAnnotations map[string]string, name string) bool {
	switch {
	case name == "context":
		return commandAnnotations[contextFreeAnnotation] != "true"
	case strings.HasPrefix(name, "ssh-"):
		flags, scoped := commandAnnotations[sshFlagsAnnotation]
		return !scoped || slices.Contains(strings.Split(flags, ","), name)
	}
	return true
}

func flagHelp(command *cobra.Command, flag *pflag.Flag) string {
	if flag.Name == "context" {
		if usage := specFor(command.Annotations["bootwright.command"]).contextHelp; usage != "" {
			return usage
		}
	}
	return flag.Usage
}
