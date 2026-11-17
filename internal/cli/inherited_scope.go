package cli

import (
	"slices"
	"strings"
)

const (
	contextFreeAnnotation = "bootwright.contextFree"
	sshFlagsAnnotation    = "bootwright.sshFlags"
)

var (
	allSSHFlags     = []string{"ssh-id-file", "ssh-user", "ssh-ask-sudo-password", "ssh-user-for-provisioned"}
	accountSSHFlags = []string{"ssh-id-file", "ssh-user"}
)

func inheritedFlagScope(path string) (contextFree bool, sshFlags []string) {
	spec := specFor(path)
	return spec.contextFree, spec.sshFlags
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
