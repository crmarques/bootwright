package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func writeHelp(w io.Writer, command *cobra.Command) error {
	var text display
	text.headline("", strings.ReplaceAll(command.CommandPath(), "bootwright ", "bootwright / "))
	description := command.Long
	if description == "" {
		description = command.Short
	}
	text.mutedLines([]string{description})
	text.section("Usage")
	text.lines([]string{helpUsage(command)})
	if command == command.Root() {
		for _, group := range rootHelpGroups {
			var rows [][]string
			for _, name := range group.names {
				if child := exactChild(command, name); child != nil && !child.Hidden {
					rows = append(rows, []string{name, child.Short})
				}
			}
			text.section(group.title)
			text.resourceTable([]string{"COMMAND", "PURPOSE"}, rows, tableStyle{Kind: "Command", Identity: 0})
		}
	} else if command.HasAvailableSubCommands() {
		var rows [][]string
		for _, child := range command.Commands() {
			if !child.Hidden {
				rows = append(rows, []string{child.Name(), child.Short})
			}
		}
		text.section("Commands")
		text.resourceTable([]string{"COMMAND", "PURPOSE"}, rows, tableStyle{Kind: "Command", Identity: 0})
	}
	appendFlags := func(title string, flags *pflag.FlagSet, advanced bool) {
		var values []field
		flags.VisitAll(func(flag *pflag.Flag) {
			if flag.Hidden || !acceptsFlag(command.Annotations, flag.Name) || strings.HasPrefix(flag.Name, "ssh-") != advanced {
				return
			}
			label := "--" + flag.Name
			if flag.Shorthand != "" {
				label = "-" + flag.Shorthand + ", " + label
			}
			usage := flag.Usage
			if flag.DefValue != "" && flag.DefValue != "false" && flag.DefValue != "[]" {
				usage += " · default " + flag.DefValue
			}
			values = append(values, field{Label: label, Value: usage})
		})
		if len(values) > 0 {
			text.section(title)
			text.mutedFields(values...)
		}
	}
	appendFlags("Command flags", command.LocalNonPersistentFlags(), false)
	common := command.InheritedFlags()
	if command == command.Root() {
		common = command.PersistentFlags()
	}
	appendFlags("Common flags", common, false)
	if presentationFor(w).Detail == "full" {
		appendFlags("SSH overrides", common, true)
	} else if acceptsFlag(command.Annotations, "ssh-user") {
		text.section("More detail")
		text.lines([]string{"Use --help --detail full to include SSH overrides."})
	}
	if command.Example != "" {
		text.section("Examples")
		for _, line := range strings.Split(command.Example, "\n") {
			text.lines([]string{strings.TrimSpace(line)})
		}
	}
	return text.writeTo(w)
}

func helpUsage(command *cobra.Command) string {
	switch command.CommandPath() {
	case "bootwright":
		return "bootwright [command] [flags]"
	}
	if command.HasAvailableSubCommands() {
		return command.CommandPath() + " <command> [flags]"
	}
	return command.UseLine()
}

func writeConciseHelp(w io.Writer, command *cobra.Command) error {
	_, err := fmt.Fprintf(w, "Usage: %s\nRun 'bootwright help' for available commands.\n", command.UseLine())
	return err
}

func presentationCommand(path string) bool { return specFor(path).presentation }

func helpExample(spec commandSpec) string {
	if !spec.implemented && !spec.presentation {
		return ""
	}
	return "  " + strings.TrimSpace("bootwright "+spec.path+" "+spec.example)
}
