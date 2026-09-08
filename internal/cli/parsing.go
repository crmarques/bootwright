package cli

import (
	"errors"
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
	failure := func(remaining []string, message string) (invocation, error) {
		arguments := append([]string(nil), forwarded...)
		for i := len(localFlags) - 1; i >= 0; i-- {
			occurrence := localFlags[i]
			if occurrence.owner != current {
				arguments = append(arguments[:occurrence.start], arguments[occurrence.end:]...)
			}
		}
		return invocation{command: current, arguments: append(arguments, remaining...)}, errors.New(message)
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			if !hasPayload(current) || helpCommand {
				return failure(args[i:], "separator is not accepted")
			}
			forwarded = append(forwarded, args[i:]...)
			break
		}
		if strings.HasPrefix(arg, "-") && arg != "-" && !operands {
			flag, attached := tokenFlag(root, current, arg)
			if flag == nil {
				return failure(args[i:], "flag is not accepted at this position")
			}
			start := len(forwarded)
			forwarded = append(forwarded, arg)
			if flag.NoOptDefVal == "" && !attached {
				if i+1 == len(args) {
					return failure(nil, "flag value is missing")
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
				return failure(args[i:], "unknown command")
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
			return failure(nil, "local flag is not inherited")
		}
	}
	command, remaining, err := root.Find(path)
	if err != nil || len(remaining) != 0 || command != current {
		return failure(nil, "command resolution failed")
	}
	result := invocation{command: command, arguments: forwarded}
	if helpCommand {
		target := root
		for _, token := range helpPath {
			target = exactChild(target, token)
			if target == nil || target.Hidden {
				return failure(nil, "unknown help command")
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
