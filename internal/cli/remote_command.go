package cli

import (
	"errors"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/machine"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const remoteWordsHelp = " Each word reaches the remote command exactly as given: no shell on the Machine expands" +
	" a glob, a variable, a pipe or a redirection in it. --shell runs the words as one script of the Machine's /bin/sh," +
	" where $name is the Machine's value. --var <name>=<value> sets $name in that script to a value from this host," +
	" quoted as data, so quote the script from your own shell and let it expand only the --var values;" +
	" a value is visible in process listings, so never pass a secret." +
	" --sudo runs the command, or the whole script with its globs, through the Machine's sudo, which must not ask for a password."

func remoteCommandFlags() []flagSpec {
	bindings := stringFlag("var", "Set $<name> in the --shell script to a value from this host, as <name>=<value> (repeatable)")
	bindings.kind = "stringArray"
	return []flagSpec{
		boolFlag("shell", "Run the command as one script of the Machine's /bin/sh, which expands its variables, globs, pipes and redirections"),
		bindings,
		boolFlag("sudo", "Run the command through the Machine's sudo, without a password prompt"),
	}
}

func validateRemoteCommand(command *cobra.Command) string {
	if _, err := remoteCommand(command.Flags(), command.Flags().Args()); err != nil {
		return err.Error()
	}
	return ""
}

func remoteCommand(flags *pflag.FlagSet, words []string) ([]string, error) {
	command := machine.RemoteCommand{Words: slices.Clone(words), Shell: boolValue(flags, "shell"), Sudo: boolValue(flags, "sudo")}
	for _, entry := range arrayValue(flags, "var") {
		name, value, found := strings.Cut(entry, "=")
		if !found {
			return nil, errors.New("a --var entry has no =: write --var <name>=<value>")
		}
		command.Bindings = append(command.Bindings, machine.ShellBinding{Name: name, Value: value})
	}
	return command.Vector()
}
