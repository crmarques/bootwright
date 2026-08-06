package cli

import (
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge"
)

var commandOwnValueFlags = append(slices.Clone(globalValueFlags), "--name")

var tailForwardingCommands = map[string][]string{
	"container-cluster": {"oc", "kubectl"},
}

func argsBeforeCommandPayload(args []string) []string {
	args = argsBeforeCommandTerminator(args)
	start := leadingGlobalFlagCount(args)
	command := args[start:]
	if !commandForwardsTail(command) {
		return args
	}
	return args[:start+forwardedTailStart(command)]
}

func argsBeforeCommandTerminator(args []string) []string {
	for i, arg := range args {
		if arg == "--" {
			return args[:i]
		}
	}
	return args
}

func commandForwardsTail(args []string) bool {
	return len(args) >= 2 && slices.Contains(tailForwardingCommands[args[0]], args[1])
}

func forwardedTailStart(args []string) int {
	for i := 2; i < len(args); i++ {
		if !strings.HasPrefix(args[i], "-") {
			return i
		}
		if slices.Contains(commandOwnValueFlags, args[i]) {
			i++
		}
	}
	return len(args)
}

func argsNeedLocalRoot(args []string) bool {
	args = argsBeforeCommandPayload(args)
	if len(args) == 0 || argsContainHelp(args) || argsHaveUnusableSSHUser(args) {
		return false
	}
	switch args[0] {
	case "version", "help", "completion", "example", cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd:
		return false
	case "validate":
		return validateArgsNeedLocalRoot(args[1:])
	case "plan", "diff", "status":
		return true
	case "apply":
		return applyArgsNeedLocalRoot(args[1:])
	case "destroy":
		return destroyArgsNeedLocalRoot(args[1:])
	case "bastion":
		if len(args) == 1 {
			return false
		}
		return true
	case "render":
		if len(args) == 1 {
			return false
		}
		if !renderArgsHaveExecutionTarget(args[1:]) {
			return false
		}
		return true
	case "preflight":
		if len(args) == 1 {
			return false
		}
		return true
	case "machine":
		if len(args) == 1 {
			return false
		}
		switch args[1] {
		case "rsh", "exec":
			return argsHaveNameValue(args[2:])
		case "list", "trust":
			return true
		default:
			return false
		}
	case "context":
		if len(args) < 2 {
			return false
		}
		switch args[1] {
		case "init", "update", "delete":
			return false
		case "list", "use", "current":
			return true
		default:
			return false
		}
	case "secret":
		if len(args) == 1 {
			return false
		}
		switch args[1] {
		case "set":
			return false
		case "delete", "show":
			return argsHaveNameValue(args[2:])
		case "generate", "list", "check", "encryption":
			return true
		default:
			return false
		}
	case "media":
		if len(args) == 1 {
			return false
		}
		switch args[1] {
		case "add", "delete":
			return argsHaveNameValue(args[2:])
		case "list":
			return true
		default:
			return false
		}
	case "add-ons":
		if len(args) == 1 {
			return false
		}
		switch args[1] {
		case "add", "delete":
			return argsHaveNameValue(args[2:])
		case "list":
			return true
		default:
			return false
		}
	case "cluster":
		if len(args) == 1 {
			return false
		}
		switch args[1] {
		case "list", "info":
			return true
		case "rsh", "exec":
			return argsHaveNameValue(args[2:])
		default:
			return false
		}
	case "container-cluster":
		if len(args) == 1 {
			return false
		}
		switch args[1] {
		case "oc", "kubectl", "kubeconfig":
			return argsHaveNameValue(args[2:])
		default:
			return false
		}
	case "storage-cluster":
		if len(args) == 1 {
			return false
		}
		switch args[1] {
		case "replace-arbiter":
			return argsHaveNameValue(args[2:])
		default:
			return false
		}
	default:
		return false
	}
}

