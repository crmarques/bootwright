package cli

import "strings"

const (
	contextFreeAnnotation = "bootwright.contextFree"
	sshFreeAnnotation     = "bootwright.sshFree"
)

var sshCommands = map[string]bool{
	"preflight infra": true, "preflight clusters": true, "preflight container-cluster": true,
	"preflight storage-cluster": true, "preflight add-ons": true, "preflight all": true,
	"apply": true, "destroy": true,
	"machine rsh": true, "machine exec": true, "cluster rsh": true, "cluster exec": true,
}

var contextFreeCommands = map[string]bool{
	"context init": true, "context update": true, "context use": true, "context list": true,
	"context current": true, "context delete": true, "add-ons list": true,
	"media add": true, "media list": true, "media delete": true,
	"setup": true, "controller rebind": true,
	"version": true, "schema": true, "help": true,
	"completion bash": true, "completion zsh": true, "completion fish": true, "completion powershell": true,
}

func inheritedFlagScope(path string) (contextFree, sshFree bool) {
	return contextFreeCommands[path], !sshCommands[path]
}

func acceptsFlag(commandAnnotations map[string]string, name string) bool {
	switch {
	case name == "context":
		return commandAnnotations[contextFreeAnnotation] != "true"
	case strings.HasPrefix(name, "ssh-"):
		return commandAnnotations[sshFreeAnnotation] != "true"
	}
	return true
}
