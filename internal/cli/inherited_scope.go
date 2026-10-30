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
	allSSHFlags       = []string{"ssh-id-file", "ssh-user", "ssh-ask-sudo-password", "ssh-user-for-provisioned"}
	accountSSHFlags   = []string{"ssh-id-file", "ssh-user"}
	sshFlagsByCommand = map[string][]string{
		"preflight clusters": allSSHFlags, "preflight container-cluster": allSSHFlags,
		"preflight storage-cluster": allSSHFlags, "preflight add-ons": allSSHFlags, "preflight all": allSSHFlags,
		"apply": allSSHFlags, "destroy": allSSHFlags,
		"machine rsh": accountSSHFlags, "machine exec": accountSSHFlags,
		"cluster rsh": accountSSHFlags, "cluster exec": accountSSHFlags,
	}
)

var contextFreeCommands = map[string]bool{
	"context init": true, "context update": true, "context use": true, "context list": true,
	"context current": true, "context delete": true, "add-ons list": true,
	"media add": true, "media list": true, "media delete": true,
	"controller install": true, "controller rebind": true, "controller uninstall": true,
	"version": true, "schema": true, "help": true,
	"completion bash": true, "completion zsh": true, "completion fish": true, "completion powershell": true,
}

func inheritedFlagScope(path string) (contextFree bool, sshFlags []string) {
	return contextFreeCommands[path], sshFlagsByCommand[path]
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