func applyArgsNeedLocalRoot(args []string) bool {
	stageValid := converge.ApplyStageNames()
	throughValid := converge.ApplyThroughNames()
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--stage" && i+1 < len(args):
			if !slices.Contains(stageValid, args[i+1]) {
				return false
			}
			i++
		case arg == "--through" && i+1 < len(args):
			if !slices.Contains(throughValid, args[i+1]) {
				return false
			}
			i++
		case strings.HasPrefix(arg, "--stage="):
			if !slices.Contains(stageValid, strings.TrimPrefix(arg, "--stage=")) {
				return false
			}
		case strings.HasPrefix(arg, "--through="):
			if !slices.Contains(throughValid, strings.TrimPrefix(arg, "--through=")) {
				return false
			}
		}
	}
	return true
}

func renderArgsHaveExecutionTarget(args []string) bool {
	if argsHaveInputDirValue(args) {
		return false
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "installer" || arg == "storage" || arg == "effective":
			return true
		case arg == "--output-dir":
			return i+1 < len(args)
		case strings.HasPrefix(arg, "--output-dir="):
			return strings.TrimPrefix(arg, "--output-dir=") != ""
		}
	}
	return false
}

func argsHaveInputDirValue(args []string) bool {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--input-dir":
			return i+1 < len(args) && args[i+1] != ""
		case strings.HasPrefix(arg, "--input-dir="):
			return strings.TrimPrefix(arg, "--input-dir=") != ""
		}
	}
	return false
}

func argsHaveUnusableSSHUser(args []string) bool {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--ssh-user":
			return i+1 >= len(args) || !v1alpha1.ValidPOSIXUserName(strings.TrimSpace(args[i+1]))
		case strings.HasPrefix(arg, "--ssh-user="):
			return !v1alpha1.ValidPOSIXUserName(strings.TrimSpace(strings.TrimPrefix(arg, "--ssh-user=")))
		}
	}
	return false
}

func argsHaveNameValue(args []string) bool {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--name":
			return i+1 < len(args) && args[i+1] != ""
		case strings.HasPrefix(arg, "--name="):
			return strings.TrimPrefix(arg, "--name=") != ""
		}
	}
	return false
}

func argsContainHelp(args []string) bool {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" || arg == "help" {
			return true
		}
	}
	return false
}

func validateArgsNeedLocalRoot(args []string) bool {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-f" || arg == "--file":
			return false
		case strings.HasPrefix(arg, "--file=") || (strings.HasPrefix(arg, "-f") && len(arg) > 2):
			return false
		case arg == "--output":
			if i+1 >= len(args) {
				return false
			}
			i++
		case strings.HasPrefix(arg, "--output="):
		default:
			return false
		}
	}
	return true
}

func argsMayMutateRegistry(args []string) bool {
	if len(args) < 2 || args[0] != "context" {
		return false
	}
	switch args[1] {
	case "init", "use":
		return true
	case "delete":
		return contextDeleteArgsHavePurge(args[2:])
	default:
		return false
	}
}

func contextDeleteArgsHavePurge(args []string) bool {
	for _, arg := range args {
		if arg == "--purge" || strings.HasPrefix(arg, "--purge=") {
			return true
		}
	}
	return false
}

func argsMayUseBecome(args []string) bool {
	args = argsBeforeCommandPayload(args)
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "apply":
		return true
	case "destroy":
		return destroyArgsMayUseBecome(args[1:])
	case "bastion":
		return len(args) >= 2 && args[1] == "setup"
	case "storage-cluster":
		return len(args) >= 2 && args[1] == "replace-arbiter"
	default:
		return false
	}
}

func destroyArgsMayUseBecome(args []string) bool {
	return destroyArgsSelectRootfulTarget(args)
}

func destroyArgsNeedLocalRoot(args []string) bool {
	return destroyArgsSelectRootfulTarget(args)
}

func destroyArgsSelectRootfulTarget(args []string) bool {
	stage := ""
	hasStage := false
	for i, arg := range args {
		switch {
		case arg == "--stage" && i+1 < len(args):
			hasStage = true
			stage = args[i+1]
		case strings.HasPrefix(arg, "--stage="):
			hasStage = true
			stage = strings.TrimPrefix(arg, "--stage=")
		}
	}
	if hasStage {
		return stage == "infra" || stage == "clusters"
	}
	return true
}
