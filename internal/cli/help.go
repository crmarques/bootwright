package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// controllerContextUsage describes --context for the two commands that never
// fall back to the current context.
func controllerContextUsage(path string) (string, bool) {
	switch path {
	case "setup":
		return "Ignored: setup selects no context", true
	case "preflight controller":
		return "Select explicit Environment requirements (default: baseline)", true
	}
	return "", false
}

func writeHelp(w io.Writer, command *cobra.Command) error {
	var text display
	text.headline("", strings.ReplaceAll(command.CommandPath(), "bootwright ", "bootwright / "))
	description := command.Long
	if description == "" {
		description = command.Short
	}
	text.mutedLines([]string{description})
	text.section("Usage")
	text.lines([]string{command.UseLine()})
	if command == command.Root() {
		for _, group := range []struct{ title, names string }{
			{"Start", "context setup validate preflight"},
			{"Lifecycle", "plan apply destroy status diff"},
			{"Resources", "machine cluster media secret add-ons render"},
			{"Tools", "version completion help"},
		} {
			var rows [][]string
			for _, name := range strings.Fields(group.names) {
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
			if flag.Hidden || strings.HasPrefix(flag.Name, "ssh-") != advanced {
				return
			}
			label := "--" + flag.Name
			if flag.Shorthand != "" {
				label = "-" + flag.Shorthand + ", " + label
			}
			usage := flag.Usage
			if flag.Name == "context" {
				if contextual, ok := controllerContextUsage(command.Annotations["bootwright.command"]); ok {
					usage = contextual
				}
			}
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
	} else {
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

func writeConciseHelp(w io.Writer, command *cobra.Command) error {
	_, err := fmt.Fprintf(w, "Usage: %s\nRun 'bootwright help' for available commands.\n", command.UseLine())
	return err
}

func presentationCommand(path string) bool {
	return path == "help" || path == "version" || strings.HasPrefix(path, "completion ")
}

func helpExample(path string) string {
	examples := map[string]string{
		"machine info": "--context lab --name node", "secret info": "--context lab --name ssh-key",
		"context init": "--name lab --input-dir ./input", "context update": "--name lab --input-dir ./input", "context use": "--name lab", "context list": "", "context current": "--short", "context delete": "--name lab --purge",
		"secret set": "--name token --value-file ./token --yes", "secret generate": "--name ssh-key", "secret check": "--output json", "secret list": "--output json", "secret show": "--name token --part value", "secret delete": "--name token", "secret encryption init": "", "secret encryption status": "", "secret encryption rotate": "",
		"media add": "--name installer.iso --from-file ./installer.iso", "media list": "--checksums", "media delete": "--name installer.iso",
		"validate": "--file ./input --output json", "preflight controller": "--context lab", "preflight storage-cluster": "--context lab --output json", "plan": "--context lab --stage machines", "status": "--context lab", "diff": "--context lab --observe", "render effective": "--context lab --output json", "apply": "--context lab --stage machines", "destroy": "--context lab",
		"machine list": "--context lab --power-status", "machine rsh": "--context lab --name node", "machine exec": "--context lab --name node -- uname -a", "machine start": "--context lab --name node", "machine stop": "--context lab --name node", "machine restart": "--context lab --name node", "machine trust": "--context lab --dry-run", "setup": "--dry-run", "cluster kubeconfig": "--context lab --name cluster",
		"cluster list": "--context lab", "cluster info": "--context lab --name storage", "cluster rsh": "--context lab --name storage --node node-1", "cluster exec": "--context lab --name storage -- uname -a",
		"version": "", "help": "context init", "completion bash": "", "completion zsh": "", "completion fish": "", "completion powershell": "",
	}
	suffix, ok := examples[path]
	if !ok {
		return ""
	}
	return "  " + strings.TrimSpace("bootwright "+path+" "+suffix)
}
