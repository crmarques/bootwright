package cli

import (
	_ "embed"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

func writeCompletion(w io.Writer, root *cobra.Command, shell string, noDescriptions bool) error {
	protocol := completionRequest
	if noDescriptions || shell == "bash" {
		protocol = completionRequestNoDescriptions
	}
	var script string
	switch shell {
	case "bash":
		script = bashCompletion
	case "zsh":
		script = zshCompletion
	case "fish":
		script = fishCompletion
	case "powershell":
		script = powershellCompletion
	default:
		return fmt.Errorf("unsupported completion shell %s", strconv.Quote(shell))
	}
	if root.Name() != "bootwright" {
		return fmt.Errorf("completion requires the bootwright command")
	}
	_, err := io.WriteString(w, strings.ReplaceAll(script, "@PROTOCOL@", protocol))
	return err
}

var (
	//go:embed completion/bash.sh
	bashCompletion string
	//go:embed completion/zsh.sh
	zshCompletion string
	//go:embed completion/fish.fish
	fishCompletion string
	//go:embed completion/powershell.ps1
	powershellCompletion string
)
