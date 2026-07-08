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
		// Read-only inspectors that read the root-owned context (rendered-dir,
		// secrets-dir, runs-dir, live state), so they escalate.
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
			// machine rsh/exec exec the ssh client as root to read the root-owned
			// SSH key, but a missing --name is a malformed invocation cobra should
			// reject as the caller, not after a doomed sudo prompt. Mirrors
			// secret delete / media add.
			return argsHaveNameValue(args[2:])
		case "list", "trust":
			// machine list and machine trust both read the root-owned context
			// (desired state, ownership records, the trust store), so they escalate.
			return true
		default:
			// Unknown subcommand (a typo like `machine lst`): stay rootless so
			// cobra rejects it with a "did you mean" hint as the caller.
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
			// Unknown subcommand (a typo like `context lst`): stay rootless so
			// cobra rejects it as the caller.
			return false
		}
	case "secret":
		if len(args) == 1 {
			return false
		}
		switch args[1] {
		case "set":
			// secret set writes via the local user and self-escalates only after
			// cobra parses, so the gate never escalates it.
			return false
		case "delete", "show":
			// --name-targeted commands: stay rootless unless a --name value is
			// present so a malformed invocation (the old positional form or a
			// missing --name) fails cobra as the caller, not after a doomed sudo
			// prompt. Mirrors validateArgsNeedLocalRoot.
			return argsHaveNameValue(args[2:])
		case "generate", "list", "check", "encryption":
			// Read or write the root-owned secrets-dir (and its encryption keys),
			// so they escalate.
			return true
		default:
			// Unknown subcommand (a typo like `secret lst`): stay rootless so
			// cobra rejects it as the caller.
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
			// Unknown subcommand (a typo like `media lst`): stay rootless so
			// cobra rejects it as the caller.
			return false
		}
	case "cluster":
		// Bare `cluster` is a pure dispatcher that only prints help, so it must
		// not escalate.
		if len(args) == 1 {
			return false
		}
		switch args[1] {
		case "list", "info", "kubeconfig":
			// Read the root-owned context cluster artifacts (kubeconfig,
			// passwords, install assets), so they escalate.
			return true
		case "rsh", "exec":
			// cluster rsh/exec resolve a node then exec the ssh client as root to
			// read the root-owned SSH key; a missing --name is a malformed
			// invocation that stays rootless so cobra rejects it as the caller.
			return argsHaveNameValue(args[2:])
		default:
			// Unknown subcommand (a typo like `cluster lst`): stay rootless so
			// cobra rejects it as the caller.
			return false
		}
	default:
		// Unknown top-level command (a typo like `statsu`): stay rootless so
		// cobra rejects it with a "did you mean" hint as the caller instead of a
		// doomed sudo password prompt.
		return false
	}
}

// applyArgsNeedLocalRoot reports whether an `apply` invocation must escalate. A
// valid apply reads the root-owned context, so it is rootful — but a bogus
// --stage/--through value is a malformed invocation cobra rejects, so the gate
// stays rootless to fail stage validation fast rather than after a doomed sudo
// password prompt, mirroring the destroy stage handling.
func applyArgsNeedLocalRoot(args []string) bool {
	valid := converge.ApplyStageNames()
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case (arg == "--stage" || arg == "--through") && i+1 < len(args):
			if !slices.Contains(valid, args[i+1]) {
				return false
			}
			i++ // skip the value
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
	// A context-free `render --input-dir <dir> --output-dir <dir>` reads a
	// user-owned input directory and writes a user-owned output directory with
	// {{ secret }} placeholders — no context secrets-dir, no root needed. The
	// carve-out wins over the --output-dir match below so the mode stays
	// rootless, mirroring validate's -f/--file carve-out.
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

// argsHaveInputDirValue reports whether args carry a --input-dir flag with a
// non-empty value, the shape the context-free render mode requires.
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

// argsHaveNameValue reports whether args carry a --name flag with a non-empty
// value, the shape every --name-targeted command requires. The gate uses it to
// keep a malformed invocation (the retired positional form or a missing --name)
// rootless so cobra rejects it as the caller instead of after a doomed sudo
// prompt, the same way validateArgsNeedLocalRoot handles a malformed validate.
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

// validateArgsNeedLocalRoot reports whether a `validate` invocation must read the
// root-owned current context and so has to escalate. Bare `validate` and
// `validate --output <fmt>` validate the active context: rootful. Explicit
// `-f`/`--file` input validates a user-owned directory: rootless. Any other token
// — an unknown flag like --source-dir, or a stray positional — is a malformed
// invocation cobra will reject (validate takes only -f/--file, --output, --help
// and no positional args), so the gate declines to escalate and lets the user see
// the "unknown flag" error as themselves instead of a doomed sudo password
// prompt. This mirrors how `destroy --stage bogus` stays rootless to fail
// validation fast.
func validateArgsNeedLocalRoot(args []string) bool {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-f" || arg == "--file":
			return false // explicit input directory: rootless
		case strings.HasPrefix(arg, "--file=") || (strings.HasPrefix(arg, "-f") && len(arg) > 2):
			return false // explicit input directory (--file=DIR / -fDIR): rootless
		case arg == "--output":
			if i+1 >= len(args) {
				return false // missing value: cobra rejects it rootlessly
			}
			i++ // skip the format value; still a context validation
		case strings.HasPrefix(arg, "--output="):
			// value attached; still a context validation
		default:
			// Unknown flag or stray positional: let cobra reject it rootlessly.
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
	// An omitted --stage is a whole-context full destroy (the clusters stage, then
	// the infra they ran on) — or, with a --clusters filter, the inferred clusters
	// stage. Both are rootful and must read the root-owned context, so escalate
	// before context resolution; otherwise a non-root run fails to lstat the
	// root-owned context directory. (A bogus explicit --stage stays rootless: it
	// fails stage validation fast, before any context read.)
	return true
}
