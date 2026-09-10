package cli

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	maxArguments          = 1024
	maxArgumentBytes      = 16384
	maxTotalArgumentBytes = 1048576
)

func rawBounds(args []string) string {
	if len(args) > maxArguments {
		return "argument count exceeds the supported limit"
	}
	total := 0
	for _, arg := range args {
		if len(arg) > maxArgumentBytes {
			return "argument length exceeds the supported limit"
		}
		total += len(arg)
		if total > maxTotalArgumentBytes {
			return "total argument length exceeds the supported limit"
		}
	}
	return ""
}

type invocation struct {
	command    *cobra.Command
	arguments  []string
	helpTarget *cobra.Command
}

type resolutionError string

const (
	separatorNotAccepted    resolutionError = "separator is not accepted"
	flagNotAccepted         resolutionError = "flag is not accepted at this position"
	flagValueMissing        resolutionError = "flag value is missing"
	unknownCommand          resolutionError = "unknown command"
	localFlagNotInherited   resolutionError = "flag does not apply to the selected subcommand"
	commandResolutionFailed resolutionError = "command resolution failed"
	unknownHelpCommand      resolutionError = "unknown help command"
)

func (e resolutionError) Error() string { return string(e) }

func trustedResolutionMessage(err error) string {
	resolution, ok := err.(resolutionError)
	if ok {
		switch resolution {
		case separatorNotAccepted, flagNotAccepted, flagValueMissing, unknownCommand,
			localFlagNotInherited, commandResolutionFailed, unknownHelpCommand:
			return resolution.Error()
		}
	}
	return "invalid command or flag syntax"
}

type localFlagOccurrence struct {
	owner      *cobra.Command
	start, end int
}

func resolveInvocation(root *cobra.Command, args []string) (invocation, error) {
	current := root
	var path, forwarded, helpPath []string
	var localFlags []localFlagOccurrence
	operands := false
	pathComplete := false
	helpCommand := false
	failure := func(remaining []string, reason resolutionError) (invocation, error) {
		arguments := append([]string(nil), forwarded...)
		for i := len(localFlags) - 1; i >= 0; i-- {
			occurrence := localFlags[i]
			if occurrence.owner != current {
				arguments = append(arguments[:occurrence.start], arguments[occurrence.end:]...)
			}
		}
		return invocation{command: current, arguments: append(arguments, remaining...)}, reason
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			if !hasPayload(current) || helpCommand {
				return failure(args[i:], separatorNotAccepted)
			}
			forwarded = append(forwarded, args[i:]...)
			break
		}
		if strings.HasPrefix(arg, "-") && arg != "-" && !operands {
			flag, attached := tokenFlag(root, current, arg)
			if flag == nil {
				return failure(args[i:], flagNotAccepted)
			}
			start := len(forwarded)
			forwarded = append(forwarded, arg)
			if flag.NoOptDefVal == "" && !attached {
				if i+1 == len(args) {
					return failure(nil, flagValueMissing)
				}
				i++
				forwarded = append(forwarded, args[i])
			}
			if root.PersistentFlags().Lookup(flag.Name) == nil {
				localFlags = append(localFlags, localFlagOccurrence{owner: current, start: start, end: len(forwarded)})
			}
			continue
		}
		if helpCommand {
			helpPath = append(helpPath, arg)
			continue
		}
		if !pathComplete {
			if child := exactChild(current, arg); child != nil && !child.Hidden {
				current = child
				path = append(path, arg)
				helpCommand = child.CommandPath() == "bootwright help"
				continue
			}
			if !current.Runnable() {
				return failure(args[i:], unknownCommand)
			}
		}
		forwarded = append(forwarded, arg)
		pathComplete = true
		if current.CommandPath() == "bootwright cluster oc" || current.CommandPath() == "bootwright cluster kubectl" {
			operands = true
		}
	}
	for _, occurrence := range localFlags {
		if occurrence.owner != current {
			return failure(nil, localFlagNotInherited)
		}
	}
	command, remaining, err := root.Find(path)
	if err != nil || len(remaining) != 0 || command != current {
		return failure(nil, commandResolutionFailed)
	}
	result := invocation{command: command, arguments: forwarded}
	if helpCommand {
		target := root
		for _, token := range helpPath {
			target = exactChild(target, token)
			if target == nil || target.Hidden {
				return failure(nil, unknownHelpCommand)
			}
		}
		result.helpTarget = target
	}
	return result, nil
}

func syntaxJSON(command *cobra.Command, args []string) bool {
	if command.LocalNonPersistentFlags().Lookup("output") == nil {
		return false
	}
	output := "text"
	for i := 0; i < len(args); i++ {
		if args[i] == "--" {
			break
		}
		if !strings.HasPrefix(args[i], "-") {
			continue
		}
		flag, attached := tokenFlag(command.Root(), command, args[i])
		if flag == nil {
			continue
		}
		value := ""
		if attached {
			_, value, _ = strings.Cut(args[i], "=")
		}
		if flag.NoOptDefVal == "" && !attached {
			if i+1 < len(args) {
				i++
				value = args[i]
			}
		}
		if flag.Name == "output" {
			output = value
		}
	}
	return output == "json"
}

func tokenFlag(root, current *cobra.Command, token string) (*pflag.Flag, bool) {
	if strings.HasPrefix(token, "--") {
		name, _, attached := strings.Cut(token[2:], "=")
		if flag := root.PersistentFlags().Lookup(name); flag != nil {
			return flag, attached
		}
		return current.LocalNonPersistentFlags().Lookup(name), attached
	}
	if len(token) < 2 {
		return nil, false
	}
	short := token[1:2]
	flag := root.PersistentFlags().ShorthandLookup(short)
	if flag == nil {
		flag = current.LocalNonPersistentFlags().ShorthandLookup(short)
	}
	if flag == nil {
		return nil, false
	}
	if len(token) == 2 {
		return flag, false
	}
	if token[2] == '=' || short == "f" {
		return flag, true
	}
	return nil, false
}

func hasPayload(command *cobra.Command) bool {
	switch command.CommandPath() {
	case "bootwright machine exec", "bootwright cluster exec", "bootwright cluster oc", "bootwright cluster kubectl":
		return true
	default:
		return false
	}
}
