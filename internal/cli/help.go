package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
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
	if usage, ok := controllerContextUsage(command.Annotations["bootwright.command"]); ok {
		if flag := command.InheritedFlags().Lookup("context"); flag != nil {
			previous := flag.Usage
			flag.Usage = usage
			defer func() { flag.Usage = previous }()
		}
	}
	var text strings.Builder
	description := command.Long
	if description == "" {
		description = command.Short
	}
	fmt.Fprintln(&text, description)
	fmt.Fprintln(&text)
	text.WriteString(command.UsageString())
	_, err := io.WriteString(w, strings.TrimRight(text.String(), "\n")+"\n")
	return err
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
