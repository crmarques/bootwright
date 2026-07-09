package cli

import (
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/converge"
	"github.com/spf13/cobra"
)

func argsNeedLocalRoot(args []string) bool {
	if len(args) == 0 || argsContainHelp(args) {
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
		case "add", "remove", "rm":
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
		case "list", "info", "kubeconfig":
			return true
		case "rsh", "exec":
			return argsHaveNameValue(args[2:])
		default:
			return false
		}
	default:
		return false
	}
}

func applyArgsNeedLocalRoot(args []string) bool {
	valid := converge.ApplyStageNames()
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case (arg == "--stage" || arg == "--through") && i+1 < len(args):
			if !slices.Contains(valid, args[i+1]) {
				return false
			}
			i++
		case strings.HasPrefix(arg, "--stage="):
			if !slices.Contains(valid, strings.TrimPrefix(arg, "--stage=")) {
				return false
			}
		case strings.HasPrefix(arg, "--through="):
			if !slices.Contains(valid, strings.TrimPrefix(arg, "--through=")) {
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
	if len(args) >= 1 && args[0] == "apply" {
		return true
	}
	if len(args) >= 1 && args[0] == "destroy" {
		return destroyArgsMayUseBecome(args[1:])
	}
	if len(args) >= 2 && args[0] == "bastion" && args[1] == "setup" {
		return true
	}
	if len(args) < 2 {
		return false
	}
	return false
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
